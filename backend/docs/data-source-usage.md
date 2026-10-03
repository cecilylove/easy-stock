# 重构后数据源使用清单

核对日期：2026-10-03。代码基线：`c3c061b`。本文按当前默认启动装配整理，描述实际代码链路，不是拟接入清单，也不表示本次验证了公网可用性。自定义 `Config` 注入可覆盖默认能力。

默认装配来源：`internal/datasource/assembly/defaults.go`、`content.go`、`routes.go`；实际业务接线：`internal/httpapi/server.go`；启动入口：`cmd/server/main.go`。运行规则另见 [数据源与取数边界](data-sources.md)，架构和保留治理项见 [当前架构](architecture.md)、[改造清单](../../docs/data-source-refactor-checklist.md)。

面向功能理解、冗余判断及关闭来源后的影响，见 [功能与数据源精简评估](../../docs/data-source-consolidation-assessment.md)。

## 1. 分类与登记总览

业务上分为行情/市场事实、资讯/文章/知识两类；注册分组则是七家公共接口来源和五项内容集成，共十二项。两种分组不是一一对应：财联社是公共资讯接口，东方财富同时提供行情和资讯；开盘啦与短线侠共用一个 `duanxianxia` 来源 ID，不能重复计数。

| 来源 ID | 来源 | 当前实际用途 | 默认使用状态 |
| --- | --- | --- | --- |
| `sina` | 新浪财经 | 实时报价/五档盘口、分钟及日周月 K、当日与历史分时、资金榜、公司主营/简介与已披露财务；另有股票目录实现 | 行情/资金/公司资料财务主源；目录已注册但未选作默认或自动备用 |
| `tencent` | 腾讯财经 | 指数快照/历史、股票日周月 K/明确复权、研究基准指数、行业强度/原生成员、美股行业 ETF 代理 | 指数与明确复权唯一默认来源；股票 K 备用，行业主源 |
| `duanxianxia` | 短线侠 / 开盘啦 | 题材榜、领涨股、涨停池、连板及逐股概念归因 | 默认题材源；持久服务启用时作为涨停合并主输入 |
| `eastmoney` | 东方财富 | 竞价、股票目录/概念、主营/基本面、行业/资金补充、板块/成员、涨停/跌停/炸板、融资、龙虎榜、公告/研报、热榜、期指历史 | 公司资料/财务改为明确备用，其余保留；股票 K、明确复权和指数已退役 |
| `cls` | 财联社 | 市场电报/快讯 | 默认快讯来源，无已接入备用 |
| `ths` | 同花顺 | 独立热榜；龙虎榜席位平台标签补充 | 热榜并行；席位标签独立注册/路由，由服务组合原始明细，失败/禁用不破坏明细 |
| `cffex` | 中国金融期货交易所 | 最近交易日持仓快照、指定合约/日期会员排名、四品种持仓共识 | 期指历史失败时提供单日降级；会员/共识直接使用 |
| `xueqiu` | 雪球 | 已知文章链接、作者文章采集/订阅 | 公开文章解析或经授权浏览器采集；登记不代表已登录 |
| `taoguba` | 淘股吧 | 已知文章链接、作者文章采集/订阅 | 公开文章解析或经授权浏览器采集；兼容 `taoguba.com.cn`、`tgb.cn` |
| `wechat` | 微信公众号 | 已知文章链接正文、授权文章服务接口 | 已知链接导入可用；自动公众号订阅未启用 |
| `official` | 预整理每日复盘 | 远程作者清单、按作者/日期发布的复盘正文 | 远程归档同步；名称不表示政策公告或原平台实时抓取 |
| `githubknowledge` | GitHub 知识资料 | 游资心法目录、Markdown 文档 | 心法库、提示上下文与 Hermes 同步；使用本地种子/缓存 |

## 2. 行情与市场能力的实际路由

下表契约路径相对 `internal/datasource/contracts/`；策略路径相对 `internal/datasource/service/`。一个供应商只需实现被绑定的小接口，不必实现整个市场总览门面。

