// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BackendConfig, KLine } from '../lib/backend';
import { StockDetailWorkspace } from './StockDetailWorkspace';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('../lib/backend', () => ({ requestJSON: request }));

const config: BackendConfig = { backendUrl: 'http://127.0.0.1:20081', token: 'fixture-only' };
const firstSymbol = '000002.SZ';
const secondSymbol = '600519.SH';
const meta = (source = 'fixture') => ({ source, fetched_at: '2026-09-30T10:00:00+08:00', latency_ms: 0, stale: false });
const lines = (symbol = firstSymbol, price = 10, source = 'fixture'): KLine[] => [35, 40].map(minute => ({
	symbol, time: `2026-09-30T09:${minute}:00+08:00`, open: price, high: price + 1, low: price - 1,
	close: price + (minute - 35) / 10, previous_close: 10, volume: 100, amount: 1000, meta: meta(source),
}));
function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (error: Error) => void;
	const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
	return { promise, resolve, reject };
}
type CapturedRequest = { url: URL; signal: AbortSignal | undefined };
let host: HTMLDivElement;
let root: Root;
let calls: CapturedRequest[];
let nextChart: ((call: CapturedRequest) => Promise<{ data: KLine[] }>) | null;

function chartRequests(period = '1', symbol = firstSymbol) {
	return calls.filter(call => call.url.pathname.endsWith('/kline') && call.url.searchParams.get('period') === period && call.url.searchParams.get('symbol') === symbol);
}
function panel() { return host.querySelector<HTMLElement>('.stock-detail-chart-panel')!; }
function chartSVG() { return panel().querySelector<SVGSVGElement>('svg[role="img"]'); }
async function render(symbol = firstSymbol) {
	await act(async () => root.render(<StockDetailWorkspace config={config} symbol={symbol} onSelectSymbol={() => {}} onOpenAnalysis={() => {}} refreshKey={0} />));
}
async function selectPeriod(label: string) {
	const button = [...host.querySelectorAll<HTMLButtonElement>('.stock-detail-periods button')].find(item => item.textContent === label)!;
	await act(async () => button.click());
}
async function advance(ms: number) {
	await act(async () => { vi.advanceTimersByTime(ms); });
}

beforeEach(() => {
	vi.useFakeTimers();
	vi.setSystemTime(new Date('2026-09-30T02:00:00Z')); // Wednesday, 10:00 Shanghai; inside trading hours.
	vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
	Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
	window.localStorage.clear();
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	calls = []; nextChart = null;
	request.mockReset();
	request.mockImplementation((_config: BackendConfig, route: string, options?: RequestInit) => {
		const url = new URL(route, config.backendUrl); const call = { url, signal: options?.signal || undefined };
		calls.push(call);
		if (url.pathname.endsWith('/directory')) return Promise.resolve({ data: { stocks: [
			{ symbol: firstSymbol, code: '000002', name: '万科A' }, { symbol: secondSymbol, code: '600519', name: '贵州茅台' },
		], stale: false } });
		if (url.pathname.endsWith('/realtime')) {
			const symbol = url.searchParams.get('symbols')!;
			return Promise.resolve({ data: [{ symbol, name: symbol === firstSymbol ? '万科A' : '贵州茅台', price: 10,
				open: 10, high: 11, low: 9, previous_close: 10, change: 0, change_percent: 0,
				trade_time: '2026-09-30T10:00:00+08:00', meta: meta() }] });
		}
		if (url.pathname.endsWith('/kline')) {
			if (nextChart) { const handler = nextChart; nextChart = null; return handler(call); }
			return Promise.resolve({ data: lines(url.searchParams.get('symbol')!) });
		}
		throw new Error(`Unexpected request: ${route}`);
	});
});
afterEach(async () => {
	await act(async () => root.unmount());
	host.remove(); window.localStorage.clear();
	vi.useRealTimers(); vi.unstubAllGlobals();
});

