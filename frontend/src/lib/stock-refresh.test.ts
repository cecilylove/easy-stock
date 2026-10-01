import { describe, expect, it } from 'vitest';
import { refreshInterval } from './stock-refresh';

const china = (day: string, hour: number, minute: number) => new Date(`${day}T${hour.toString().padStart(2, '0')}:${minute.toString().padStart(2, '0')}:00+08:00`);

describe('stock detail automatic refresh schedule', () => {
	it('refreshes auction only during 09:15–09:25 and trades during sessions', () => {
		expect(refreshInterval('auction', china('2026-09-30', 9, 15))).toBe(5_000);
		expect(refreshInterval('auction', china('2026-09-30', 9, 25))).toBe(5_000);
		expect(refreshInterval('auction', china('2026-09-30', 9, 26))).toBe(5_000);
		expect(refreshInterval('auction', china('2026-09-30', 9, 29))).toBe(5_000);
		expect(refreshInterval('auction', china('2026-09-30', 9, 30))).toBeNull();
		expect(refreshInterval('intraday', china('2026-09-30', 9, 29))).toBeNull();
		expect(refreshInterval('intraday', china('2026-09-30', 9, 30))).toBe(5_000);
		expect(refreshInterval('quote', china('2026-09-30', 14, 15))).toBe(5_000);
		expect(refreshInterval('kline', china('2026-09-30', 14, 15))).toBe(60_000);
		expect(refreshInterval('kline', china('2026-09-30', 9, 25))).toBeNull();
	});
	it('pauses for lunch, after the close, and weekends', () => {
		for (const date of [china('2026-09-30', 12, 1), china('2026-09-30', 15, 1), china('2026-10-03', 9, 23)]) {
			expect(refreshInterval('quote', date)).toBeNull();
			expect(refreshInterval('auction', date)).toBeNull();
			expect(refreshInterval('intraday', date)).toBeNull();
			expect(refreshInterval('kline', date)).toBeNull();
		}
	});
});
