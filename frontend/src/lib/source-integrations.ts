export type SourceKind = 'market' | 'information';
export type SourceIntegration = {
	id: string;
	name: string;
	mode: 'public' | 'credential' | 'browser' | 'archive';
	kinds: SourceKind[];
	usage: string;
	configuration: string;
	capabilities?: string[];
	probeScope?: string;
	implemented?: boolean;
	enabled?: boolean;
};

// Compatibility catalog for servers predating the registry response. Current
// servers supply their catalog; this list must not override that response.
export const sourceIntegrations: SourceIntegration[] = [
	{ id: 'duanxianxia', name: '短线侠 / 开盘啦', mode: 'public', kinds: ['market'], usage: '题材榜、龙头和涨停池', configuration: '内置公共接口，自动使用；至少 5 分钟刷新一次，无需填写凭据。' },
	{ id: 'eastmoney', name: '东方财富', mode: 'public', kinds: ['market', 'information'], usage: '集合竞价、目录与概念归属、资金补充、涨跌停与连板梯队、基本面、融资、龙虎榜、公告和研报、人气榜及期指持仓历史；已停用个股K、复权和指数取数', configuration: '仅剩余能力自动调用，无需填写 Cookie 或 Token；手动检测使用新客户端读取目录，不检测已停用价格接口。代表接口成功/失败都不代表所有行情与资讯功能。' },
	{ id: 'sina', name: '新浪财经', mode: 'public', kinds: ['market'], usage: '个股报价、分时、历史分时、K线和资金榜；公司主营/简介与已披露财务主源', configuration: '进入相应功能后自动调用公共接口；历史分时读取官网月档案，档案缺失时仅可回退同日近期样本并说明覆盖；K 线采用来源默认口径，不充当严格前 / 后复权备用。无需填写凭据。' },
	{ id: 'tencent', name: '腾讯财经', mode: 'public', kinds: ['market'], usage: '指数同标的优先、沪深股票日周月K与明确来源复权、行业强度及成分股、美股行业 ETF', configuration: '股票明确复权与指数取数不再回退东方财富；行业仍可按有效字段降级，具体以功能规则为准，无需填写凭据。' },
	{ id: 'cls', name: '财联社', mode: 'public', kinds: ['information'], usage: '市场快讯', configuration: '内置公开资讯接口，自动使用；无需填写凭据。' },
	{ id: 'ths', name: '同花顺', mode: 'public', kinds: ['market'], usage: '个股研究人气榜', configuration: '请求人气榜时自动调用公共接口；该榜不等价于其它平台人气排名，无需填写 Cookie 或 Token。' },
	{ id: 'cffex', name: '中国金融期货交易所', mode: 'public', kinds: ['market'], usage: '行情总览股指期货单日持仓快照、会员排名与共识', configuration: '打开股指期货或会员功能后自动读取交易所单日持仓数据；不提供价格、基差、实时盘口或完整历史曲线，无需填写凭据。' },
];

export function sourceIntegrationLabel(mode: SourceIntegration['mode']) {
	return { public: '已内置 · 自动使用', credential: '凭据接入', browser: '浏览器登录', archive: '归档内容' }[mode];
}

export function sourceKindLabel(kind: SourceKind) {
	return kind === 'market' ? '行情数据' : '资讯信息';
}

export function sourceName(sourceId: string, catalog: readonly SourceIntegration[] = sourceIntegrations): string {
	return [...new Set(sourceId.split('+').map(id => {
		const provider = id.trim().split(':')[0];
		// Retain historical aliases when an old report references a removed source.
		return catalog.find(source => source.id === provider)?.name || sourceIntegrations.find(source => source.id === provider)?.name || id.trim();
	}).filter(Boolean))].join(' + ');
}

export const sourceFallbackPolicies = [
	{ feature: '个股 K 线', chain: '来源默认：新浪 → 腾讯不复权（支持周期）；不复权 / 前复权 / 后复权：腾讯', boundary: '指定复权失败时不会混用备用口径；普通日 / 周 / 月 K 接口没有统一的服务器旧快照兜底，年 K 按实际复权口径聚合。' },
	{ feature: '个股实时行情、分时与竞价', chain: '报价：新浪；分时 / 分钟 K：新浪（目前无分钟备用）；竞价参考：东方财富（暂无已验证独立备用）', boundary: '有同股同日最近成功快照时可标记陈旧并展示；没有可用快照时显示不可用。竞价参考不是逐笔成交；旧行情不代表实时行情。' },
	{ feature: '市场指数 / 行业强度 / 资金榜', chain: '指数快照 / 日周月历史：腾讯独立；行业：腾讯 → 东方财富有效字段；资金榜：新浪 → 东方财富', boundary: '备用数据覆盖范围或字段可能较少；有模块旧快照时标记陈旧，否则该模块不可用，其余功能仍可使用。' },
	{ feature: '趋势题材', chain: '行业数据与开盘啦题材融合，缺一方时使用其余有效数据', boundary: '开盘啦旧题材超过两个交易日不参与融合；渐进页面可保留旧快照并显示失败步骤。所有来源都失效且无可用快照时不可用。' },
	{ feature: '涨停池 / 连板梯队', chain: '开盘啦优先，东方财富补充历史梯队与缺失字段；炸板池 / 跌停池：东方财富', boundary: '缓存有刷新间隔，来源失败时旧记录须查看时间与降级说明；不同平台缺失字段不会伪造。' },
	{ feature: '人气榜', chain: '同花顺 + 东方财富', boundary: '按平台展示排名与可用性，一家失败仍展示另一家；旧快照标记陈旧，不能冒充实时双源榜单。' },
	{ feature: '股指期货', chain: '趋势价格 / 历史 / 基差：东方财富 → 中金所单日持仓；会员排名与共识：中国金融期货交易所', boundary: '中金所回退只有单日持仓快照，不能补齐价格、基差或完整历史曲线；没有有效数据时不可用。' },
	{ feature: '公司主营与财务', chain: '新浪 → 东方财富（失败或覆盖不足整份回退）', boundary: '报告期和披露日期分开；银行毛利不适用，未知不作零。主源完整不请求东财，回退保留实际来源，不跨源或报告期补字段。' },
	{ feature: '公告、研报、融资、原始龙虎榜', chain: '东方财富；当前没有统一备用供应商', boundary: '行情总览模块有最近成功快照时标记陈旧并展示；无快照时该模块不可用，不以行情或其他模块数据替代。' },
	{ feature: '市场快讯', chain: '财联社；当前没有备用供应商', boundary: '页面可能保留已加载内容，无内容时快讯不可用。' },
];
