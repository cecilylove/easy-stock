import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { AuctionTrace, KLine } from '../lib/backend';
import { StockIntradayChart } from './StockIntradayChart';

const auction: AuctionTrace = {
	symbol: '600519.SH', trade_date: '2026-09-30',
	points: [{ time: '2026-09-30T09:15:00+08:00', price: 10 }, { time: '2026-09-30T09:25:00+08:00', price: 10.2 }],
	meta: { source: 'eastmoney:pre-open', fetched_at: '2026-09-30T09:25:00+08:00', latency_ms: 5, stale: false },
};

const line = (time: string, close: number) => ({ time, close, open: close, high: close, low: close, volume: 2 }) as KLine;

describe('stock intraday fixed-axis view', () => {
	it('renders auction-only before continuous trading without fabricated previous close', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[]} auction={auction} showAuction />);
		expect(html).toContain('09:15–09:25');
		expect(html).toContain('stock-intraday-auction-line');
		expect(html).not.toContain('class="stock-intraday-price-line"');
		expect(html).toContain('昨收暂不可用');
	});
	it('uses a verified prior close only when supplied and separates afternoon from morning', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[
			line('2026-09-29T09:31:00+08:00', 20), line('2026-09-30T09:31:00+08:00', 10), line('2026-09-30T13:01:00+08:00', 10.4),
		]} auction={auction} showAuction previousClose={9.8} />);
		expect(html).toContain('昨收参考 9.80');
		expect((html.match(/class="stock-intraday-price-line"/g) || []).length).toBe(2);
		expect(html).not.toContain('2026-09-29 固定');
	});
	it('does not draw an auction path when the switch is off', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[line('2026-09-30T09:31:00+08:00', 10)]} auction={auction} showAuction={false} />);
		expect(html).not.toContain('stock-intraday-auction-line');
		expect(html).not.toContain('09:15–09:25');
	});
});
