// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BackendConfig, HistoricalIntradayResponse, KLine } from '../lib/backend';
import { HistoricalIntradayDialog } from './HistoricalIntradayDialog';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('../lib/backend', () => ({ requestJSON: request }));
vi.mock('./StockIntradayChart', () => ({ StockIntradayChart: (props: { symbol: string; tradeDay: string; lines: KLine[]; showAuction: boolean; previousClose?: number }) => <div role="img" aria-label={`${props.symbol} ${props.tradeDay} 历史分时图`} data-auction={String(props.showAuction)} data-previous-close={props.previousClose}>{props.lines.map(line => `${line.time}:${line.close}`).join(',')}</div> }));

const config: BackendConfig = { backendUrl: 'http://127.0.0.1:20081', token: 'fixture' };
const symbol = '000002.SZ';
const date = '2026-09-30';
let host: HTMLDivElement;
let root: Root;
const close = vi.fn();
function response(tradeDate = date, overrides: Partial<HistoricalIntradayResponse['data']> = {}): HistoricalIntradayResponse {
	const meta = { source: 'sina:historical-intraday', fetched_at: '2026-10-01T10:00:00+08:00', latency_ms: 1, stale: false };
	return { data: { symbol, trade_date: tradeDate, availability: 'available', available_dates: ['2026-09-30', '2026-09-28'], previous_close: 4,
		lines: [{ symbol, time: `${tradeDate}T09:35:00+08:00`, open: 4, high: 4.2, low: 4, close: 4.2, volume: 20, amount: 84, meta }], meta, ...overrides } };
}
function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason: Error) => void;
	const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
	return { promise, resolve, reject };
}
async function render(props: Partial<Parameters<typeof HistoricalIntradayDialog>[0]> = {}) {
	await act(async () => root.render(<HistoricalIntradayDialog config={config} symbol={symbol} date={date} onClose={close} {...props} />));
}
async function click(label: string) {
	const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent?.includes(label) || item.getAttribute('aria-label') === label);
	if (!button) throw new Error(`missing button: ${label}`);
	await act(async () => button.click());
}
beforeEach(() => {
	vi.useFakeTimers(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	close.mockReset(); request.mockReset();
	request.mockImplementation(async (_config: BackendConfig, route: string) => response(new URL(route, config.backendUrl).searchParams.get('date')!));
});
afterEach(async () => {
	await act(async () => root.unmount()); host.remove();
	vi.useRealTimers(); vi.unstubAllGlobals();
});

describe('historical intraday dialog', () => {
	it('queries an explicit date once, without auction or background polling', async () => {
		await render();
		const route = new URL(request.mock.calls[0][1], config.backendUrl);
		expect(route.pathname).toBe('/api/v1/quotes/intraday'); expect(route.searchParams.get('date')).toBe(date); expect(route.searchParams.get('symbol')).toBe(symbol);
		expect(host.querySelector('[role="img"]')?.getAttribute('data-auction')).toBe('false'); expect(host.querySelector('[role="img"]')?.getAttribute('data-previous-close')).toBe('4');
		expect(host.textContent).toContain('新浪财经'); expect(host.textContent).toContain('当日原始成交价格'); expect(host.textContent).toContain('前复权 / 后复权');
		await act(async () => { vi.advanceTimersByTime(120_000); }); expect(request).toHaveBeenCalledTimes(1);
	});
	it('uses the actual service date list before daily-K fallback and skips unknown calendar dates', async () => {
		await render({ availableDates: ['2026-09-25', '2026-09-29', date] });
		await click('上一交易日');
		expect(request.mock.calls.at(-1)![1]).toContain('date=2026-09-28'); expect(host.querySelector('[role="img"]')?.getAttribute('aria-label')).toContain('2026-09-28');
		expect(host.textContent).toContain('来自来源分时覆盖记录');
		await click('下一交易日'); expect(request.mock.calls.at(-1)![1]).toContain(`date=${date}`);
	});
	it('falls back to loaded daily dates for unavailable history without showing today', async () => {
		request.mockResolvedValue(response('2026-09-25', { availability: 'unavailable', available_dates: [], lines: response().data.lines }));
		await render({ date: '2026-09-25', availableDates: ['2026-09-25', '2026-09-29'] });
		expect(host.querySelector('[role="img"]')).toBeNull(); expect(host.textContent).toContain('2026-09-25 的历史分时暂不可用');
		expect(host.textContent).toContain('来自已加载日 K'); expect(host.textContent).not.toContain('已知交易日范围'); await click('下一交易日'); expect(request.mock.calls.at(-1)![1]).toContain('date=2026-09-29');
	});
	it('aborts a date switch and ignores a late older response', async () => {
		const older = deferred<HistoricalIntradayResponse>();
		const latest = deferred<HistoricalIntradayResponse>();
		request.mockReturnValueOnce(older.promise).mockReturnValueOnce(latest.promise);
		await render({ availableDates: ['2026-09-28', date] });
		const signal = request.mock.calls[0][2].signal as AbortSignal;
		await click('上一交易日'); expect(signal.aborted).toBe(true); expect(host.querySelector('[role="img"]')).toBeNull();
		await act(async () => latest.resolve(response('2026-09-28')));
		await act(async () => older.resolve(response()));
		expect(host.querySelector('[role="img"]')?.getAttribute('aria-label')).toContain('2026-09-28'); expect(host.querySelector('[role="img"]')?.textContent).not.toContain('2026-09-30');
	});
	it('rejects mismatched dates, symbols and today-only points for a historic date', async () => {
		for (const payload of [response(date), response('2026-09-28', { symbol: '600519.SH' }), response('2026-09-28', { lines: response(date).data.lines })]) {
			request.mockResolvedValue(payload); await render({ date: '2026-09-28' }); await click('重试');
			expect(host.querySelector('[role="img"]')).toBeNull(); expect(host.querySelector('[role="alert"]')).not.toBeNull();
		}
	});
	it('keeps the same dated chart during retry and reports errors without hiding context', async () => {
		await render(); const chart = host.querySelector('[role="img"]');
		const retry = deferred<HistoricalIntradayResponse>(); request.mockReturnValueOnce(retry.promise);
		await click('重新获取'); expect(host.querySelector('[role="img"]')).toBe(chart); expect(host.textContent).toContain(`正在获取 ${date}`);
		await act(async () => retry.reject(new Error('上游暂不可用')));
		expect(host.querySelector('[role="img"]')).toBe(chart); expect(host.textContent).toContain('上游暂不可用；保留本交易日上次查询快照');
	});
	it.each(['backendUrl', 'token'] as const)('isolates the chart and source coverage when %s changes even if the new request fails', async field => {
		await render(); expect(host.querySelector('[role="img"]')).not.toBeNull();
		const pending = deferred<HistoricalIntradayResponse>(); request.mockReturnValueOnce(pending.promise);
		await render({ config: { ...config, [field]: field === 'token' ? 'changed-fixture' : 'http://localhost:20082' } });
		expect(host.querySelector('[role="img"]')).toBeNull();
		expect(host.textContent).not.toContain('来自来源分时覆盖记录');
		await act(async () => pending.reject(new Error('新后端请求失败')));
		expect(host.querySelector('[role="img"]')).toBeNull();
		expect(host.textContent).not.toContain('保留本交易日上次查询快照');
	});
	it('preserves source partial/stale messages and does not use the current quote as yesterday close', async () => {
		const payload = response(date, { availability: 'partial', previous_close: undefined }); payload.data.meta.stale = true; payload.data.meta.fallback_reason = '该日只返回部分分钟';
		request.mockResolvedValue(payload); await render();
		expect(host.textContent).toContain('部分分时点'); expect(host.textContent).toContain('09:35–09:35 · 1 个有效时间点'); expect(host.textContent).toContain('不能作为全日走势'); expect(host.textContent).toContain('缓存 / 陈旧快照'); expect(host.textContent).toContain('该日只返回部分分钟');
		expect(host.querySelector('[role="img"]')?.getAttribute('data-previous-close')).toBeNull();
	});
	it('locks scrolling, closes on Escape, restores focus and aborts on unmount', async () => {
		const opener = document.createElement('button'); document.body.append(opener); opener.focus();
		const overflow = document.body.style.overflow;
		request.mockReturnValue(new Promise(() => {})); await render();
		expect(document.body.style.overflow).toBe('hidden'); expect(document.activeElement?.getAttribute('aria-label')).toBe('关闭历史分时');
		await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))); expect(close).toHaveBeenCalledTimes(1);
		const signal = request.mock.calls[0][2].signal as AbortSignal;
		await act(async () => root.render(null)); expect(signal.aborted).toBe(true); expect(document.body.style.overflow).toBe(overflow); expect(document.activeElement).toBe(opener); opener.remove();
	});
	it('updates the symbol and initial date atomically, without requesting the previous date for the new symbol', async () => {
		await render();
		request.mockImplementation(async (_config: BackendConfig, route: string) => { const url = new URL(route, config.backendUrl); return response(url.searchParams.get('date')!, { symbol: url.searchParams.get('symbol')!, availability: 'unavailable', lines: [] }); });
		await render({ symbol: '600519.SH', date: '2026-09-28' });
		expect(request).toHaveBeenCalledTimes(2); expect(request.mock.calls.at(-1)![1]).toContain('symbol=600519.SH&date=2026-09-28'); expect(host.textContent).toContain('600519.SH · 2026-09-28');
	});
});
