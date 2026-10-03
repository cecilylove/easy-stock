package registry

// DefaultDescriptors describes implemented integrations, not reserved credential
// fields. Tushare and TradingView are deliberately absent.
func DefaultDescriptors() []Descriptor {
	return []Descriptor{
		{ID: "duanxianxia", Name: "短线侠 / 开盘啦", Mode: "public", Kinds: []string{"market"}, Usage: "题材榜、龙头和涨停池", Configuration: "内置公共接口，自动使用；至少 5 分钟刷新一次，无需填写凭据。", Capabilities: []string{"theme"}, ProbeScope: "开盘啦直接涨停池", Implemented: true, Enabled: true},
		{ID: "eastmoney", Name: "东方财富", Mode: "public", Kinds: []string{"market", "information"}, Usage: "集合竞价、目录与概念归属、资金补充、涨跌停与连板梯队、基本面、融资、龙虎榜、公告和研报、人气榜及期指持仓历史；已停用个股K、复权和指数取数", Configuration: "仅剩余能力自动调用，无需填写 Cookie 或 Token；手动检测使用新客户端读取目录，不检测已停用价格接口。代表接口成功/失败都不代表所有行情与资讯功能。", Capabilities: []string{"auction", "stock-directory", "boards", "business", "fundamentals", "industry", "fund-flow", "limit-up", "market-pools", "margin", "billboard", "announcements", "reports", "hot-ranks", "futures-history"}, ProbeScope: "股票目录与名称代表接口（保留能力；不检测已停用K线）", Implemented: true, Enabled: true},
		{ID: "sina", Name: "新浪财经", Mode: "public", Kinds: []string{"market"}, Usage: "个股报价/分时/历史分时/K线与资金榜；公司主营/简介和最新已披露财务主源", Configuration: "进入相应功能后调用公共接口；公司主营/简介与财务默认新浪，失败/覆盖不足明确整份回退东财，不跨源拼字段。财务披露日与报告期分开，银行毛利不适用；历史分时读月档案，K来源默认不替代严格复权。无需填写凭据。", Capabilities: []string{"quote", "kline", "intraday", "historical-intraday", "fund-flow", "stock-directory", "business", "fundamentals"}, ProbeScope: "平安银行代表行情（000001.SZ）", Implemented: true, Enabled: true},
		{ID: "tencent", Name: "腾讯财经", Mode: "public", Kinds: []string{"market"}, Usage: "指数同标的优先、沪深股票日周月K与明确来源复权、行业强度及成分股、美股行业 ETF", Configuration: "股票明确复权与指数取数不再回退东方财富；行业仍可按有效字段降级，具体以功能规则为准，无需填写凭据。", Capabilities: []string{"index", "kline", "adjusted-kline", "industry", "board-members", "us-sector"}, ProbeScope: "上证指数代表行情", Implemented: true, Enabled: true},
		{ID: "cls", Name: "财联社", Mode: "public", Kinds: []string{"information"}, Usage: "市场快讯", Configuration: "内置公开资讯接口，自动使用；无需填写凭据。", Capabilities: []string{"news"}, ProbeScope: "财联社最新电报", Implemented: true, Enabled: true},
		{ID: "ths", Name: "同花顺", Mode: "public", Kinds: []string{"market"}, Usage: "个股研究人气榜、龙虎榜公开席位标签补充", Configuration: "人气榜和龙虎榜标签是独立路由；标签失败或禁用不影响原始买卖明细，平台分类不代表监管确认资金身份，无需填写 Cookie 或 Token。", Capabilities: []string{"hot-ranks", "billboard-labels"}, ProbeScope: "同花顺公开个股热榜", Implemented: true, Enabled: true},
		{ID: "cffex", Name: "中国金融期货交易所", Mode: "public", Kinds: []string{"market"}, Usage: "行情总览股指期货单日持仓快照、会员排名与共识", Configuration: "打开股指期货或会员功能后自动读取交易所单日持仓数据；不提供价格、基差、实时盘口或完整历史曲线，无需填写凭据。", Capabilities: []string{"futures-snapshot", "futures-members", "futures-consensus"}, ProbeScope: "中金所IF最近交易日单日持仓快照", Implemented: true, Enabled: true},
	}
}
func Default() *Registry {
	entries := []Entry{}
	for _, d := range DefaultDescriptors() {
		entries = append(entries, Entry{Descriptor: d})
	}
	r, _ := New(entries...)
	return r
}
