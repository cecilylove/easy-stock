import { describe, expect, it } from 'vitest';
import type { SourceHealth } from './backend';
import { expireSourceHealth, normalizeSourceHealth, parseSourceRecords, sourceCategoryLabel, sourceHealthCounts, sourceHealthDetail, sourceHealthLabel } from './source-health';
import { sourceIntegrations, type SourceIntegration } from './source-integrations';

const source = (id: string, status: SourceHealth['status']): SourceHealth => ({
	id, name: id, category: 'quote', ok: status === 'available', status,
});

describe('registry source catalog', () => {
	const vendor: SourceIntegration = { id: 'new_vendor', name: '新的行情供应商', mode: 'public', kinds: ['market'], usage: '报价', configuration: '自动取数', capabilities: ['quote'], probeScope: '报价代表接口', implemented: true, enabled: true };
	const checked_at = '2026-10-02T01:00:00Z';
	const probe = { id: vendor.id, status: 'available', checked_at, scope: vendor.probeScope, message: '有效报价', latency_ms: 1 };
	it('uses the registered providers for observations and complete probes after adding and removing a source', () => {
		const payload = { catalog: [vendor], sources: [source(vendor.id, 'available'), source('sina', 'available')], probes: [probe], checked_at };
		const records = parseSourceRecords(payload, true);
		expect(normalizeSourceHealth(records.sources, records.catalog)).toEqual([{ ...source(vendor.id, 'available'), name: vendor.name }]);
		expect(sourceHealthCounts(records.sources, records.catalog)).toEqual({ total: 1, available: 1, degraded: 0, unknown: 0 });
		expect(() => parseSourceRecords({ ...payload, probes: [] }, true)).toThrow('缺少完整且唯一');
		expect(() => parseSourceRecords({ ...payload, probes: [probe, probe] }, true)).toThrow('缺少完整且唯一');
		expect(() => parseSourceRecords({ ...payload, probes: [probe, { ...probe, id: 'sina' }] }, true)).toThrow('缺少完整且唯一');
	});
	it('accepts an empty registry without resurrecting the compatibility catalog', () => {
		const records = parseSourceRecords({ catalog: [], sources: [], probes: [], checked_at }, true);
		expect(records.catalog).toEqual([]); expect(normalizeSourceHealth([], records.catalog)).toEqual([]);
	});
	it('does not require probes for disabled, unimplemented or non-probe sources', () => {
		const catalog = [{ ...vendor, enabled: false }, { ...vendor, id: 'planned', implemented: false }, { ...vendor, id: 'archive', mode: 'archive' as const, probeScope: '' }];
		expect(parseSourceRecords({ catalog, sources: [], probes: [], checked_at }, true).catalog).toEqual(catalog);
	});
	it('rejects malformed and duplicate catalog entries instead of falling back silently', () => {
		for (const catalog of [null, [vendor, vendor], [{ ...vendor, enabled: 'true' }], [{ ...vendor, capabilities: 'quote' }], [{ ...vendor, id: '' }]]) {
			expect(() => parseSourceRecords({ catalog, sources: [] })).toThrow('目录响应格式异常');
		}
	});
	it('retains the compatibility catalog only when the server does not provide a catalog', () => {
		expect(parseSourceRecords({ sources: [] }).catalog).toBe(sourceIntegrations);
	});
});

