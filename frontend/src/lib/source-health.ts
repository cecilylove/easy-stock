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
		case 'degraded': return '最近失败 / 降级';
		case 'unconfigured': return '未接入';
		default: return source.checked_at ? '观测已过期' : '未检测';
	}
}

export function sourceHealthDetail(source: SourceHealth) {
	const checked = source.checked_at ? new Date(source.checked_at) : null;
	const checkedAt = checked && !Number.isNaN(checked.getTime()) ? checked.toLocaleString('zh-CN') : '';
	return [source.message, checkedAt ? `最近观测 ${checkedAt}` : source.status === 'unconfigured' ? '当前版本未接入' : '本次服务启动后无实际请求记录'].filter(Boolean).join(' · ');
}

export function sourceCategoryLabel(category: string) {
	const labels: Record<string, string> = { theme: '题材', leaders: '龙头榜单', 'limit-up': '涨停池', concept: '概念归因', quote: '实时行情', kline: 'K 线', f10: '公司资料', report: '研报', 'money-flow': '资金流', index: '指数', hk: '港股', news: '资讯', calendar: '日历', basic: '基础资料', daily: '日线' };
	return category.split(',').map(item => labels[item.trim()] || item.trim()).filter(Boolean).join(' · ');
}
