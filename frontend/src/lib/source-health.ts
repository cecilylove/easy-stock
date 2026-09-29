import type { SourceHealth } from './backend';

export function sourceHealthCounts(sources: SourceHealth[]) {
	return {
		available: sources.filter(source => source.status === 'available').length,
		degraded: sources.filter(source => source.status === 'degraded').length,
		unknown: sources.filter(source => source.status === 'unknown').length,
		unconfigured: sources.filter(source => source.status === 'unconfigured').length,
	};
}

export function sourceHealthLabel(source: SourceHealth) {
	switch (source.status) {
		case 'available': return '最近可用';
		case 'degraded': return '最近失败';
		case 'unconfigured': return '未接入';
		default: return '未检测';
	}
}

export function sourceHealthDetail(source: SourceHealth) {
	const checked = source.checked_at ? new Date(source.checked_at) : null;
	const checkedAt = checked && !Number.isNaN(checked.getTime()) ? checked.toLocaleString('zh-CN') : '';
	return [source.message, checkedAt ? `最近观测 ${checkedAt}` : '无实际请求记录'].filter(Boolean).join(' · ');
}