describe('passive source observations', () => {
	it('counts the implemented catalog and ignores legacy placeholders and duplicate records', () => {
		const sources = [source('sina', 'available'), source('tencent', 'degraded'), source('eastmoney', 'available'), source('cls', 'unknown'), source('tushare', 'available'), source('tradingview', 'unconfigured'), source('sina', 'available')];
		expect(sourceHealthCounts(sources)).toEqual({ total: sourceIntegrations.length, available: 2, degraded: 1, unknown: sourceIntegrations.length - 3 });
		expect(normalizeSourceHealth(sources).map(item => item.id)).toEqual(sourceIntegrations.map(item => item.id));
	});
	it('provides unobserved catalog rows before the API responds without inventing availability or observation times', () => {
		const normalized = normalizeSourceHealth([]);
		expect(normalized).toHaveLength(sourceIntegrations.length);
		for (const item of normalized) {
			expect(item.status).toBe('unknown'); expect(item.ok).toBe(false);
			expect(item.checked_at).toBeUndefined(); expect(sourceHealthLabel(item)).toBe('暂无业务观测');
		}
		expect(sourceHealthCounts([])).toEqual({ total: sourceIntegrations.length, available: 0, degraded: 0, unknown: sourceIntegrations.length });
	});
	it('treats an old backend THS placeholder as missing observation, not a failed integration', () => {
		const legacy = { ...source('ths', 'unconfigured'), checked_at: '2026-09-30T01:00:00Z', message: '当前没有接入' };
		const current = normalizeSourceHealth([legacy]).find(item => item.id === 'ths')!;
		expect(current.name).toBe('同花顺'); expect(current.status).toBe('unknown');
		expect(current.checked_at).toBeUndefined(); expect(current.message).toBeUndefined();
		expect(sourceHealthLabel(legacy)).toBe('暂无业务观测'); expect(sourceHealthDetail(legacy)).not.toContain('当前没有接入');
	});
	it('preserves expired observations and their success/failure history', () => {
		const expired = { ...source('sina', 'unknown'), checked_at: '2026-09-30T01:00:00Z', last_success: '2026-09-30T01:00:00Z', message: '最近观测已过期，等待实际请求' };
		const normalized = normalizeSourceHealth([expired]).find(item => item.id === 'sina')!;
		expect(normalized).toMatchObject({ ...expired, name: '新浪财经' });
		expect(sourceHealthLabel(normalized)).toBe('观测已过期');
		expect(sourceHealthDetail(normalized)).toContain('最近观测'); expect(sourceHealthDetail(normalized)).not.toContain('无实际请求记录');
	});

	it('explains the observation without inventing a check time', () => {
		expect(sourceHealthDetail(source('cls', 'unknown'))).toContain('无实际请求记录');
		expect(sourceHealthDetail({ ...source('eastmoney', 'degraded'), message: '实时更新失败', checked_at: '2026-09-29T07:00:00Z' })).toContain('实时更新失败');
		expect(sourceHealthLabel(source('eastmoney', 'degraded'))).toBe('最近失败 / 降级');
		expect(sourceCategoryLabel('quote,hot-ranks,billboard-labels')).toBe('实时行情 · 人气榜 · 龙虎榜席位标签');
		expect(sourceCategoryLabel('futures', 'cffex')).toBe('期指单日持仓快照'); expect(sourceCategoryLabel('futures', 'eastmoney')).toBe('期指行情 / 持仓');
	});
	it('translates every category exposed by the implemented backend sources', () => {
		const categories = 'kline,auction,stock-directory,concept,business,fundamentals,index,sector,money-flow,limit-up,market-pools,margin,billboard,announcement,report,hot-ranks,futures,futures-members,futures-consensus,sector-stocks,us-sector,news,billboard-labels';
		for (const category of categories.split(',')) expect(sourceCategoryLabel(category)).not.toBe('');
	});
	it('drops old placeholder and unknown capabilities rather than displaying raw identifiers', () => {
		expect(sourceCategoryLabel('calendar,basic,daily,hk,new-placeholder')).toBe('');
		expect(sourceCategoryLabel('news,calendar')).toBe('资讯');
	});
	it('expires retained success without renewing or losing its real timestamp', () => {
		const checked_at = '2026-10-01T01:00:00Z';
		const observed = { ...source('sina', 'available'), checked_at, last_success: checked_at };
		expect(expireSourceHealth([observed], Date.parse(checked_at) + 599_999)[0].status).toBe('available');
		const expired = expireSourceHealth([observed], Date.parse(checked_at) + 600_000)[0];
		expect(expired).toMatchObject({ status: 'unknown', ok: false, checked_at, last_success: checked_at });
		expect(observed.status).toBe('available');
	});
});
