import { describe, expect, it } from 'vitest';
import type { SourceHealth } from './backend';
import { sourceHealthCounts, sourceHealthDetail, sourceHealthLabel } from './source-health';

const source = (status: SourceHealth['status']): SourceHealth => ({
	id: status, name: status, category: 'quote', ok: status === 'available', status,
});

describe('passive source observations', () => {
	it('keeps unobserved and unconfigured providers out of the available count', () => {
		const sources = [source('available'), source('degraded'), source('unknown'), source('unconfigured')];
		expect(sourceHealthCounts(sources)).toEqual({ available: 1, degraded: 1, unknown: 1, unconfigured: 1 });
		expect(sources.map(sourceHealthLabel)).toEqual(['最近可用', '最近失败', '未检测', '未接入']);
	});

	it('explains the observation without inventing a check time', () => {
		expect(sourceHealthDetail(source('unknown'))).toContain('无实际请求记录');
		expect(sourceHealthDetail({ ...source('degraded'), message: '实时更新失败', checked_at: '2026-09-29T07:00:00Z' })).toContain('实时更新失败');
	});
});
