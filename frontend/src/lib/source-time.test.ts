import { describe, expect, it, vi } from 'vitest';
import { formatIndexHistoryDate, formatIndexTradeTime } from './source-time';
import type { SourceMeta } from './backend';
const meta = (zone?: string): SourceMeta => ({ source: 'tencent', fetched_at: '', latency_ms: 0, stale: false, time_zone: zone });

describe('index source-time contract', () => {
	it.each([undefined, '', '0001-01-01T00:00:00Z', 'bad-time'])('labels absent or invalid instant %s unknown', value => {
		expect(formatIndexTradeTime(value, meta('Asia/Shanghai'))).toBe('未知');
	});
	it('retains provider wall time only as unconfirmed-offset evidence', () => {
		expect(formatIndexTradeTime('0001-01-01T00:00:00Z', { ...meta('unknown'), native_timestamp: '20261001093000' })).toBe('未知（来源时间 20261001093000；时区偏移未确认）');
		expect(formatIndexTradeTime('2026-10-01T09:30:00Z', meta('unknown'))).toBe('未知（时区偏移未确认）');
	});
	it('formats confirmed zones explicitly and keeps legacy timestamp offset text', () => {
		expect(formatIndexTradeTime('2026-10-01T01:30:00Z', meta('Asia/Shanghai'))).toContain('09:30:00');
		expect(formatIndexTradeTime('2026-10-01T01:30:00Z', meta())).toBe('2026-10-01T01:30:00Z');
		expect(formatIndexTradeTime('2026-10-01T01:30:00Z', meta('invalid-zone'))).toBe('未知（时区未确认）');
	});
	it('uses the provider trading-date label without host locale conversion', () => {
		const spy = vi.spyOn(Date.prototype, 'toLocaleDateString').mockReturnValue('09-30');
		try {
			expect(formatIndexHistoryDate('2026-10-01T00:00:00Z')).toBe('10-01');
			expect(formatIndexHistoryDate('2026-10-01T00:00:00+08:00')).toBe('10-01');
			expect(spy).not.toHaveBeenCalled();
			expect(formatIndexHistoryDate('0001-01-01T00:00:00Z')).toBe('--');
		} finally { spy.mockRestore(); }
	});
});
