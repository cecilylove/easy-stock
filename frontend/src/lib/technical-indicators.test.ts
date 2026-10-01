import { describe, expect, it } from 'vitest';
import type { KLine } from './backend';
import { calculateIndicators, movingAverage, resolveChartWindow } from './technical-indicators';
const bar = (index: number, close = 10) => ({ time: new Date(Date.UTC(2026, 0, index + 1)).toISOString(), open: close, high: close + 1, low: close - 1, close, volume: 100 } as KLine);
describe('technical indicator sample calculations', () => {
	it('does not invent MA history before enough samples exist', () => {
		expect(movingAverage([1, 2, 3, 4, 5, 6], 5)).toEqual([null, null, null, null, 3, 4]);
	});
	it('has zero MACD and neutral KDJ on a constant symmetrical price series', () => {
		const data = calculateIndicators(Array.from({ length: 80 }, (_, index) => bar(index)));
		expect(data.macd.every(value => value.dif === 0 && value.dea === 0 && value.histogram === 0)).toBe(true);
		expect(data.kdj.slice(0, 8).every(value => value === null)).toBe(true);
		expect(data.kdj.at(-1)).toEqual({ k: 50, d: 50, j: 50 });
		expect(data.ma[3].values[58]).toBeNull(); expect(data.ma[3].values[59]).toBe(10);
	});
	it('retains a historical viewport anchor while a new bar arrives', () => {
		const data = Array.from({ length: 120 }, (_, index) => bar(index));
		const anchor = data[80].time;
		expect(resolveChartWindow(data, 30, anchor)).toEqual({ start: 51, end: 80, count: 30 });
		expect(resolveChartWindow([...data, bar(120)], 30, anchor)).toEqual({ start: 51, end: 80, count: 30 });
		expect(resolveChartWindow(data, 30, null)).toEqual({ start: 90, end: 119, count: 30 });
	});
});
