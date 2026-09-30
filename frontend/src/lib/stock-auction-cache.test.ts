import { describe, expect, it } from 'vitest';
import type { AuctionTrace } from './backend';
import { readAuctionTrace, saveAuctionTrace } from './stock-auction-cache';

const trace: AuctionTrace = { symbol: '600519.SH', trade_date: '2026-09-30', points: [{ time: '2026-09-30T09:15:00+08:00', price: 12 }], meta: { source: 'eastmoney:pre-open', fetched_at: '2026-09-30T09:16:00+08:00', latency_ms: 20, stale: false } };
const values = new Map<string, string>();
const storage = { getItem: (key: string) => values.get(key) || null, setItem: (key: string, value: string) => { values.set(key, value); } };

describe('same-day auction fallback cache', () => {
	it('preserves a fetched trace for a same-day transient upstream outage', () => {
		values.clear(); saveAuctionTrace(trace, storage);
		expect(readAuctionTrace('600519.SH', '2026-09-30', storage)).toEqual(trace);
		expect(readAuctionTrace('600519.SH', '2026-10-01', storage)).toBeNull();
		expect(readAuctionTrace('000001.SZ', '2026-09-30', storage)).toBeNull();
	});
	it('does not crash when browser storage is unavailable', () => {
		expect(readAuctionTrace('600519.SH', '2026-09-30')).toBeNull();
		expect(() => saveAuctionTrace(trace)).not.toThrow();
	});
	it('rejects invalid or mixed-date price points', () => {
		values.clear(); saveAuctionTrace({ ...trace, points: [{ time: '2026-09-29T09:15:00+08:00', price: 12 }] }, storage);
		expect(readAuctionTrace('600519.SH', '2026-09-30', storage)).toBeNull();
		saveAuctionTrace({ ...trace, points: [{ time: trace.points[0].time, price: Number.NaN }] }, storage);
		expect(readAuctionTrace('600519.SH', '2026-09-30', storage)).toBeNull();
	});
});