| 数据能力 | 契约方法 | 当前默认来源/策略 | 业务用途及关键边界 |
| --- | --- | --- | --- |
| 实时报价、五档盘口 | `RealtimeProvider.Realtime` | 新浪；`Access` 单源访问 | 个股详情、自选/行情刷新、WS、题材补充、研究与验证；没有自动腾讯报价备用 |
| 集合竞价参考轨迹 | `AuctionProvider.AuctionTrace` | 东方财富；单源 | 个股竞价；失败仅允许同股同日旧参考点，不补造轨迹 |
| 普通股票 K | `KLineProvider.KLine` | `PriceService`：新浪 → 腾讯 | 新浪来源默认口径；腾讯备用限已支持股票日/周/月，使用 none；不拼接两家历史 |
| 明确复权 K | `AdjustedKLineProvider.KLineAdjusted` | 腾讯；`none/qfq/hfq` 精确系列 | 明确请求这些模式且未指定 provider 时默认腾讯；provider 和 adjust 同时缺省仍走普通链路；失败不借新浪来源默认替代；腾讯不声称支持北交所/分钟股票复权 |
| 年 K | 业务聚合 `price_year.go` | 同源、同口径月 K 聚合 | 没有单独年 K 供应商；缺字段保留缺失 |
| 当日分时/近期分钟采样 | `Intraday` 槽位复用 `KLineProvider` | 新浪 | 个股分时；五日图为 5 分钟采样，不是单独五日分时接口 |
| 指定日期历史分时 | `HistoryIntradayProvider.HistoryIntraday` | 新浪历史月档案 → 同日近期样本 | 档案失败可降级；不能用今天替代历史日；档案明确缺日不伪恢复 |
| 市场指数快照及日周月历史 | `IndexProvider.MarketIndexes/MarketIndexSeries` | 腾讯独立指数链路 | 市场总览、复盘；不自动使用东方财富补缺；全球未覆盖项保留缺失 |
| 研究基准/交易日历 K | 价格能力中的独立指数适配 | 默认 K 路由；腾讯备用按明确研究指数映射 | 腾讯映射上证、沪深300、上证50、中证1000、科创50、深证成指、创业板指；北交所研究还请求北证50，但无对应腾讯备用映射；不把指数当股票处理 |
| 行业强度 | `IndustryProvider.IndustryMomentum` | 腾讯 → 东方财富有效字段 | 市场总览、行业/题材融合、研究/复盘；只有有效字段才参与排序和评分 |
| 原生行业成分 | `BoardMemberProvider.SupportsMembers/Members` | 腾讯；按 `BoardRef` 的来源/原生码/维度选择支持者 | 不是任意来源失败后的重试备用链；纯行业和融合成员默认上限 500，返回完整性，不把 topN 当全集；东财 BK 不能传给腾讯 |
| 板块搜索与概念成分 | `BoardProvider.Boards/BoardStocks` | 东方财富 | 题材映射、成分补充/回退；与腾讯原生行业成员是不同链路 |
| 行业/题材/个股资金榜 | `FundFlowProvider.MarketFundFlows` | 新浪 → 东方财富有效字段 | 市场总览/研究/复盘；板块总净额与主力净额不等价，记录字段及排序降级 |
| 融资余额 | `MarginProvider.MarketMarginSeries` | 东方财富 | 市场总览；不是完整两融账户/融券能力 |
| 龙虎榜榜单/明细 | `BillboardProvider.MarketBillboard/MarketBillboardDetail` | 东方财富；明细另补同花顺标签 | `BillboardLabels`独立路由在访问服务补充同花顺标签；禁用/失败保留东财买卖明细，标签只是平台分类口径，非监管确认的资金身份 |
| 公告 | `AnnouncementProvider.MarketAnnouncements` | 东方财富 | 公告工作台与研究证据；索引/链接之外最多4并发补正文（共用10秒预算、最多8000字）；content_status/scope/issue明确正文失败/截断，不等于附件全文采集 |
| 个股/行业研报 | `ReportProvider.MarketReports` | 东方财富 | 机构研报、行业研究、研究证据；支持现有查询过滤，不能据此声称全网研报/PDF正文能力 |
| 股票目录及概念归属 | `StockDirectoryProvider.StockCatalog` | 东方财富 | 名称搜索、证券身份、概念映射、成员候选；新浪目录虽已注册但不自动回退 |
| 主营业务 | `BusinessProvider.StockBusinessProfile` | `Company`：新浪 → 东方财富 | 主营/简介须明确，Industry/Scope不冒充；失败/覆盖不足整份回退，保留实际来源 |
| 财务基本面 | `FundamentalsProvider.StockFundamentals` | `Company`：新浪 → 东方财富 | 最新已披露合并累计/CNY，报告期/披露日分开；覆盖不足整份选择、旧报告不覆盖有效新报告，缺失/银行不适用不按0评分 |
| 题材榜、领涨股 | `ThemeFetcher.Fetch` | 开盘啦；`Themes` 校验中立快照 | 题材雷达、强度、融合、筛选；持久服务五分钟闸门，旧数据按交易日衰减 |
| 题材源涨停池 | `ThemeFetcher.FetchLimitUpPool` | 开盘啦 | 与题材榜同属 `Theme` 槽位；原生题材代码在来源内解释 |
| 涨停事件/连板输入 | `LimitUpProvider.RecentLimitUps` | `LimitUpProvider` 业务服务：开盘啦保留池 + 东方财富补字段/补缺 | 仅在持久服务启用时组合；无服务则东财；History/ProgressiveHistory另携带覆盖/合法空日，不跨缺日比较；字段补值带来源/时间，非两家直接覆盖 |
| 逐股题材归因 | `StockThemeAttributionProvider.StockThemes` | 由保留池和题材领涨快照派生 | 个股研究按 `pool/leader` 角色选择；保留真实供应商与交易日；不是独立供应商注册槽位 |
| 炸板/跌停池 | `MarketPoolProvider.BrokenLimitUpPool/LimitDownPool` | 东方财富 | 情绪、市场宽度和复盘等派生输入 |
| 人气/热榜 | `HotRankProvider.HotRank` | `HotRanks` 并行同花顺、东方财富 | 个股研究热榜；榜单/错误各自保留，不平均排名、不宣称热度尺度相同 |
| 股指期货历史 | `FuturesTrendProvider.Trend` | 东方财富历史 → 中金所最新会员单日快照 | `Futures` 业务降级；单日持仓不冒充历史、指数价格或基差 |
| 期指最近交易日快照 | `FuturesSnapshotProvider.LatestMembers` | 中金所 | 期指历史备用；与指定日期会员请求是独立能力 |
| 指定合约/日期会员排名 | `FuturesMembersProvider.Members` | 中金所 | 会员工作台；保留前20会员口径和实际日期 |
| 四品种持仓共识 | `FuturesConsensusProvider.Consensus` | 中金所 | IF/IH/IC/IM 共识；协议归一化及部分汇总仍在原适配器 |
| 美股行业 ETF 代理 | `USSectorProvider.USSectorMomentum` | 主槽位空，备用槽位腾讯 | 当前实际用于复盘隔夜市场输入，11 个 SPDR 行业 ETF 代理；无独立市场 HTTP 页面端点，不代表所有美股板块股票 |

