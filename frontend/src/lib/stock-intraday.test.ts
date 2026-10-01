import { describe, expect, it } from 'vitest';
import type { KLine } from './backend';
import { auctionFraction, intradaySampleCoverage, latestTradingSession, minuteText, shanghaiDayAndMinute, tradingFraction, tradingSessionForDay } from './stock-intraday';

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
	it('distinguishes a truncated first sample and interior gaps from the lunch and closing-auction grid', () => {
		const full = [...Array.from({ length: 120 }, (_, i) => 571 + i), ...Array.from({ length: 117 }, (_, i) => 781 + i), 900];
		expect(intradaySampleCoverage(full)).toMatchObject({ startsAtOpen: true, hasGaps: false, count: 238 });
		expect(intradaySampleCoverage([570, ...full])).toMatchObject({ startsAtOpen: true, hasGaps: false });
		expect(intradaySampleCoverage(full.filter(minute => minute !== 590)).hasGaps).toBe(true);
		expect(intradaySampleCoverage([815, 816, 817])).toMatchObject({ startsAtOpen: false, hasGaps: false, first: 815 });
		expect(intradaySampleCoverage([571, 573])).toMatchObject({ startsAtOpen: true, hasGaps: true });
		expect(intradaySampleCoverage([])).toMatchObject({ startsAtOpen: false, hasGaps: false, count: 0 });
		expect(minuteText(815)).toBe('13:35');
	});
	it('does not turn a masked numeric close into a verified minute sample', () => {
		const sample = { ...line('2026-09-30T09:31:00+08:00'), meta: { source: 'sina', fields_known: true, available_fields: ['volume'], fetched_at: '', stale: false, latency_ms: 0 } };
		expect(tradingSessionForDay([sample], '2026-09-30')).toEqual([]);
	});
});
