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

## 数据源最近观测状态

`GET /api/v1/sources` 只读取本机运行期间的请求记录，**不主动访问第三方**。`status` 为 `available`（最近一次实际请求成功）、`degraded`（最近一次失败或回退到缓存/备用来源）、`unknown`（尚未观测，或最近观测已超过 10 分钟）、`unconfigured`（当前版本没有接入该来源）。`ok` 仅在 `available` 时为 `true`。`checked_at`、`last_success`、`last_failure` 只在发生实际观测后出现；再次读取目录不会刷新这些时间。失败消息经过概括，不回显第三方请求 URL 或密钥。

该状态不是对整个供应商所有接口的全面探针：目前从 HTTP/WebSocket 行情、K 线、财联社快讯、渐进题材和行情总览的实际刷新记录观测；渐进题材仅在来源可归因且有实际结果的刷新时记录，不因轮询或读取本地快照续期；组合刷新整体超时但无法确认哪家上游失败时，只在题材步骤显示超时，不猜测具体供应商。缓存读取不制造新观测；可确认的单一来源实时刷新失败与备用来源成功分别记录，无法归因的组合失败不据旧快照推断故障来源。开盘啦同一批题材与涨停池若部分成功、部分失败，最近状态为降级；来源列表不能替代具体能力的状态。TradingView 与 Tushare 当前未接入，标记未接入而非等待检测。某个接口成功或回退成功，不等于该供应商的全部服务正常。请同时以具体功能页面的 `meta.source`、`meta.fetched_at`、`meta.stale` 和 `meta.fallback_reason` 判断数据能否使用。

## Source Reliability Rules

- A provider must expose normal unit tests with mocked upstream responses.
- A provider that reaches the public internet must expose opt-in live tests.
- Live test failures should identify whether the cause is HTTP status, parse failure, auth missing, no data, or timeout.
- API consumers should read `meta.source` rather than assuming one fixed upstream.
- Sector map nodes must have at least one board matching rule before they are exposed in a theme.
