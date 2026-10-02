import type { SourceHealth, SourceProbeResult } from './backend';
import { sourceIntegrations, type SourceIntegration } from './source-integrations';

export function sourceProbeExpired(probe: SourceProbeResult, now: number) {
	const checkedAt = Date.parse(probe.checked_at);
	return !Number.isFinite(checkedAt) || now - checkedAt >= 10 * 60_000;
}

function isSourceProbe(value: unknown): value is SourceProbeResult {
	if (!value || typeof value !== 'object') return false;
	const probe = value as Partial<SourceProbeResult>;
	return typeof probe.id === 'string'
		&& (probe.status === 'available' || probe.status === 'unavailable')
		&& typeof probe.checked_at === 'string' && Number.isFinite(Date.parse(probe.checked_at))
		&& typeof probe.scope === 'string' && typeof probe.message === 'string'
		&& typeof probe.latency_ms === 'number' && Number.isFinite(probe.latency_ms) && probe.latency_ms >= 0;
}

function isSourceIntegration(value: unknown): value is SourceIntegration {
	if (!value || typeof value !== 'object') return false;
	const source = value as Partial<SourceIntegration>;
	return typeof source.id === 'string' && /^[a-z][a-z0-9_-]*$/.test(source.id)
		&& typeof source.name === 'string' && source.name.trim().length > 0
		&& ['public', 'credential', 'browser', 'archive'].includes(source.mode || '')
		&& Array.isArray(source.kinds) && source.kinds.length > 0 && source.kinds.every(kind => kind === 'market' || kind === 'information')
		&& typeof source.usage === 'string' && typeof source.configuration === 'string'
		&& Array.isArray(source.capabilities) && source.capabilities.every(capability => typeof capability === 'string')
		&& typeof source.probeScope === 'string' && typeof source.implemented === 'boolean' && typeof source.enabled === 'boolean';
}

// Settings routes use {data}, but source routes have their own top-level envelope.
export function parseSourceRecords(payload: unknown, requireProbes = false): { sources: SourceHealth[]; probes: SourceProbeResult[]; catalog: SourceIntegration[] } {
	if (!payload || typeof payload !== 'object' || !('sources' in payload) || !Array.isArray(payload.sources)) throw new Error('数据源观测响应格式异常');
	const catalog = 'catalog' in payload ? payload.catalog : sourceIntegrations;
	if (!Array.isArray(catalog) || ('catalog' in payload && !catalog.every(isSourceIntegration)) || new Set(catalog.map(source => source.id)).size !== catalog.length) throw new Error('数据源目录响应格式异常');
	const probes = 'probes' in payload ? payload.probes : undefined;
	if ((requireProbes && !Array.isArray(probes)) || (probes !== undefined && (!Array.isArray(probes) || !probes.every(isSourceProbe)))) throw new Error('数据源检测响应格式异常');
	const probeRecords: SourceProbeResult[] = Array.isArray(probes) ? probes : [];
	const probeSources = catalog.filter(source => source.implemented !== false && source.enabled !== false && source.probeScope !== '');
	if (requireProbes && (probeSources.some(source => probeRecords.filter(probe => probe.id === source.id).length !== 1) || probeRecords.some(probe => !probeSources.some(source => source.id === probe.id)))) throw new Error('数据源检测响应缺少完整且唯一的来源结果');
	if (requireProbes && (!('checked_at' in payload) || typeof payload.checked_at !== 'string' || !Number.isFinite(Date.parse(payload.checked_at)))) throw new Error('数据源检测响应格式异常');
	return { sources: payload.sources, probes: probeRecords, catalog };
}

// The implemented catalog defines capabilities; API records only describe
// observed requests. Older servers may still return placeholder providers.
export function normalizeSourceHealth(sources: SourceHealth[], catalog: readonly SourceIntegration[] = sourceIntegrations): SourceHealth[] {
	const observations = new Map(sources.map(source => [source.id, source]));
	return catalog.map(source => {
		const observed = observations.get(source.id);
		if (observed && observed.status !== 'unconfigured') return { ...observed, name: source.name };
		return { id: source.id, name: source.name, category: '', ok: false, status: 'unknown' };
	});
}

export function sourceHealthCounts(sources: SourceHealth[], catalog: readonly SourceIntegration[] = sourceIntegrations) {
	const implemented = normalizeSourceHealth(sources, catalog.filter(source => source.implemented !== false && source.enabled !== false));
	return {
		total: implemented.length,
		available: implemented.filter(source => source.status === 'available').length,
		degraded: implemented.filter(source => source.status === 'degraded').length,
		unknown: implemented.filter(source => source.status === 'unknown').length,
	};
}

// Keep the original timestamps when the settings drawer retains an old read.
// A failed read must not keep claiming a previous success is current forever.
export function expireSourceHealth(sources: SourceHealth[], now: number): SourceHealth[] {
	return sources.map(source => {
		const checkedAt = Date.parse(source.checked_at || '');
		if ((source.status === 'available' || source.status === 'degraded') && Number.isFinite(checkedAt) && now - checkedAt >= 10 * 60_000) {
			return { ...source, ok: false, status: 'unknown' };
		}
		return source;
	});
}

export function sourceHealthLabel(source: SourceHealth) {
	switch (source.status) {
		case 'available': return '最近可用';
		case 'degraded': return '最近失败 / 降级';
		case 'unconfigured': return '暂无业务观测';
		default: return source.checked_at ? '观测已过期' : '暂无业务观测';
	}
}

export function sourceHealthDetail(source: SourceHealth) {
	const checked = source.status !== 'unconfigured' && source.checked_at ? new Date(source.checked_at) : null;
	const checkedAt = checked && !Number.isNaN(checked.getTime()) ? checked.toLocaleString('zh-CN') : '';
	return [source.status !== 'unconfigured' ? source.message : '', checkedAt ? `最近观测 ${checkedAt}` : '本次服务启动后无实际请求记录'].filter(Boolean).join(' · ');
}

export function sourceCategoryLabel(category: string, sourceId?: string) {
	const labels: Record<string, string> = { theme: '题材', leaders: '龙头榜单', 'limit-up': '涨停池', 'limit-down': '跌停池', 'broken-limit-up': '炸板池', 'market-pools': '涨跌停 / 炸板池', concept: '概念归因', quote: '实时行情', kline: 'K 线', 'historical-intraday': '按日期历史分时', auction: '竞价参考', f10: '公司资料', business: '主营业务', fundamentals: '基本面', report: '研报', announcement: '公告', announcements: '公告', margin: '融资余额', billboard: '龙虎榜', 'money-flow': '资金流', index: '指数', sector: '行业强度', 'sector-stocks': '行业成分股', 'us-sector': '美股行业', 'billboard-labels': '龙虎榜席位标签', 'stock-directory': '股票目录', futures: '期指行情 / 持仓', 'futures-members': '期指会员排名', 'futures-consensus': '期指会员共识', news: '资讯', 'hot-ranks': '人气榜' };
	return category.split(',').map(item => item.trim() === 'futures' && sourceId === 'cffex' ? '期指单日持仓快照' : labels[item.trim()]).filter(Boolean).join(' · ');
}
