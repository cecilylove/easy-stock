import { describe, expect, it } from 'vitest';
import { disconnectLiveQuotes, isCurrentLiveQuote, mergeLiveQuotes } from './live-quotes';
import type { Quote, SectorMap } from './backend';
import { buildThemeStocks } from './short-term';

const now = Date.parse('2026-09-30T10:00:00+08:00');
const meta = { source: 'sina', fetched_at: new Date(now).toISOString(), latency_ms: 0, stale: false };
const quote: Quote = { symbol: '000001.SZ', name: 'fixture', price: 9, open: 10, previous_close: 10, high: 10, low: 9, change: -1, change_percent: -10, trade_time: new Date(now).toISOString(), meta };
const map: SectorMap = { theme: 'fixture', name: 'fixture', tabs: [], meta, groups: [{ id: 'a', name: 'a', nodes: [{ id: 'b', name: 'b', change_percent: 0, main_net_inflow: 0, match_status: 'matched', stocks: [{ ...quote, price: 11, amount: 1, volume: 1, total_market_cap: 1, float_market_cap: 1, main_net_inflow: 0, meta: { ...meta, fetched_at: new Date(now - 5_000).toISOString() } }] }] }] };

describe('live theme quote freshness', () => {
	it('keeps source/time metadata and overlays only current connected observations', () => {
		const quotes = mergeLiveQuotes({}, [quote], now);
		expect(isCurrentLiveQuote({ ...quotes[quote.symbol], trade_time: undefined }, undefined, now)).toBe(false);
		expect(quotes[quote.symbol].meta).toEqual(meta);
		expect(quotes[quote.symbol].trade_time).toEqual(quote.trade_time);
		expect(buildThemeStocks(map, quotes, {}, now)[0]).toMatchObject({ price: 9, live: true });
		expect(buildThemeStocks(map, disconnectLiveQuotes(quotes), {}, now)[0]).toMatchObject({ price: 11, live: false });
		expect(buildThemeStocks(map, quotes, {}, now + 31_000)[0]).toMatchObject({ price: 11, live: false });
	});
	it('rejects stale, cross-day and older observations and newer snapshots win', () => {
		const quotes = mergeLiveQuotes({}, [quote], now);
		const stale = mergeLiveQuotes({}, [{ ...quote, meta: { ...meta, stale: true } }], now);
		expect(isCurrentLiveQuote(stale[quote.symbol], map.meta, now)).toBe(false);
		expect(isCurrentLiveQuote(quotes[quote.symbol], { ...meta, fetched_at: new Date(now + 1_000).toISOString() }, now)).toBe(false);
		const historical = mergeLiveQuotes({}, [{ ...quote, trade_time: '2026-09-29T10:00:00+08:00' }], now);
		expect(isCurrentLiveQuote(historical[quote.symbol], undefined, now)).toBe(false);
		const older = { ...quote, price: 8, meta: { ...meta, fetched_at: new Date(now - 1_000).toISOString() } };
		expect(mergeLiveQuotes(quotes, [older], now)[quote.symbol].price).toBe(9);
	});
	it('compares provider observations when the last tick precedes a snapshot fetch', () => {
		const lastTick = new Date(now - 5_000).toISOString();
		const olderSnapshot = { ...meta, fetched_at: new Date(now - 2_000).toISOString() };
		const quotes = mergeLiveQuotes({}, [{ ...quote, trade_time: lastTick }], now);
		expect(isCurrentLiveQuote(quotes[quote.symbol], olderSnapshot, now)).toBe(true);
		const newerSnapshot = { ...meta, fetched_at: new Date(now + 1_000).toISOString() };
		expect(isCurrentLiveQuote(quotes[quote.symbol], newerSnapshot, now)).toBe(false);
		const refreshed = mergeLiveQuotes(quotes, [{ ...quote, price: 9.1, trade_time: lastTick, meta: { ...meta, fetched_at: new Date(now + 2_000).toISOString() } }], now + 2_000);
		expect(refreshed[quote.symbol].price).toBe(9.1);
		expect(isCurrentLiveQuote(refreshed[quote.symbol], newerSnapshot, now + 2_000)).toBe(true);
		const lateOlder = mergeLiveQuotes(refreshed, [{ ...quote, price: 8.9, trade_time: lastTick }], now + 3_000);
		expect(lateOlder[quote.symbol].price).toBe(9.1);
	});
});
