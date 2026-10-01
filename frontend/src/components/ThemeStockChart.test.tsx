// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON, type KLine } from '../lib/backend';
import { ThemeStockChart } from './ThemeStockChart';

vi.mock('./ProfessionalKLineChart', () => ({ ProfessionalKLineChart: ({ compact, onOpenIntraday }: { compact?: boolean; onOpenIntraday?: (date: string) => void }) => {
	const [window, setWindow] = useState(80);
	return <section data-testid={compact ? 'compact-chart' : 'expanded-chart'}><span>窗口 {window}</span><button onClick={() => setWindow(40)}>调整窗口</button><button onClick={() => onOpenIntraday?.('2026-09-30')}>查看当日分时</button></section>;
} }));
vi.mock('./StockIntradayChart', () => ({ StockIntradayChart: () => <div>历史分钟图</div> }));
vi.mock('../lib/backend', () => ({ requestJSON: vi.fn(async () => ({ data: { symbol: '000002.SZ', trade_date: '2026-09-30', availability: 'unavailable', lines: [], available_dates: ['2026-09-30'], meta: { source: 'sina', fetched_at: '2026-10-01T10:00:00+08:00', stale: true } } })) }));

let host: HTMLDivElement, root: Root;
const lines = [{ symbol: '000002.SZ', time: '2026-09-30T00:00:00+08:00', open: 4, high: 5, low: 3, close: 4.5, volume: 100 }] as KLine[];
beforeEach(() => { vi.clearAllMocks(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); host = document.createElement('div'); document.body.append(host); root = createRoot(host); });
afterEach(() => { act(() => root.unmount()); host.remove(); document.body.style.overflow = ''; vi.unstubAllGlobals(); });
async function render(symbol = '000002.SZ') { await act(async () => root.render(<ThemeStockChart config={{ backendUrl: 'http://localhost', token: '' }} symbol={symbol} name="万科A" lines={lines} state="ready" />)); }
async function click(label: string, parent: Element = host) { const button = [...parent.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent === label || button.getAttribute('aria-label') === label); expect(button).toBeDefined(); await act(async () => button!.click()); }

describe('theme chart inspection overlays', () => {
	it('keeps compact and expanded windows while inspecting a historical date, and restores scrolling', async () => {
		await render();
		await click('调整窗口', host.querySelector('[data-testid="compact-chart"]')!);
		await click('放大图表');
		await click('调整窗口', host.querySelector('[data-testid="expanded-chart"]')!);
		await click('查看当日分时', host.querySelector('[data-testid="expanded-chart"]')!);
		expect(host.querySelector('.theme-chart-backdrop')?.hasAttribute('hidden')).toBe(true);
		expect(host.querySelector('.historical-intraday-dialog')?.textContent).toContain('2026-09-30');
		expect(document.body.style.overflow).toBe('hidden');
		await click('关闭历史分时');
		expect(host.querySelector('.theme-chart-backdrop')?.hasAttribute('hidden')).toBe(false);
		expect(host.querySelector('[data-testid="expanded-chart"]')?.textContent).toContain('窗口 40');
		expect(document.body.style.overflow).toBe('hidden');
		await click('关闭放大图表');
		expect(host.querySelector('[data-testid="compact-chart"]')?.textContent).toContain('窗口 40');
		expect(document.body.style.overflow).toBe('');
	});
	it('Escape closes only historical inspection, retaining the expanded view', async () => {
		await render(); await click('放大图表'); await click('查看当日分时', host.querySelector('[data-testid="expanded-chart"]')!);
		await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
		expect(host.querySelector('.historical-intraday-dialog')).toBeNull();
		expect(host.querySelector('.theme-chart-dialog')).not.toBeNull();
	});
	it('closes inspection on stock changes rather than showing another stock with an old date', async () => {
		await render(); await click('放大图表'); await click('查看当日分时', host.querySelector('[data-testid="expanded-chart"]')!);
		await render('000001.SZ');
		expect(host.querySelector('[role="dialog"]')).toBeNull();
		expect(document.body.style.overflow).toBe('');
		expect(vi.mocked(requestJSON).mock.calls.some(([, path]) => path.includes('symbol=000001.SZ'))).toBe(false);
	});
});
