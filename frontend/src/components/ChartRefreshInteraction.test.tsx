import { beforeEach, describe, expect, it, vi } from 'vitest';
import { isValidElement, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { AuctionTrace, KLine } from '../lib/backend';
import { KLineChart } from './KLineChart';
import { StockIntradayChart } from './StockIntradayChart';

// Drive the components' actual pointer handlers and render the resulting tree.
// The hook seam stores the point identity; it does not depend on hook indexes.
const hover = vi.hoisted(() => ({ selection: null as unknown }));
vi.mock('react', async importOriginal => {
	const actual = await importOriginal<typeof import('react')>();
	return { ...actual, useState: (initial: unknown) => {
		if (initial !== null) throw new Error('Unexpected chart state');
		return [hover.selection, (value: unknown) => { hover.selection = value; }];
	} };
});
vi.mock('../lib/use-chart-viewport', () => ({ useChartViewport: () => ({ containerRef: { current: null }, width: 960, scrollable: false }) }));

type InteractiveProps = { className?: string; children?: unknown; onMouseMove?: (event: unknown) => void };
function elementWithClass(node: unknown, className: string): ReactElement<InteractiveProps> | undefined {
	if (Array.isArray(node)) {
		for (const child of node) { const found = elementWithClass(child, className); if (found) return found; }
		return;
	}
	if (!isValidElement<InteractiveProps>(node)) return;
	if (node.props.className?.split(' ').includes(className)) return node;
	return elementWithClass(node.props.children, className);
}
const line = (day: string, minute: string, close: number): KLine => ({ time: `${day}T${minute}:00+08:00`, open: close, close, high: close, low: close, volume: 100, amount: 1000 } as KLine);
const svgPointer = (clientX: number) => ({ clientX, clientY: 100, currentTarget: {
	getBoundingClientRect: () => ({ width: 960, height: 400 }),
	getScreenCTM: () => ({ a: 1, b: 0, inverse: () => ({}) }),
	createSVGPoint: () => ({ x: 0, y: 0, matrixTransform() { return { x: this.x, y: this.y }; } }),
} });

beforeEach(() => { hover.selection = null; });

describe('chart inspection across refreshes', () => {
	it('retains the inspected candle across history backfill and clears a removed candle safely', () => {
		const lines = [line('2026-09-28', '15:00', 10), line('2026-09-29', '15:00', 11), line('2026-09-30', '15:00', 12)];
		const props = { lines, symbol: '000002.SZ', fluid: true };
		const layer = elementWithClass(KLineChart(props), 'kline-hover-layer')!;
		layer.props.onMouseMove!({ clientX: 472, currentTarget: { getBoundingClientRect: () => ({ left: 68, width: 808 }) } });
		expect(renderToStaticMarkup(KLineChart(props))).toContain('<strong>09/29</strong>');
		const backfilled = [line('2026-09-25', '15:00', 9), ...lines];
		expect(renderToStaticMarkup(KLineChart({ ...props, lines: backfilled, state: 'loading' }))).toContain('<strong>09/29</strong>');
		const shortened = renderToStaticMarkup(KLineChart({ ...props, lines: [lines[2]] }));
		expect(shortened).not.toContain('kline-hover-card');
		expect(shortened).not.toContain('NaN');
		expect(renderToStaticMarkup(KLineChart({ ...props, symbol: '600519.SH' }))).not.toContain('kline-hover-card');
	});

	it('retains the inspected trading minute when an earlier minute is added', () => {
		const lines = [line('2026-09-30', '09:31', 10), line('2026-09-30', '09:32', 11)];
		const props = { lines, symbol: '000002.SZ', tradeDay: '2026-09-30', showAuction: false };
		const svg = elementWithClass(StockIntradayChart(props), 'stock-intraday-chart')!;
		svg.props.onMouseMove!(svgPointer(68 + 808 / 240));
		expect(renderToStaticMarkup(StockIntradayChart(props))).toContain('<strong>2026-09-30 09:31</strong>');
		const refreshed = [line('2026-09-30', '09:30', 9), { ...lines[0], close: 10.5 }, lines[1]];
		const html = renderToStaticMarkup(StockIntradayChart({ ...props, lines: refreshed }));
		expect(html).toContain('<strong>2026-09-30 09:31</strong>');
		expect(html).toContain('<b>10.50</b>');
		expect(renderToStaticMarkup(StockIntradayChart({ ...props, tradeDay: '2026-10-01' }))).not.toContain('stock-intraday-hover-card');
		expect(renderToStaticMarkup(StockIntradayChart({ ...props, symbol: '600519.SH' }))).not.toContain('stock-intraday-hover-card');
	});

	it('keeps the auction reference time across backfill and hides it when the auction switch is off', () => {
		const auction: AuctionTrace = { symbol: '000002.SZ', trade_date: '2026-09-30', points: [
			{ time: '2026-09-30T09:20:00+08:00', price: 10 },
			{ time: '2026-09-30T09:25:00+08:00', price: 11 },
		], meta: { source: 'fixture', fetched_at: '2026-09-30T09:25:00+08:00', latency_ms: 0, stale: false } };
		const props = { lines: [], auction, symbol: '000002.SZ', tradeDay: '2026-09-30', showAuction: true };
		const svg = elementWithClass(StockIntradayChart(props), 'stock-intraday-chart')!;
		svg.props.onMouseMove!(svgPointer(68 + 808 * .19 / 2));
		const backfilled = { ...auction, points: [{ time: '2026-09-30T09:15:00+08:00', price: 9 }, ...auction.points] };
		expect(renderToStaticMarkup(StockIntradayChart({ ...props, auction: backfilled }))).toContain('<strong>2026-09-30 09:20 · 竞价参考</strong>');
		expect(renderToStaticMarkup(StockIntradayChart({ ...props, showAuction: false }))).not.toContain('stock-intraday-hover-card');
	});
});