策略入口：`access.go`、`price*.go`、`index.go`、`market.go`、`members.go`、`themes.go`、`limit_up.go`、`hot_rank.go`、`futures.go`。单源协议实现位于 `internal/providers/{sina,tencent,eastmoney,duanxianxia,hotstock,futuresposition}/`。

## 3. 资讯、文章、归档与知识能力

| 数据能力 | 契约/服务 | 实际来源及采集方式 | 消费/限制 |
| --- | --- | --- | --- |
| 市场快讯 | `NewsProvider.LatestNews` / `service.News` | 财联社 `providers/cls` | 市场快讯、研究等证据；无当前备用，实际请求统一观测 |
| 公告与研报 | `AnnouncementProvider` / `ReportProvider` | 东方财富 | 见上一表；资讯类数据共用公共注册集 |
| 已知文章链接 | `ArticleSource.FetchArticle` / `service.Articles` | 雪球、淘股吧、微信公众号；按 `ArticleHosts` 分派 | 手动导入、订阅正文补充；URL 不在登记域名内返回不支持，不是任意网站正文爬取 |
| 作者主页文章链接发现 | `AuthorLinksProvider.DiscoverAuthorLinks` / `service.Collections` | 仅淘股吧登记该槽位；保留淘股吧主页 HTML 链接发现 | 正常订阅走浏览器；主页发现为兼容路径，不是浏览器失败后的自动备用；雪球不提供该发现实现 |
| 浏览器作者采集 | `BrowserCollectionProvider.CollectBrowser` / `service.Collections` | 雪球/淘股吧本机浏览器桥接 | 正常订阅需要对应登录状态及 Hermes；有桥走桥，无桥用 Hermes 浏览器工具；业务管理订阅、去重和同步结果 |
| 授权文章正文 | `AuthorizedArticleProvider.FetchAuthorizedArticle` / `service.Collections` | 微信文章服务接口 | 有实现槽位，但默认不提供公众号列表发现能力；不能把槽位登记称为自动订阅已上线 |
| 预整理作者目录 | `ArchiveProvider.FetchAuthors` / `service.Archive` | `official` 远程 `authors.json` | 复盘作者目录/同步；ETag/未发布状态保留 |
| 作者/日期复盘正文 | `ArchiveProvider.FetchArchiveArticle` / `service.Archive` | `official` 远程发布归档 | review 校验 schema、作者/日期/身份、正文 SHA256 后入库；不是原平台直接请求 |
| 知识目录树 | `KnowledgeTreeProvider.Tree` | `githubknowledge` GitHub Tree API | methodology 识别目录与版本，完整目录才允许替换清单 |
| 知识 Markdown 正文 | `KnowledgeDocumentProvider.Document` | `githubknowledge` Raw 文档 | 心法展示、提示上下文、Hermes 技能/记忆同步；历史知识不作为实时事实 |

