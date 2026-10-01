import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { KLine } from '../lib/backend';
import { KLineChart } from './KLineChart';

const line = (time: string, price: number) => ({ time, open: price, close: price, high: price, low: price, volume: 100 }) as KLine;

describe('stock detail multi-day chart', () => {
	const lines = [line('2026-09-29T09:35:00+08:00', 10), line('2026-09-29T15:00:00+08:00', 11), line('2026-09-30T09:35:00+08:00', 12), line('2026-09-30T15:00:00+08:00', 13)];

	it('keeps the chart and scroll container while existing data refreshes', () => {
		const html = renderToStaticMarkup(<KLineChart lines={lines} state="loading" fluid />);
		expect(html).toContain('kline-plot-scroll');
		expect(html).toContain('<svg');
		expect(html).not.toContain('kline-chart-placeholder');
		const empty = renderToStaticMarkup(<KLineChart lines={[]} state="loading" fluid />);
		expect(empty).toContain('正在加载日K数据');
	});

	it('shows annual labels and never emits negative price ticks for long-term gains', () => {
		const html = renderToStaticMarkup(<KLineChart lines={[line('1991-12-31T15:00:00+08:00', 10), line('2026-09-30T15:00:00+08:00', 40)]} mode="daily" periodLabel="年K" fluid />);
		expect(html).toContain('1991年'); expect(html).toContain('2026年');
		const priceLabels = [...html.matchAll(/class="kline-axis-label"[^>]*>([^<]+)</g)].map(match => Number(match[1]));
		expect(priceLabels.length).toBeGreaterThan(0); expect(priceLabels.every(price => price > 0)).toBe(true);
	});

	it('keeps multi-day gains above a single-day limit inside the plotted range', () => {
		const html = renderToStaticMarkup(<KLineChart lines={lines} symbol="000002.SZ" mode="intraday" periodLabel="5日" fluid />);
		const path = /class="kline-close-line" d="([^"]+)"/.exec(html)![1];
		const points = [...path.matchAll(/[ML] ([\d.]+) ([\d.]+)/g)];
		// The 20% and 30% prices must remain distinguishable, below the top edge.
		expect(Number(points[2][2])).toBeGreaterThan(20);
		expect(Number(points[3][2])).toBeGreaterThan(20);
		expect(Number(points[3][2])).toBeLessThan(Number(points[2][2]));
		expect(html).toContain('首个采样价');
	});

	it('marks actual trading days and does not join overnight prices as intraday trades', () => {
		const html = renderToStaticMarkup(<KLineChart lines={lines} mode="intraday" periodLabel="5日" fluid />);
		const path = /class="kline-close-line" d="([^"]+)"/.exec(html)![1];
		expect((path.match(/M /g) || []).length).toBe(2);
		expect(html).toContain('09/29');
		expect(html).toContain('09/30');
		expect((html.match(/class="kline-session-divider"/g) || []).length).toBe(1);
	});
});
