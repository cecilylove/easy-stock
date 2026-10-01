# Data Sources

This document tracks the stock-related data sources that form the `easy-stock` data foundation.

## Implemented In MVP

| Source | Domain | Current API | Data |
| --- | --- | --- | --- |
| Sina Finance | `hq.sinajs.cn` | `/api/v1/quotes/realtime` | A-share realtime quote. |
| EastMoney | `push2his.eastmoney.com` | `/api/v1/quotes/kline` | A-share K-line data. |
| EastMoney | `push2.eastmoney.com` | `/api/v1/sector-map` | Board list, board quote, board fund flow, board constituents. |
| EastMoney Datacenter | `datacenter-web.eastmoney.com` | `/api/v1/market/margin-balance` | Shanghai, Shenzhen, and Beijing financing balance, securities-lending balance, total margin balance, and financing net purchases. |
| CLS | `www.cls.cn` | `/api/v1/market/news` | Market telegraph news. |
| 短线侠 / 开盘啦 | `duanxianxia.com`, `ds.duanxianxia.com` | `/api/v1/themes/overview`, `/api/v1/themes/screen`, `/api/v1/short-term/limit-up-ladder` | 开盘啦题材排名、龙一至龙五、涨停/连板池与逐股炒作题材。 |

## Stock K-Line Fallback

`/api/v1/quotes/kline` requests EastMoney first and falls back to Sina when the primary request fails or returns no bars. The primary has at most 6 seconds and half the remaining request budget; the fallback has at most 10 seconds within the original deadline. This reserves time for a healthy fallback during slow primary failures. Caller cancellation stops both, and an unattempted fallback is not recorded as a failed source. If both sources return no bars, the API reports an error rather than a successful empty result. Sina uses `scale=240` for daily bars, `1200` for weekly bars, and `7200` for calendar-month bars (`month`, `monthly`, `103`, or `7200`). Monthly bars come directly from the source, rather than aggregating a fixed number of trading days. Read each bar's `meta.source` and `meta.source_url` to identify the actual source; the source's latest monthly bar may still represent an unfinished month.

## Trend Theme Radar Priority

- 开盘啦当天快照优先，题材榜和领涨股作为同一来源快照使用。
- 第三方刷新批次全局至少间隔 5 分钟，限制时间与最近成功快照持久化到 `theme-radar.db`，重启不会重置。
- 趋势题材雷达将行业趋势强度与开盘啦题材按相同权重融合；两边先各自标准化，再分别生成当日与 5 日融合强度。
- 行业独有题材与开盘啦独有题材按强度约束交替展示；同一题材命中两套来源时合并为一条，并保留开盘啦龙头与行业多周期指标。
- 本地趋势榜不再参与正常候选和补位。开盘啦沿用历史快照时按交易日衰减，超过两个交易日后只展示行业趋势强度。
- 开盘啦龙一至龙五保留来源排序，同时通过本地维护的“开盘啦题材 → 东财题材”对照表补充完整候选股池。
- 当前对照表覆盖开盘啦近 20 个交易日出现的 60 个题材；只维护题材级板块、行业和概念关键词，不维护个股一一映射，个股始终从映射后的东财题材实时成分中获取。
- 没有任何可用开盘啦快照时，完整回退到现有趋势题材识别。
- 开盘啦只负责题材归属和龙一至龙五，实时行情、K 线及领导力指标继续由现有行情源计算。
- 默认缓存路径由 `A_STOCK_THEME_RADAR_DB` 覆盖；测试环境可用 `A_STOCK_DUANXIANXIA_BASE_URL` 指向模拟服务。
- 行业/股票强度按当日和五日分别校验有效样本；只有名称、没有可用行情或 K 线的结果不作为零分成功。失败窗口保留本次运行中已有的有效强度，真实零分仍参与计算；这类保留值没有新增跨交易日过期机制，不能据此判断当前行情已恢复。
- 前端五日相对强度使用最近六个价格点形成的五个交易间隔，股票与题材均按相同日期复利计算。趋势 WebSocket 报价保留来源与行情时间，断线、跨日、来源标陈旧或超时后不继续显示为实时。

## Short-Term Limit-Up Radar Priority

- 当日涨停池、连续板数、首封/末封时间、开板次数、板型和逐股炒作题材优先使用开盘啦股票池。
- 开盘啦板块轮动与涨停池共用同一个持久化 5 分钟刷新闸门；页面重复刷新、切换工作台和服务重启均不会突破限制。
- 东方财富补充开盘啦缺少的价格、换手率、行业等字段，并提供历史交易日、昨日梯队与开盘啦缺失股票。
- 开盘啦尚未更新到当日时，当日梯队使用东方财富兜底，开盘啦最近成功快照仍用于对应历史交易日；接口元数据会标记兜底原因。
- 个股题材直接使用开盘啦逐股标签，不维护个股映射；仅当某只股票没有开盘啦题材时，才使用东方财富动态概念目录。

