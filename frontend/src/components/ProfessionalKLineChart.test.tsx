// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { KLine } from '../lib/backend';
import { ProfessionalKLineChart } from './ProfessionalKLineChart';

const lines: KLine[] = Array.from({ length: 160 }, (_, index) => ({ symbol: '000002.SZ', time: new Date(Date.UTC(2026, 0, index + 1)).toISOString(), open: 10 + index / 100, close: 10.01 + index / 100, high: 10.2 + index / 100, low: 9.9 + index / 100, volume: 100 + index, amount: 1000, meta: { source: 'fixture', fetched_at: '2026-09-30T10:00:00+08:00', stale: false, latency_ms: 0 } }));
let host: HTMLDivElement, root: Root;
beforeEach(() => {
	vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	act(() => root.render(<ProfessionalKLineChart lines={lines} symbol="000002.SZ" periodLabel="日K" state="ready" />));
});
afterEach(() => { act(() => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
function click(name: string) { const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(item => item.getAttribute('aria-label') === name || item.textContent === name)!; act(() => button.click()); }
function key(name: string) { act(() => host.querySelector('.professional-kline')!.dispatchEvent(new KeyboardEvent('keydown', { key: name, bubbles: true }))); }
describe('professional chart navigation', () => {
	it('zooms, browses history and restores the latest viewport', () => {
		expect(host.querySelectorAll('.kline-body')).toHaveLength(80);
		expect(host.innerHTML).not.toContain('NaN');
		click('放大K线'); expect(host.querySelectorAll('.kline-body')).toHaveLength(64);
		const latest = host.querySelector('.chart-value-strip strong')!.textContent;
		click('向前查看历史K线'); expect(host.querySelector('.chart-value-strip strong')!.textContent).not.toBe(latest);
		click('回到最新'); expect(host.querySelectorAll('.kline-body')).toHaveLength(80);
		expect(host.innerHTML).not.toContain('NaN');
		expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(latest);
	});
	it('links keyboard inspection to all panes and switches indicators', () => {
		key('ArrowLeft'); expect(host.querySelector('.professional-crosshair')).not.toBeNull();
		const previous = host.querySelector('.chart-value-strip strong')!.textContent;
		key('ArrowLeft'); expect(host.querySelector('.chart-value-strip strong')!.textContent).not.toBe(previous);
		key('Escape'); expect(host.querySelector('.professional-crosshair')).toBeNull();
		click('KDJ'); expect(host.textContent).toContain('KDJ (9,3,3)'); expect(host.querySelectorAll('.professional-macd-up')).toHaveLength(0);
		click('仅量价'); expect(host.textContent).not.toContain('KDJ (9,3,3)');
	});
	it('renders legal negative adjusted prices and excludes malformed candles', () => {
		const valid = [
			{ ...lines[0], open: -2, high: 1, low: -3, close: -1 },
			{ ...lines[1], open: 1, high: 4, low: 0, close: 3 },
		];
		act(() => root.render(<ProfessionalKLineChart lines={[...valid, { ...lines[2], volume: NaN }, { ...lines[3], high: Infinity }]} symbol="000002.SZ" periodLabel="年K" state="ready" />));
		expect(host.querySelectorAll('.kline-body')).toHaveLength(2);
		expect(host.innerHTML).not.toMatch(/NaN|Infinity/);
		const ticks = [...host.querySelectorAll('.professional-chart-svg .kline-axis-label')].map(item => Number(item.textContent));
		expect(ticks.some(value => value < 0)).toBe(true);
	});

	it('preserves a panned viewport and SVG while appending new data', () => {
		click('向前查看历史K线');
		const svg = host.querySelector('svg'), candle = host.querySelector('.kline-body');
		const date = host.querySelector('.chart-value-strip strong')?.textContent;
		act(() => root.render(<ProfessionalKLineChart lines={[...lines, { ...lines.at(-1)!, time: '2026-10-01T15:00:00+08:00' }]} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('svg')).toBe(svg); expect(host.querySelector('.kline-body')).toBe(candle);
		expect(host.querySelector('.chart-value-strip strong')?.textContent).toBe(date);
	});
});
