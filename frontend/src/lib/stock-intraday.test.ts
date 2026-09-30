import { describe, expect, it } from 'vitest';
import type { KLine } from './backend';
import { auctionFraction, latestTradingSession, shanghaiDayAndMinute, tradingFraction, tradingSessionForDay } from './stock-intraday';

const line = (time: string) => ({ time, close: 10, open: 10, high: 10, low: 10, volume: 1 }) as KLine;

describe('fixed session time axis', () => {
	it('keeps 9:48 near the beginning instead of stretching it across the chart', () => {
		expect(tradingFraction(9 * 60 + 30)).toBe(0);
		expect(tradingFraction(9 * 60 + 48)).toBeCloseTo(18 / 240);
		expect(tradingFraction(11 * 60 + 30)).toBeCloseTo(0.5);
		expect(tradingFraction(13 * 60)).toBeCloseTo(0.5);
		expect(tradingFraction(15 * 60)).toBe(1);
		expect(tradingFraction(12 * 60)).toBeNull();
	});
	it('isolates 09:15–09:25 auction reference time from continuous trade', () => {
		expect(auctionFraction(9 * 60 + 15)).toBe(0);
		expect(auctionFraction(9 * 60 + 25)).toBe(1);
		expect(auctionFraction(9 * 60 + 26)).toBeNull();
	});
	it('uses Shanghai time and does not mix trading days', () => {
		expect(shanghaiDayAndMinute('2026-09-30T01:48:00Z')).toEqual({ day: '2026-09-30', minute: 588 });
		const result = latestTradingSession([
			line('2026-09-29T09:31:00+08:00'), line('2026-09-30T09:31:00+08:00'),
			line('2026-09-30T12:00:00+08:00'), line('2026-09-30T13:01:00+08:00'),
		]);
		expect(result.map(item => item.instant.minute)).toEqual([571, 781]);
		expect(tradingSessionForDay([line('2026-09-29T09:31:00+08:00')], '2026-09-30')).toEqual([]);
		expect(tradingSessionForDay([line('2026-09-30T09:31:00+08:00')], '2026-09-30')).toHaveLength(1);
		expect(tradingSessionForDay([
			{ ...line('2026-09-30T09:31:00+08:00'), close: 0 },
			{ ...line('2026-09-30T09:32:00+08:00'), close: Number.NaN },
			{ ...line('2026-09-30T09:33:00+08:00'), volume: Number.POSITIVE_INFINITY },
		], '2026-09-30')).toEqual([]);
	});
});