## Sector Map Data

The industry chain map is deliberately split into two layers:

| Layer | Owner | Data |
| --- | --- | --- |
| Theme rule layer | Local code in `backend/internal/sector/theme.go` | Theme IDs, tabs, group names, node names, and board matching rules. |
| Market hydration layer | EastMoney board APIs | Real-time board涨跌幅, 主力净流入, and top constituent stocks when `push2` is reachable. |
| Stock fallback layer | Sina realtime quotes | Representative node stocks when EastMoney board constituents are temporarily unavailable. |

EastMoney does not provide the exact screenshot-style fine-grained tree in one stable public endpoint. The stable approach is to keep the fine-grained taxonomy locally, then match each node to a real EastMoney board by board code or board name keyword.

Current EastMoney board endpoints:

| Purpose | Endpoint | Important Params | Fields |
| --- | --- | --- | --- |
| Board list | `https://push2.eastmoney.com/api/qt/clist/get` | `fs=m:90+t:2+f:!50` | `f12` code, `f14` name, `f3` pct change, `f20` total cap, `f21` float cap, `f62` main net inflow. |
| Board constituents | `https://push2.eastmoney.com/api/qt/clist/get` | `fs=b:<BK code>` | `f12` stock code, `f14` name, `f2` price, `f3` pct change, `f4` change, `f5` volume, `f6` amount, `f20`, `f21`, `f62`. |

For example, the local node `photoresist / 光刻胶` matches the EastMoney board name `光刻胶`, then hydrates `BK0891` constituents through `fs=b:BK0891`.

If EastMoney `push2` closes the constituent connection, the node falls back to `StockSymbols` in `theme.go` and uses Sina realtime quotes. The stock list is then a curated representative sample, but the price, change, and quote metadata are still live market data.

## Planned Sources

| Source | Domain | Planned Data |
| --- | --- | --- |
| Tencent Finance | `qt.gtimg.cn`, `web.ifzq.gtimg.cn`, `proxy.finance.qq.com` | HK/US quote, index, minute data, global indexes. |
| Tushare | `api.tushare.pro` | Stock basics, index basics, A/HK/US daily bars. Requires token. |
| EastMoney Datacenter | `datacenter.eastmoney.com`, `datacenter-web.eastmoney.com` | Additional F10, finance, shareholder, macro, and stock-selection datasets. |
| EastMoney Report | `reportapi.eastmoney.com`, `np-anotice-stock.eastmoney.com` | Research reports and announcements. |
| Iwencai | `openapi.iwencai.com` | Screening, report/news/investor/announcement search. Requires API key. |
| Xueqiu | `xueqiu.com`, `stock.xueqiu.com` | Hot stocks, hot events, finance pages. May require browser cookies. |
| TradingView | `news-mediator.tradingview.com`, `news-headlines.tradingview.com` | Global Chinese news flow and details. |
| Wallstreetcn | `api-one-wscn.awtmt.com`, `api-ddc-wscn.awtmt.com` | Live news, global markets, K-line, calendar. |
| Juyangongshe | `app.jiuyangongshe.com` | Investment calendar. |
| CNInfo IRM | `irm.cninfo.com.cn` | Investor interaction answers. |

## 单股盘前竞价参考轨迹

`GET /api/v1/quotes/auction?symbol=600519.SH` 使用东方财富 `stock/trends2/get` 的单交易日分钟快照，仅筛选 09:15–09:25 价格点；09:15–09:25 价格是**参考价而非逐分钟成交价**，不从 09:26 来源量额推断竞价最终成交。该接口不改写普通 K 线口径；返回 `data.meta` 的来源/抓取时间与 `data.trade_date`，历史交易日只给 `status=historical`、空点，避免旧日数据冒充当日竞价。东财主节点失败可尝试现有行情镜像节点，但可用性与盘中更新频率仍须在交易时段持续验证；无可靠点时页面显示缺失，不用 09:30 开盘价补造；页面可暂存同股同日已经成功获取的真实参考点，后续刷新失败时明确标为旧快照，跨交易日不可复用。仅个股详情的 `detail=1` 单股请求使用本机短时复用：成功至少间隔 5 秒，按股票/周期/上海日期隔离，同键合并在途请求，失败暂时退避 30–120 秒；取消最后一个查看者时取消尚未完成的来源请求。普通报价/K 线公共接口的行为不变。返回的 `meta.stale`、行情时间及来源仍是判断旧快照的依据；昨天的数据不能当作今天行情。