文章公共解析在 `providers/article`；授权链接/浏览器桥接/微信文章协议在 `providers/reviewautomation`；复盘归档在 `providers/reviewarchive`；知识下载在 `providers/githubknowledge`。研究报告/LLM加工不是文章供应商适配器的返回数据。

微信公众号已知链接配置文章侧车时先 POST `/api/article`，失败后读原文；未配置则直接读原文。雪球/淘股吧公开链接导入失败不自动切到登录浏览器流程。浏览器桥接口分别为 `/v1/xueqiu/collect`、`/v1/taoguba/collect`；作者订阅与公开原文导入是不同调用路径。

默认公开归档地址：`https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/reviews/daily`。知识默认仓库：`zhouqinglong520/trading-mastery`，读取 `游资心法/` 目录。这里仅列代码默认公开值，不读取本机配置或凭据。

启动配置可覆盖 `A_STOCK_DUANXIANXIA_BASE_URL`、`A_STOCK_WECHAT_API_URL`、`A_STOCK_DAILY_REVIEW_BASE_URL`。浏览器 Profile、桥接地址与登录状态按来源业务配置管理，不纳入公共行情凭据。

## 4. 业务消费入口

| 业务入口 | 使用的数据组合 | 主要代码/API |
| --- | --- | --- |
| 个股终端 | 报价、分时/K、竞价、题材 | `StockDetailWorkspace.tsx`；`/quotes/realtime`、`/quotes/kline`、`/quotes/intraday`、`/quotes/auction` |
| 市场总览 | 指数、行业、资金、融资、龙虎榜、期指 | `MarketOverviewWorkspace.tsx`；`httpapi/market_overview.go`；`/market/*` |
| 题材雷达/筛选/行业图谱 | 开盘啦题材 + 行业强度/成员 + 板块映射/目录 + 报价 + 涨停输入 | `sector/radar*.go`；`/themes/overview`、`/themes/screen`、`/sector-map` |
| 连板梯队/情绪 | 合并涨停、跌停/炸板、目录/概念、价格和历史快照 | `httpapi/limit_up_ladder.go`、`market_emotion.go`；`/short-term/limit-up-ladder`、`emotion-history` |
| 个股量化/AI研究与验证 | 价格、基准、题材归因、主营/财务、公告/研报、快讯/相关证据 | `stockanalysis`、`httpapi/stock_research.go`；`/stocks/ai-analysis`、`/stocks/research*` |
| 人气研究入口 | 同花顺、东方财富独立热榜 | `httpapi/stock_hot.go`；`/stocks/hot-ranks` |
| 持仓巡检/明日预期 | 复用研究和验证能力，不另建行情供应商 | `portfolioinspection`；`/portfolio-inspections`、`/reviews/portfolio-expectations*` |
| 复盘内容/每日摘要/次日验证 | 文章/授权订阅/远程归档 + 市场输入；隔夜输入含腾讯美股 ETF | `review`、`httpapi/review_market_data.go`、`review_validation.go`；`/reviews/*` |
| 心法库与AI上下文 | 内置资料、本地知识缓存、GitHub更新 | `methodology/library.go`、`httpapi/mastery.go`、`ai_chat.go`；`/short-term/mastery*` |
| WebSocket 行情 | 相同 Realtime 能力，保留独立连接与刷新生命周期 | `httpapi/stream.go`；`/ws/stream` |
| 来源设置 | Registry目录、已有业务观测、独立代表接口探针 | `source_health.go`、`source_probe.go`；`/sources`、`/sources/check` |

