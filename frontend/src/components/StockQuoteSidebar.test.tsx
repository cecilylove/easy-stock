import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { Quote } from '../lib/backend';
import { StockQuoteSidebar } from './StockQuoteSidebar';
const quote: Quote = { symbol: '000002.SZ', name: '万科A', price: 10, open: 9.8, high: 10.2, low: 9.7, previous_close: 9.8, change: .2, change_percent: 2.04, volume: 10000, amount: 100000, bids: Array.from({ length: 5 }, (_, index) => ({ price: 9.99 - index / 100, volume: 1000 })), asks: Array.from({ length: 5 }, (_, index) => ({ price: 10.01 + index / 100, volume: 500 })), meta: { source: 'fixture', fetched_at: '2026-09-30T15:00:00+08:00', latency_ms: 0, stale: false } };
describe('quote book boundaries', () => {
	it('displays actual levels and converts shares to lots once', () => {
		const html = renderToStaticMarkup(<StockQuoteSidebar quote={quote} stale symbol={quote.symbol} />);
		expect(html).toContain('卖五'); expect(html).toContain('买一'); expect(html).toContain('33.33%');
		expect(html).toContain('旧快照 · 非实时'); expect(html).toContain('委托量（手）');
		expect(html).toContain('未接入逐笔数据');
	});
	it('does not fabricate orders when quote source lacks depth', () => {
		const html = renderToStaticMarkup(<StockQuoteSidebar quote={{ ...quote, bids: undefined, asks: undefined }} stale={false} symbol={quote.symbol} />);
		expect(html).toContain('当前来源没有可用五档'); expect(html).not.toContain('33.33%');
	});
});