## 接入方式与降级边界

东方财富、新浪、腾讯、财联社、短线侠/开盘啦已内置公共接口，无需用户 Token；来源按具体功能自动选择，不支持任意替换供应商。设置中的 Tushare Token、同花顺 Cookie/Token 和东方财富登录态 Cookie 是预留凭据，当前没有相应取数实现，保存后不会启用新数据源。TradingView 没有配置入口。来源状态目录的 7 项不包含尚未实现的同花顺；设置目录额外展示其预留项，不改变可用来源计数。

- 个股 K 线：东方财富失败回退新浪；普通日/周/月 K 没有统一服务器旧快照兜底，两源均失败可能不可用。个股详情报价/分时的同股同日成功快照另有短时复用和 30–120 秒退避，旧快照标记陈旧，无快照时不可用。
- 指数：东方财富失败回退腾讯，备用覆盖范围较少；行业强度：腾讯失败回退东方财富；资金榜：新浪失败回退东方财富，备用可能缺字段。
- 行情总览模块：成功数据缓存 45 秒，刷新失败可返回本次服务运行中已有的成功快照并标记陈旧；没有快照时模块报错。融资余额、龙虎榜、公告/研报没有统一备用供应商，不能用 Tushare 或同花顺预留凭据兜底。
- 盘中情绪刷新被调用者取消或其上下文超时时，不把取消错误写入缓存、不延长缓存期限；重新进入可以重新请求。真实上游失败仍遵循原有缓存策略。
- 趋势题材：融合行业和开盘啦的有效结果，缺一方时使用其余来源；开盘啦旧题材超过两个交易日不再参与融合。渐进页面可保留旧快照并显示失败步骤；来源和快照均不可用时仍会报错。
- 财联社快讯没有备用供应商，失败后页面可能保留已有内容，无内容时快讯不可用。陈旧数据只能作为历史参考，以抓取时间、来源和缺失字段判断当前功能是否可用。

来源详情的接入与配置按钮直达系统设置的数据源区块；SettingsDrawer 展示 SourceIntegrationCatalog 的全部来源说明，预留凭据不能视为已实现服务。

## 数据源最近观测状态

`GET /api/v1/sources` 只读取本机运行期间的请求记录，**不主动访问第三方**。`status` 为 `available`（最近一次实际请求成功）、`degraded`（最近一次失败或回退到缓存/备用来源）、`unknown`（尚未观测，或最近观测已超过 10 分钟）、`unconfigured`（当前版本没有接入该来源）。`ok` 仅在 `available` 时为 `true`。`checked_at`、`last_success`、`last_failure` 只在发生实际观测后出现；再次读取目录不会刷新这些时间。失败消息经过概括，不回显第三方请求 URL 或密钥。

该状态不是对整个供应商所有接口的全面探针：目前从 HTTP/WebSocket 行情、K 线、财联社快讯、普通/渐进题材和行情总览的实际刷新记录观测；渐进题材仅在来源可归因且有实际结果的刷新时记录，不因轮询或读取本地快照续期；组合刷新整体超时但无法确认哪家上游失败时，只在题材步骤显示超时，不猜测具体供应商。缓存读取不制造新观测；可确认的单一来源实时刷新失败与备用来源成功分别记录，无法归因的组合失败不据旧快照推断故障来源。开盘啦同一批题材与涨停池若部分成功、部分失败，最近状态为降级；来源列表不能替代具体能力的状态。TradingView 与 Tushare 当前未接入，标记未接入而非等待检测。某个接口成功或回退成功，不等于该供应商的全部服务正常。请同时以具体功能页面的 `meta.source`、`meta.fetched_at`、`meta.stale` 和 `meta.fallback_reason` 判断数据能否使用。

普通题材接口与渐进接口共用来源观测规则：融合后的总元数据不能代替内部来源记录，分别记录行业实际来源、开盘啦题材/涨停池真实刷新与部分失败；5 分钟闸门内的缓存读取和旧失败不会生成新的开盘啦观测。普通接口后续计算超时不会丢弃已完成的来源结果，用户主动取消时不记录来源故障。前端趋势题材和行情总览的数据源入口可点击展开来源名单、时间、状态说明和取数规则；趋势题材另展示当前更新步骤，供应商全局状态与该步骤的可用性分别判断。

## Source Reliability Rules

- A provider must expose normal unit tests with mocked upstream responses.
- A provider that reaches the public internet must expose opt-in live tests.
- Live test failures should identify whether the cause is HTTP status, parse failure, auth missing, no data, or timeout.
- API consumers should read `meta.source` rather than assuming one fixed upstream.
- Sector map nodes must have at least one board matching rule before they are exposed in a theme.