表中 HTTP 路径省略共同前缀 `/api/v1`；源文件相对 `internal/`，前端组件相对 `frontend/src/components/`。HTTP 查询和研究直接取数共享能力/策略，但不必共享同一个页面缓存。

## 5. 本地与派生数据

| 数据 | 归属 | 是否新增外部来源 |
| --- | --- | --- |
| 连板梯队、情绪、融合题材与强度、筛选结果 | 业务基于真实行情/池/成员计算 | 否；标注输入供应商与覆盖不足 |
| 年 K、指标、样本均价、双榜共识 | 业务或图表加工 | 否；不能改称供应商原生事实 |
| 复盘文章/订阅、研究证据/报告、持仓任务、情绪/题材历史 | 本地 SQLite/文件归档 | 是持久数据，不是可替代的公网供应商 |
| 心法内置种子、已下载 Markdown 与 manifest | methodology 本地知识 | 是种子/缓存，不增加公共来源数 |
| Hermes/LLM、显式联网检索和AI输出 | 分析运行时及业务任务 | 不登记为固定行情源；检索证据保留实际链接，生成文字不是原始市场事实 |

## 6. 缓存与检测

缓存仍由对应场景管理：总览成功数据 45 秒；目录 6 小时；热榜 2 分钟；详情报价/分时同键请求合并、成功复用至少 5 秒；开盘啦持久刷新最短 5 分钟；知识默认刷新间隔 24 小时。各自失败降级和字段语义见数据源边界文档；没有覆盖所有场景的统一新缓存。

`GET /sources` 只读，不主动请求供应商。`POST /sources/check` 默认检测：新浪代表报价、腾讯上证指数、东方财富新客户端股票目录、开盘啦直接涨停池、财联社电报、同花顺热榜、中金所 IF 单日快照。五项内容集成不参加该七源检测，按具体任务/授权状态判断。

能力成功不等于整家来源全部功能健康；缓存命中、未尝试、取消和不支持能力不续期成功或伪报失败。THS标签有独立能力观测及fetched/cache/joined/skipped执行状态，但没有独立主动探针；其它内部取数与每次传输重试仍有观测覆盖边界。

## 7. 已实现未默认使用、已退役与保留边界