describe('stock detail refresh preserves the rendered chart', () => {
	it('keeps the auction failure reason visible while a retry is pending', async () => {
		vi.setSystemTime(new Date('2026-09-30T09:20:00+08:00'));
		const normalRequest = request.getMockImplementation()!;
		const retry = deferred<unknown>();
		let attempts = 0;
		request.mockImplementation((backend: BackendConfig, route: string, options?: RequestInit) => {
			if (route.includes('/quotes/auction')) return ++attempts === 1 ? Promise.reject(new Error('auction fixture failure')) : retry.promise;
			return normalRequest(backend, route, options);
		});
		await render(); await selectPeriod('分时');
		await act(async () => host.querySelector<HTMLInputElement>('.stock-detail-auction-toggle input')!.click());
		expect(panel().textContent).toContain('auction fixture failure');
		const retryButton = [...panel().querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent === '重试竞价')!;
		await act(async () => retryButton.click());
		expect(attempts).toBe(2);
		expect(panel().textContent).toContain('auction fixture failure');
		expect(panel().textContent).not.toContain('正在获取竞价参考点');
		await act(async () => retry.reject(new Error('auction fixture failure')));
	});

	it('loads the first chart and keeps the exact SVG during and after a 5-second intraday refresh', async () => {
		const initial = deferred<{ data: KLine[] }>(); nextChart = () => initial.promise;
		await render();
		expect(panel().textContent).toContain('正在加载'); expect(chartSVG()).toBeNull();
		await act(async () => initial.resolve({ data: lines() }));
		expect(chartSVG()).not.toBeNull();
		await selectPeriod('分时');
		const svg = chartSVG(); expect(svg).not.toBeNull();
		const layoutRows = [...panel().children];
		const poll = deferred<{ data: KLine[] }>(); nextChart = () => poll.promise;
		const inserted: string[] = [];
		const observer = new MutationObserver(records => records.forEach(record => record.addedNodes.forEach(node => inserted.push(node.textContent || ''))));
		observer.observe(panel(), { subtree: true, childList: true });
		try {
			await advance(5000);
			expect(chartRequests()).toHaveLength(2); expect(chartSVG()).toBe(svg);
			expect([...panel().children]).toEqual(layoutRows);
			expect(panel().textContent).not.toMatch(/正在加载|正在更新；|本页先前加载的旧快照/);
			expect(inserted.join(' ')).not.toMatch(/正在加载|正在更新；|本页先前加载的旧快照/);
			await act(async () => poll.resolve({ data: lines(firstSymbol, 11) }));
			expect(chartSVG()).toBe(svg); expect(panel().querySelector('.kline-chart-placeholder')).toBeNull();
			expect([...panel().children]).toEqual(layoutRows);
		} finally { observer.disconnect(); }
	});

	it('keeps the old SVG and data when an automatic refresh fails', async () => {
		await render(); await selectPeriod('分时');
		const svg = chartSVG(); const path = svg!.querySelector('path')?.getAttribute('d');
		const poll = deferred<{ data: KLine[] }>(); nextChart = () => poll.promise;
		await advance(5000);
		await act(async () => poll.reject(new Error('fixture upstream unavailable')));
		expect(chartSVG()).toBe(svg); expect(svg!.querySelector('path')?.getAttribute('d')).toBe(path);
		expect(panel().querySelector('[role="status"]')?.textContent).toContain('fixture upstream unavailable');
		const retry = deferred<{ data: KLine[] }>(); nextChart = () => retry.promise;
		await advance(5000);
		expect(chartSVG()).toBe(svg); expect(panel().textContent).not.toContain('正在加载');
		expect(panel().querySelector('[role="status"]')?.textContent).toContain('fixture upstream unavailable');
		await act(async () => retry.reject(new Error('fixture second failure')));
		expect(chartSVG()).toBe(svg);
		expect(panel().querySelector('[role="status"]')?.textContent).toContain('fixture second failure');
	});

	it.each([['分时', '1', 5000], ['日K', 'day', 60000]] as const)('does not overlap or cancel a slow %s refresh', async (label, apiPeriod, interval) => {
		await render(); await selectPeriod(label);
		const poll = deferred<{ data: KLine[] }>(); nextChart = () => poll.promise;
		await advance(interval); const active = chartRequests(apiPeriod).at(-1)!;
		await advance(interval); await advance(interval); await advance(interval);
		expect(chartRequests(apiPeriod)).toHaveLength(2); expect(active.signal?.aborted).toBe(false);
		await act(async () => poll.resolve({ data: lines() }));
		await advance(interval); expect(chartRequests(apiPeriod)).toHaveLength(3);
	});

	it.each([['分时', '1'], ['日K', 'day']])('%s pauses every source while hidden and refreshes when visible again', async (label, apiPeriod) => {
		await render(); await selectPeriod(label);
		const before = calls.length; const chartBefore = chartRequests(apiPeriod).length; const svg = chartSVG();
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
		await advance(65000);
		expect(calls).toHaveLength(before); expect(chartSVG()).toBe(svg);
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
		await act(async () => document.dispatchEvent(new Event('visibilitychange')));
		expect(chartRequests(apiPeriod)).toHaveLength(chartBefore + 1); expect(chartSVG()).toBe(svg);
	});

	it('ignores a late intraday response after switching the selected period', async () => {
		await render(); await selectPeriod('分时');
		const poll = deferred<{ data: KLine[] }>(); nextChart = () => poll.promise;
		await advance(5000); const previous = chartRequests().at(-1)!;
		await selectPeriod('周K'); const svg = chartSVG();
		expect(previous.signal?.aborted).toBe(true);
		await act(async () => poll.resolve({ data: lines(firstSymbol, 99, 'old-period-response') }));
		expect(panel().querySelector('h3')?.textContent).toContain('周K'); expect(chartSVG()).toBe(svg);
		expect(panel().textContent).not.toContain('old-period-response');
	});

	it('ignores a late response from the previously selected stock', async () => {
		const initial = deferred<{ data: KLine[] }>(); nextChart = () => initial.promise;
		await render(); const previous = chartRequests('day').at(-1)!;
		await render(secondSymbol); const svg = chartSVG();
		expect(previous.signal?.aborted).toBe(true);
		await act(async () => initial.resolve({ data: lines(firstSymbol, 99, 'old-stock-response') }));
		expect(panel().querySelector('h3')?.textContent).toContain('贵州茅台'); expect(chartSVG()).toBe(svg);
		expect(panel().textContent).not.toContain('old-stock-response');
	});

	it.each([['5日', '5'], ['日K', 'day'], ['周K', 'week'], ['月K', 'month']])('%s refreshes at 60 seconds while preserving the SVG', async (label, apiPeriod) => {
		await render(); await selectPeriod(label);
		const svg = chartSVG(); const before = chartRequests(apiPeriod).length;
		for (let tick = 0; tick < 11; tick += 1) await advance(5000);
		expect(chartRequests(apiPeriod)).toHaveLength(before);
		await advance(5000);
		expect(chartRequests(apiPeriod)).toHaveLength(before + 1); expect(chartSVG()).toBe(svg);
	});
});
