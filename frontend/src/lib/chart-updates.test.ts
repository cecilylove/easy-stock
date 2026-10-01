import { describe, expect, it } from 'vitest';
import type { KLine } from './backend';
import { intradayAveragePrices, reconcileChartLines } from './chart-updates';

const bar = (time: string, close = 10): KLine => ({ symbol: '000002.SZ', time: `2026-09-30T${time}:00+08:00`, open: 10, high: 11, low: 9, close, volume: 100, amount: 1000, meta: { source: 'fixture', fetched_at: '2026-09-30T10:00:00+08:00', stale: false, latency_ms: 0 } });
describe('chart snapshot reconciliation', () => {
	it('appends new minutes, patches corrections and preserves prior unchanged points', () => {
		const previous = [bar('09:31'), bar('09:32')];
		const next = reconcileChartLines(previous, [bar('09:32', 10.5), bar('09:33')], true);
		expect(next).toHaveLength(3); expect(next[0]).toBe(previous[0]); expect(next[1].close).toBe(10.5);
		expect(reconcileChartLines(previous, [...previous], true)[0]).toBe(previous[0]);
	});
	it('does not mix days or sources, and does not merge historical adjusted candle snapshots', () => {
		const previous = [bar('09:31'), bar('09:32')];
		expect(reconcileChartLines(previous, [{ ...bar('09:33'), time: '2026-10-01T09:33:00+08:00' }], true)).toHaveLength(1);
		expect(reconcileChartLines(previous, [{ ...bar('09:33'), meta: { ...bar('09:33').meta, source: 'another' } }], true)).toHaveLength(1);
		expect(reconcileChartLines(previous, [bar('09:32')], false)).toHaveLength(1);
	});
	it('preserves non-positive adjusted prices for historical candles only', () => {
		const adjusted = { ...bar('09:31', -1), open: -2, high: 1, low: -3 };
		expect(reconcileChartLines([], [adjusted], false)).toEqual([adjusted]);
		expect(reconcileChartLines([], [adjusted], true)).toEqual([]);
	});
	it('ignores older updates for an observed minute', () => {
		const previous = [bar('09:31')];
		const stale = { ...bar('09:31', 9), meta: { ...previous[0].meta, fetched_at: '2026-09-30T09:50:00+08:00' } };
		expect(reconcileChartLines(previous, [stale], true)[0]).toBe(previous[0]);
	});
});
describe('verified intraday weighted average', () => {
	it('uses cumulative amount and volume, accepting shares or lots', () => {
		expect(intradayAveragePrices([bar('09:31'), { ...bar('09:32'), volume: 300, amount: 3150 }])).toEqual([10, 10.375]);
		expect(intradayAveragePrices([{ ...bar('09:31'), amount: 100000 }])).toEqual([10]);
	});
	it('does not substitute a moving average when amount is missing or inconsistent', () => {
		expect(intradayAveragePrices([{ ...bar('09:31'), amount: 0 }, bar('09:32')])).toEqual([null, null]);
		expect(intradayAveragePrices([{ ...bar('09:31'), amount: 100 }])).toEqual([null]);
	});
});
