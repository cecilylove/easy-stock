export type SourceIntegration = { id: string; name: string; mode: 'public' | 'reserved' | 'unimplemented'; usage: string; configuration: string };

export const sourceIntegrations: SourceIntegration[] = [
	{ id: 'duanxianxia', name: '短线侠 / 开盘啦', mode: 'public', usage: '题材榜、龙头和涨停池', configuration: '内置公共接口，自动使用；至少 5 分钟刷新一次，无需填写凭据。' },
	{ id: 'eastmoney', name: '东方财富', mode: 'public', usage: 'K 线、指数、资金及研究信息', configuration: '公共接口已内置，无需 Cookie；下方 Cookie 仅为尚未实现的登录态增强预留。' },
	{ id: 'sina', name: '新浪财经', mode: 'public', usage: '实时报价、K 线回退和资金榜', configuration: '内置公共接口，自动使用；无需填写 Token 或 Cookie。' },
	{ id: 'tencent', name: '腾讯财经', mode: 'public', usage: '行业强度及指数回退', configuration: '内置公共接口，自动使用；无需填写 Token 或 Cookie。' },
	{ id: 'cls', name: '财联社', mode: 'public', usage: '市场快讯', configuration: '内置公开资讯接口，自动使用；无需填写凭据。' },
	{ id: 'tushare', name: 'Tushare', mode: 'reserved', usage: '计划提供基础资料和日线增强', configuration: '当前没有取数实现；下方 Token 仅保存备用，填写后不会接入，也不会参与失败回退。' },
	{ id: 'ths', name: '同花顺', mode: 'reserved', usage: '计划提供题材催化与涨停原因', configuration: '当前没有取数实现；下方 Cookie / Token 仅保存备用，不参与行情或题材聚合。' },
	{ id: 'tradingview', name: 'TradingView', mode: 'unimplemented', usage: '计划提供资讯', configuration: '当前没有取数实现和配置入口，需开发对应服务后才能接入。' },
];

export function sourceIntegrationLabel(mode: SourceIntegration['mode']) {
	return mode === 'public' ? '已内置 · 自动使用' : mode === 'reserved' ? '未接入 · 仅预留凭据' : '未接入 · 尚无配置';
}

export const sourceFallbackPolicies = [
	{ feature: '个股 K 线', chain: '东方财富 → 新浪', boundary: '两源均失败时该周期可能不可用；普通日 / 周 / 月 K 接口没有统一的服务器旧快照兜底。' },
	{ feature: '个股实时行情、分时', chain: '报价使用新浪；分时 K 线东方财富 → 新浪', boundary: '个股详情可复用同股同日最近成功快照，失败后退避 30–120 秒并标记陈旧；没有快照时显示不可用。' },
	{ feature: '市场指数', chain: '东方财富 → 腾讯', boundary: '备用来源覆盖范围较少；两源均失败时尝试已有模块快照，否则不可用。' },
	{ feature: '行业强度 / 资金榜', chain: '行业：腾讯 → 东方财富；资金榜：新浪 → 东方财富', boundary: '备用数据可能缺字段；两源均失败时尝试已有模块快照，否则不可用。' },
	{ feature: '趋势题材', chain: '行业数据与开盘啦题材融合，缺一方时使用其余有效数据', boundary: '开盘啦旧题材超过两个交易日不参与融合；渐进页面可保留旧快照并显示失败步骤。所有来源都失效且无可用快照时不可用。' },
	{ feature: '融资余额、龙虎榜、公告 / 研报', chain: '东方财富；当前没有统一备用供应商', boundary: '行情总览模块有最近成功快照时标记陈旧并展示；无快照时该模块不可用。' },
	{ feature: '市场快讯', chain: '财联社；当前没有备用供应商', boundary: '失败时不切换到未接入来源；页面可能保留已加载内容，无内容时快讯不可用。' },
];