| 项目 | 当前事实 | 后续变更需要处理 |
| --- | --- | --- |
| 新浪股票目录 | 已实现、已注册；默认目录仅东方财富。新浪仅有沪深股票名称/报价目录，不提供东财同等行业/概念或北交所覆盖 | 若作主源/备用，需显式路由/策略和名称、概念覆盖验收 |
| 中金所 `FuturesTrend` | 旧方法仅兼容单日；默认Registry不注册历史槽位，业务降级走 `FuturesSnapshot` | 不把单日接口误称为完整历史来源 |
| 美股行业 `USSector` | 主源为空，腾讯接在备用槽位；实际消费者是复盘隔夜输入 | 不声称已有独立市场页面/API；若启用其它主源注意当前策略不是主源失败后自动备用 |
| 东方财富股票K/明确复权/指数 | 遗留方法可能仍在源码，但未注册到这些默认能力，路由明确拒绝重启用 | 不恢复默认或备用；历史报告来源仍可读 |
| Tushare、TradingView | 无当前取数实现/注册 | 预留配置不是接入；历史凭据兼容字段不触发取数 |
| 微信公众号自动订阅 | UI不开放、`SyncReady=false`；默认无 `AuthorLinks`。后端兼容创建接口仍可保存微信订阅，默认同步返回 unsupported | 需要可靠文章列表协议和订阅业务/授权验证；不能描述为后端全入口禁止保存或已支持自动同步 |
| 同花顺龙虎榜标签 | 独立 `providers/ths` 和 `BillboardLabels` 路由，12小时/32日期缓存，最后等待者退出取消共享请求 | 原始东财明细不再访问同花顺；标签失败/禁用保留明细，平台分类不等于监管身份 |
| 默认知识装配 | `main.go` 先创建 Library，其默认适配器为 GitHub；显式 `ContentSources` 时才重新绑定 `service.Knowledge` | 同时核对 Library 注入与注册目录；不能只改默认目录名字就推断实际下载源已变 |
| 作者链接发现登记 | 仅淘股吧登记 `AuthorLinks`；雪球不支持的槽位已移除，正常订阅仍走浏览器 | 不能仅按非nil槽位宣称方法所有来源分支可用 |
| sector 映射元数据 | `BoardRef`/`StockSource`来自实际输入，聚合标本地组合；原生成分和新增涨停/目录候选区分 | 供应商未标来源保留unknown，不猜东财；分类映射版本与更多派生字段治理仍需审计 |
| 非页面消费者的健康观测 | PriceService/News自带回调；Market事件主要在Meta中返回，部分研究/验证/复盘消费者未写 sourceHealth | 相同取数能力不代表健康记录覆盖所有消费者；无记录不能推断未请求 |
| 题材持久生命周期 | SQLite/五分钟闸门仍在 duanxianxia 历史 Service/Store，但注入中立 ThemeFetcher | 继续重构实现时保留旧库/身份/刷新规则，避免重复缓存或重复请求 |
| 旧组合 Provider/HTTP注入 | 市场总览、热榜、期指、涨停兼容入口仍存在；新默认装配走能力服务 | 后续清理兼容字段前确认测试/外部调用，不把门面要求强加新供应商 |
| ClickHouse | `strategy/inflection/clickhouse.go` 有查询工具，不在默认来源装配/业务运行链路 | 不计作第十三项已启用数据源 |

源码入口：`providers/ths/billboard_labels.go`、`datasource/service/billboard_labels.go`（旧 `providers/eastmoney/billboard_labels.go` 仅保留未接入的兼容实现）、`providers/marketoverview/`、`providers/hotstock/client.go`、`providers/futuresposition/client.go`、`providers/duanxianxia/{service,store,limit_up_provider}.go`、`methodology/library.go`、`httpapi/server.go`。全量错误分类、网络事件、限流/重试统一和旧评分字段进一步清理仍按改造清单分阶段实施。

研究按股票所属市场/板块选择基准，可能请求北证50；腾讯明确研究指数映射不含北证50，不能承诺其备用覆盖。研究验证还会读取沪深300作为交易日历输入，见 `stockanalysis/decision.go` 和 `httpapi/stock_research.go`。

## 8. 后续替换的操作边界

替换现有能力通常修改对应适配器、Registry能力槽位及 Routes/内容分派，再验证单位、日期、复权、成员完整性、排名和文章授权语义。注册与业务选用是两步；目录有该能力不表示默认会请求它。

删除来源需调整所有受影响能力路由并审计上述内部补充调用；关闭能力不删除本地历史数据，不应复活默认供应商。新增未有业务能力/订阅平台还需扩展契约及消费者。当前注册在启动时固定，没有运行中热切换或用户设置里的自由路由编辑器。
