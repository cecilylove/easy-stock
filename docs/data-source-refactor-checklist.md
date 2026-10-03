# 数据源重构改造清单

本文是 [数据源架构方案](data-source-architecture-plan.md) 的执行清单。版本：1.2（2026-10-02）；状态：**架构核心已实施，后续治理项保留**。勾选项对应本轮已完成范围；未勾选项可能已有部分基础，具体边界见下表。新增供应商模板始终留空。各阶段命令是范围建议，实际执行结果只记在末尾实施记录。

架构迁移与替换供应商是两项工作：先保持当前来源策略迁移边界，再按 [来源替换计划](source-replacement-rollout.md) 验证并替换供应商。现有来源与回退边界以 [数据源说明](../backend/docs/data-sources.md) 和源码为准。

## 本轮核心验收与保留项

已建立按能力的抽象、不可变目录、显式路由、默认集中装配和访问服务；市场总览、人气、期指、涨停的多源策略与单源取数分离；文章、浏览器桥接、归档、GitHub 知识取数抽离。新增来源、替换独立能力、移除/禁用、目录同步和来源身份有离线回归。

默认公共七家和原路由保持。五项内容来源加入目录但不加入公共探测：雪球、淘股吧、公众号正文、预整理复盘、GitHub 知识。公众号自动订阅仍未启用，登记内容能力不等于已经登录。来源移除不删除持久数据，显式空注册集不恢复默认供应商；路由仍引用已移除能力时启动报错，需同步调整路由。

| 保留任务 | 本轮已完成 | 仍需后续实施 |
| --- | --- | --- |
| DS-P0-04 | 错误枚举、核心新服务分类；东财JSON GET结构化HTTP状态/原因链、TimedOut与Canceled分离 | 遗留单源错误全量归一化，不能宣称所有协议错误都已分类 |
| DS-P0-05 | 现有 TTL/共享刷新/历史分时缓存与来源边界保持，价格键保留能力/基准 | 所有场景缓存键及融合算法版本统一审计；当前注册在启动后不可变，无运行中热切换 |
| DS-P1-02 | 父预算与取消组件、价格退避集中；行业/资金备用预算、东财有限退避及429冷却、公告正文4并发/总预算 | 供应商限流、传输、安全日志、重试进一步统一；保留现有唯一所有者，不叠加全局重试 |
| DS-P1-05 | 目录由真实槽位生成、注册探针；新闻唯一观测；主要能力记录；THS标签链显式fetched/cache/joined/skipped与健康过滤 | 全量网络 Attempt/Outcome 和内部缓存/共享等待状态迁移；旧探针验证器仍作兼容 |
| DS-P1-06 | 新快讯源 API/研究/独立探针与移除契约测试 | 尚未运行独立提交级回滚演练；原装配可通过恢复具体接线回滚 |
| DS-P3-05 | 独立期指历史、单日快照、会员、共识能力、分别配置的路由与跨源降级 | 单一交易所共识的协议归一化/汇总仍在原适配器中；后续供应商内部重构再拆计算职责 |
| DS-P4-04 | sector 使用中立快照，来源元信息随替换保留；涨停合并移入服务；归因证据按 pool/leader 角色消费，原生代码和强度缓存按来源隔离 | 旧 kpl/Kaipanla 评分字段、融合评分缓存版本进一步规范；本轮保留同输入评分，不改算法 |
| DS-P5-04 | 文档/长期入口同步，旧组合 Provider 改成委托 | 旧 HTTP 注入字段、构造器兼容与题材持久生命周期暂留；后续确认外部调用后收敛 |

这些保留项属于供应商实现和治理进一步规范化，不妨碍现有能力按“适配器 + 注册 + 路由 + 契约验证”扩展；也不能据此承诺任何新供应商天然语义等价、永久稳定或支持任意能力。

## 执行约束

- 按能力逐批迁移，保持 HTTP 路径、响应字段、页面功能与已有数据可读；阶段内允许薄兼容适配器，禁止同一能力同时走两条真实请求链路而重复采集。
- 不恢复已退役的东方财富个股 K、指定复权、指数快照与指数历史默认/备用链路。其它仍需东方财富的能力保留到可靠替代通过验收。
- 保持默认来源选择：实时与分时新浪；默认个股 K 新浪后腾讯；明确复权腾讯且不静默换口径；指数腾讯；行业腾讯后东方财富；资金新浪后东方财富；涨停开盘啦与东方财富合并补充；人气同花顺与东方财富独立两榜；期指历史东方财富、失败降级中金所单日，中金所会员与共识单独取数；快讯财联社。
- 缓存命中不续期来源健康、未尝试能力不报成功；手动代表接口探针与实际业务观测继续分开。
- 不删除或改写用户设置、凭据、研究/复盘/情绪数据库、订阅、浏览器登录态、Hermes Home。验证使用独立数据路径，不采集生产密钥到夹具或日志。
- 保持本机监听、Token、Host 与 Origin 边界；不为接入新来源放宽鉴权。
- 每个任务 ID 稳定保留，完成后记录实际改动、实际命令、结果与回滚入口。分工时按独立模块划定写入范围；共享契约先完成再接适配器。

## 阶段依赖

| 阶段 | 依赖 | 交付物 |
| --- | --- | --- |
| P0 基线与契约 | 无 | 当前行为基线、能力契约、模型与语义约束 |
| P1 注册目录与快讯试点 | P0 验收 | 静态注册目录、请求治理、财联社快讯完整迁移 |
| P2 报价与价格序列 | P1 验收 | 实时、竞价、分时、K、历史分时、指数路由迁移 |
| P3 拆组合与证券资料 | P1；共用价格能力部分依赖 P2 | 小能力接口、市场总览、人气与期指编排、资料能力 |
| P4 涨停与题材解耦 | P2、P3 验收 | 中立题材快照与成员契约、独立融合业务服务 |
| P5 内容采集与收尾 | P1；依赖市场证据部分等待 P3、P4 | 内容采集适配、研究/复盘兼容、文档与旧入口收敛 |

建议每个阶段拆成可独立回滚的本地提交。阶段完成不等于允许推送。

## P0：冻结现状与建立标准契约

- [x] **DS-P0-01 当前策略基线**：核对 `backend/internal/httpapi/server.go`、`config.go` 和 `backend/internal/providers/marketoverview/provider.go`，列出每项能力的当前主备、并行、合并、降级策略及禁用链路。记录上游失败、空结果、取消、旧缓存的行为；不要以文档代替源码检查。依赖：无。
- [x] **DS-P0-02 能力接口拆分表**：从 `backend/internal/httpapi/config.go` 的 Provider 接口提取业务能力，明确实时、竞价、源默认 K、严格复权 K、历史分时、指数、行业强度、板块成员、资金、融资余额、龙虎榜、涨停/市场池、人气、证券目录/主营/财务、快讯/公告/研报和内容采集的输入输出。每个能力使用有类型的请求与响应，避免通用 `Fetch(kind, map)`。依赖：DS-P0-01。
- [x] **DS-P0-03 模型兼容与语义**：审查 `backend/internal/foundation/types.go`、`market_overview.go`、`hot_stock.go`、`futures_position.go`、`theme_progress.go`、`trading_calendar.go`；优先保留已通用的模型，新增必要契约，不一次性移动全部文件。区分业务时间、交易日、抓取时间、发布/更新时间、陈旧、部分成功、字段缺失与真实零值。保留原始来源与原生身份；定义价格复权/量纲、资金统计、人气榜单、期指覆盖范围的口径标识。依赖：DS-P0-02。
- [ ] **DS-P0-04 请求结果与错误分类**：整理 `kline_routing.go`、`source_health.go`、`source_probe.go` 的既有规则，建立能力不支持、无数据、非法响应、限流、上游/传输失败、鉴权、取消的有类型结果。取消不计来源失败；单标的无数据不熔断整家来源；探针成功不表示全部能力成功。依赖：DS-P0-03。
- [ ] **DS-P0-05 缓存和观测清单**：核对 `stock_detail_poll.go`、`stock_intraday.go`、`stock_hot.go`、`market_overview.go`、`theme_progress.go`、`stream.go`、`stock_analysis.go` 及对应测试，分别冻结详情轮询、普通 API、WebSocket、市场快照、研究证据和手动探测的 TTL、合并、旧缓存与观测规则。五秒详情缓存不能自动推广给普通报价、WS 或研究。设计分能力缓存键：包含标的/板块完整身份、周期、复权/价格基准、查询过滤与排序、供应商/解析后的路由、交易日/快照版本中实际影响结果的维度。融合缓存包含算法版本和输入快照身份；避免给所有能力套一份最大化键。依赖：DS-P0-03、DS-P0-04。
- [x] **DS-P0-06 基线验收**：保存可复现的离线契约夹具与必要回归场景；不写仅镜像实现的测试。后续每个阶段使用基线对比 HTTP 字段、来源和降级含义。依赖：DS-P0-01 至 DS-P0-05。

验收：行为矩阵覆盖现有七家来源与内容采集入口；没有把“同字段名”定义为“同口径”；所有后续接口归属、共享模型和兼容方案可审查。

已有测试入口：`backend/internal/httpapi/default_providers_test.go`、`source_health_test.go`、`source_probe_test.go`、`kline_no_eastmoney_test.go`，`backend/internal/foundation/symbol_test.go`、`trading_calendar_test.go`。计划命令（工作目录 `backend/`）：`go test ./internal/foundation ./internal/httpapi`。

## P1：注册目录、基础治理和快讯试点

- [x] **DS-P1-01 来源与能力静态注册**：替换 `source_health.go` 固定来源白名单、`server.go` 分散来源 ID 和默认装配；目录分别声明来源身份、能力及覆盖范围、凭据需求、适配器和默认策略。不默认承诺未实现功能；预留凭据只兼容存储，不因为填写凭据启用能力。依赖：P0。
- [ ] **DS-P1-02 请求治理最小实现**：提取共享 HTTP 传输、总预算内超时、供应商限流、有限重试、网络/能力级退避和安全日志；取消向下游传播。注册记录机制但保留供应商专有签名、端点和解析在适配器；缓存按能力及调用场景策略选择，不统一 TTL。迁移后每层缓存与重试有唯一所有者，不与原客户端机制叠加。依赖：DS-P1-01、DS-P0-04、DS-P0-05。
- [x] **DS-P1-03 财联社快讯端到端迁移**：从 `backend/internal/providers/cls/client.go`、`httpapi/server.go` 的 `news` 处理器和 `stockanalysis/news.go` 起，完成适配器 → 统一快讯模型 → 能力路由 → 原 HTTP 响应的闭环。仍只有财联社默认源，保留来源链接和发布时间。先验证此小能力，再推广其他能力。依赖：DS-P1-01、DS-P1-02。
- [x] **DS-P1-04 来源设置接入目录**：同步 `frontend/src/lib/source-integrations.ts`、`source-health.ts`、`components/SourceIntegrationCatalog.tsx`、`SourceHealthPanel.tsx`、`SettingsDrawer.tsx`。清单与检测数据使用注册目录映射，保留配置保存和旧 API 兼容，防止展示信息与后端实现脱节。依赖：DS-P1-01、DS-P1-03。
- [ ] **DS-P1-05 业务观测与探针迁移**：重构 `source_health.go`、`source_probe.go`；真实网络尝试在适配器/治理边界唯一记录，迁移能力关闭原 HTTP/组合 Provider 重复写入，合并请求的等待者不重复计次，内部缓存命中显式传状态。缓存、未触发回退不产生供应商成功记录。快讯试点验证部分失败、取消、旧缓存与探针状态。依赖：DS-P1-02 至 DS-P1-04。
- [ ] **DS-P1-06 试点验收与回滚演练**：通过同一 HTTP 契约比较新旧快讯链路，检查目录、检测、业务证据；一次只启用一条正式链路。验证将快讯装配恢复为旧适配器不需要变更数据或前端。依赖：DS-P1-05。

验收：新增来源身份不再修改固定白名单；财联社快讯行为保持；设置能解释已实现能力和检测范围；旧配置仍可读取。

已有测试入口：`providers/cls/news_test.go`，`httpapi/source_probe_test.go`、`source_health_test.go`、`settings_test.go`；前端 `SourceHealthPanel.test.tsx`、`SourceIntegrationCatalog.test.tsx`、`SettingsDrawer.source-settings.test.tsx` 和 `lib/source-health.test.ts`。计划命令：在 `backend/` 执行 `go test ./internal/providers/cls ./internal/httpapi ./internal/stockanalysis`；根目录执行 `npm --workspace frontend test -- --run`、`npm run build:frontend`。

## P2：实时报价、竞价、K、分时和指数

- [x] **DS-P2-01 报价和竞价能力迁移**：从 `providers/sina/client.go`、`providers/eastmoney/auction.go`、`httpapi/server.go`、`stock_detail_poll.go`、`stream.go`、`stock_analysis.go` 和 `frontend/src/components/StockDetailWorkspace.tsx` 接入能力目录；HTTP 普通/详情、WS 周期调用与研究采集统一接到数据访问服务，保持各自缓存/旧值许可、取消、WS 消息字段及上游观测语义。保持实时/五档快照与竞价不同能力，不把轮询快照称为逐笔/L2 数据。统一时间、价格、成交量单位与字段可用性。依赖：P1。
- [x] **DS-P2-02 默认 K 路由迁移**：从 `httpapi/kline_routing.go` 和 `providers/sina/client.go`、`providers/tencent/stock_kline.go` 提取能力判断与候选路由；保留新浪后腾讯、总预算、退避、单供应商完整快照。将供应商专有单位转换移至适配器，领域层继续校验 OHLC、标的、时间和价格基准。依赖：DS-P2-01、DS-P0-05。
- [x] **DS-P2-03 严格复权契约与显式选择**：从 `httpapi/kline_adjustment.go`、`server.go` 的腾讯静态判断迁移至注册能力；接口用供应商、复权类型、复权约定、价格基准、支持周期及市场共同校验。保留腾讯唯一已支持显式复权与不静默回退，拒绝退役东方财富入口；不伪装其他源为腾讯。历史时点复权仍未实现时明确拒绝。依赖：DS-P2-02。
- [x] **DS-P2-04 年 K 与前端价格兼容**：保留 `httpapi/kline_year.go` 月 K 聚合和元数据/字段掩码；核对 `frontend/src/lib/kline.ts`、`stock-detail.ts`、`source-fields.ts`、`components/ProfessionalKLineChart.tsx`。切换来源或基准不得把新价格接到旧基准序列上，刷新仍保持坐标。依赖：DS-P2-03。
- [x] **DS-P2-05 当前及历史分时迁移**：迁移 `httpapi/stock_intraday.go`、`providers/sina/stock_intraday.go`、`intraday_codec.go` 与 `frontend/src/components/HistoricalIntradayDialog.tsx`；保留历史月档案优先、失败仅允许同日近期样本回退，缓存键隔离交易日和标的。缺历史不得替换为当天走势。依赖：DS-P2-02。
- [x] **DS-P2-06 指数独立路由迁移**：从 `providers/marketoverview/index.go`、`providers/tencent/benchmark_kline.go` 和 `httpapi/market_overview.go` 接入指数能力；腾讯仍为唯一正式指数来源。保留严格指数身份、缺失指数列表、范围/周期键、部分成功与不跨指数替代；海外指数不能按名称混用。依赖：DS-P2-02、DS-P2-03。
- [x] **DS-P2-07 价格阶段验收**：覆盖周期/市场不支持、空数据、取消、限流、坏响应、切换来源、不同复权/单位、同日旧缓存、年度聚合与指数部分缺失，检查缓存不触发健康续期。依赖：DS-P2-01 至 DS-P2-06。

验收：HTTP 与图表行为兼容；复权身份正确；全链路没有退役东方财富价格/指数请求；分时不跨交易日回退；所有价格字段缺失可辨识。

已有测试入口：`httpapi/kline_routing_test.go`、`kline_adjustment_test.go`、`kline_no_eastmoney_test.go`、`kline_fallback_test.go`、`kline_month_test.go`、`kline_year_test.go`、`stock_intraday_test.go`、`stock_detail_poll_routes_test.go`、`stock_detail_poll_historical_test.go`、`market_index_cache_contract_test.go`、`auction_test.go`；`providers/sina/realtime_test.go`、`kline_test.go`、`stock_intraday_test.go`，`providers/tencent/stock_kline_test.go`、`benchmark_kline_test.go`，`providers/marketoverview/index_test.go`；前端 `StockDetailWorkspace.test.tsx`、`HistoricalIntradayDialog.test.tsx`、`ChartRefreshInteraction.test.tsx`、`lib/stock-detail.test.ts`、`stock-intraday.test.ts`、`kline.test.ts`。

计划命令：在 `backend/` 执行 `go test ./internal/providers/sina ./internal/providers/tencent ./internal/providers/eastmoney ./internal/providers/marketoverview ./internal/httpapi`；根目录执行 `npm --workspace frontend test -- --run`、`npm run build:frontend`。

## P3：拆市场总览、人气、期指与证券资料

- [x] **DS-P3-01 拆分 MarketOverview 大接口**：拆 `httpapi/config.go` 与 `providers/marketoverview/provider.go` 中捆绑的行业、资金、融资余额、龙虎榜、公告、研报接口；HTTP 暂通过薄门面保持原调用契约。主备路由使用注册描述与真实尝试元数据，移除写死腾讯/新浪/东方财富观测身份。依赖：P1；指数边界使用 DS-P2-06。
- [x] **DS-P3-02 行业与资金迁移**：迁移 `providers/tencent/industry.go`、`industry_stocks.go`、`providers/sina/money_flow.go`、`providers/eastmoney/market_overview.go`。保留腾讯行业→东方财富、新浪资金→东方财富；跨源字段不等价时表达可用字段、统计定义与覆盖范围。行业身份使用供应商+维度+原生代码，不用名称替代主键。依赖：DS-P3-01。
- [x] **DS-P3-03 融资余额、龙虎榜、公告与研报独立接入**：从 `providers/eastmoney/market_overview.go` 和 `billboard_labels.go` 分出能力；保留现有东方财富实现、链接、交易日、席位标签及查询过滤，不扩写为完整两融能力。架构重构不顺便引入未验收替代源。依赖：DS-P3-01。
- [x] **DS-P3-04 人气双榜编排**：拆 `providers/hotstock/client.go` 的两家 HTTP 取数和榜单并发编排，迁移 `httpapi/stock_hot.go`。保持同花顺/东方财富独立来源榜单、独立错误与各自排名，业务层负责并排呈现，不取平均排名或假定热度值等价。依赖：P1。
- [ ] **DS-P3-05 期指拆源与降级**：拆 `providers/futuresposition/client.go` 中东方财富历史/主力合约与中金所会员快照/全合约共识请求。业务层处理历史失败→单日降级和共识计算，标准模型显式表达单日/历史、主力/全合约、前20会员等范围；缺指数/基差用缺失值，不补零或补假历史。依赖：DS-P3-01。
- [x] **DS-P3-06 证券资料独立能力**：整理 `httpapi/stock_directory.go`、`providers/eastmoney/stock_catalog.go`、`stock_business.go`、`providers/sina/stock_catalog.go`、`httpapi/config.go` 的目录/概念/主营/财务接口；分开证券基础身份与供应商板块归属，保留当前目录和公司资料默认链路、目录旧缓存与名称搜索。新浪目录已有实现不代表本阶段自动更改默认装配。依赖：DS-P3-01、DS-P3-02。
- [x] **DS-P3-07 总览与研究接线**：接回 `httpapi/market_overview.go`、`review_market_data.go`、`review_validation.go`、`stock_analysis.go`、`frontend/src/components/MarketOverviewWorkspace.tsx` 与 `lib/market-overview.ts`、`futures-position.ts`；复盘验证的指数/行业/资金/目录/报价均接到能力服务，题材/情绪的派生入口待 P4 继续接线。AI 证据保留真实来源、范围与缺失，不把降级称为完整结果。依赖：DS-P3-02 至 DS-P3-06。
- [x] **DS-P3-08 组合阶段验收**：验证独立替换一项能力不需要实现其它能力，双榜错误互不覆盖，期指会员/共识和历史状态独立，目录/基本面不影响行情能力，缓存命中与实际请求可区分。依赖：DS-P3-07。

验收：大接口仅作为临时兼容门面；供应商适配不包含跨供应商编排；数据范围和字段语义可追踪；默认策略未变。

已有测试入口：`providers/marketoverview/provider_test.go`、`providers/hotstock/client_test.go`、`providers/futuresposition/exchange_only_test.go`，`providers/tencent/industry_contract_test.go`、`providers/eastmoney/stock_catalog_test.go`、`stock_business_test.go`、`market_overview_test.go`、`billboard_labels_test.go`；`httpapi/stock_hot_test.go`、`stock_directory_test.go`、`market_overview_test.go`、`review_market_data_test.go`、`stock_analysis_test.go`；前端 `MarketOverviewWorkspace.test.tsx`、`market/MarketDataViews.contract.test.tsx`、`lib/market-overview.test.ts`、`futures-position.test.ts`、`stock-directory-cache.test.ts`。

计划命令：在 `backend/` 执行 `go test ./internal/providers/... ./internal/httpapi ./internal/stockanalysis`；根目录执行 `npm --workspace frontend test -- --run`、`npm run build:frontend`。

## P4：涨停合并与题材业务解耦

- [x] **DS-P4-01 涨停/市场池标准契约**：从 `providers/duanxianxia/types.go`、`limit_up_provider.go` 和 `providers/eastmoney/limit_up.go` 提取带交易日、标的、连板、封板状态、题材归因与来源的契约。保留开盘啦与东方财富实际合并；新字段presence与补值溯源、可选History/ProgressiveHistory携带覆盖/合法空日，HTTP/情绪不跳过空日或跨缺日比较；区分题材归因证据与供应商概念标签。依赖：P2、P3。
- [x] **DS-P4-02 中立题材输入快照**：替换 `sector/radar.go` 的 `duanxianxia.Snapshot/FetchMeta` 类型边界，保留源题材强度、核心股、快照 ID 和数据时间。将 Attempted/FromCache/PoolRefreshed 等转换为中立执行状态；一次联合刷新分别记录题材与涨停池结果，持久闸门跳过和缓存读取不伪报上游成功。`providers/duanxianxia/client.go`、`parser.go`、`service.go`、`store.go` 负责协议和既有持久化兼容，不要求一次性迁移 SQLite 表或删除旧快照。依赖：DS-P4-01。
- [x] **DS-P4-03 成员与原生身份路由**：改造 `sector/radar_identity.go`、`radar.go`、`mapper.go`、`industry_mapping.go`、`radar_mapping.go`、`kaipanla_theme_mapping.go`。成员路由依据能力描述与完整原生身份，移除业务中只允许腾讯 `pt` 的静态分支；跨源题材映射显式保留规则和匹配证据。旧 `kpl:`、`industry:`、`fusion:` 身份继续可读，不把东方财富 BK 代码直接发给腾讯。依赖：DS-P4-02、DS-P3-02、DS-P3-06。
- [ ] **DS-P4-04 独立题材融合与评分**：迁移 `sector/radar_fusion.go`、`radar_strength.go`、`mapper_radar_strength.go` 的融合、评分、补股与覆盖完整性到业务服务。内部评分以来源角色与口径表达，HTTP 如需继续输出 `KaipanlaDailyScore` 等旧字段，使用兼容映射；算法版本纳入融合缓存。单来源输出保留降级证据，不伪造另一来源分数。依赖：DS-P4-02、DS-P4-03。
- [x] **DS-P4-05 连板、情绪、渐进返回接线**：同步 `httpapi/limit_up_ladder.go`、`market_emotion.go`、`theme_progress.go`、`theme_screen.go`、`ladder_theme_ai.go`。派生情绪和题材分数标注为业务结果，来源尝试仍逐家记录；保留渐进快照一致性、取消与旧缓存规则。依赖：DS-P4-01、DS-P4-04。
- [x] **DS-P4-06 题材阶段验收**：验证部分来源失败、行业成员失败仅留领涨股、成员不足、旧身份解析、涨停池未更新、题材融合得分、快照过期与缓存不续期。使用相同输入验证迁移前后的融合规则，不在架构改造中顺便调分。依赖：DS-P4-05。

验收：`sector` 的标准输入不再返回供应商专用结构；供应商负责采集，业务负责合并；旧快照/题材入口可读；来源与成员完整性正确。

已有测试入口：`providers/duanxianxia/limit_up_test.go`、`progressive_limit_up_test.go`、`service_test.go`、`store_limit_up_test.go`；`sector/radar_test.go`、`radar_identity_test.go`、`radar_observation_test.go`、`radar_progress_test.go`、`member_contract_test.go`、`mapper_test.go`；`httpapi/theme_progress_test.go`、`theme_observation_test.go`、`theme_membership_contract_test.go`、`short_term_progress_test.go`、`market_emotion_test.go`、`market_emotion_cancel_test.go`、`ladder_theme_ai_test.go`。

计划命令：在 `backend/` 执行 `go test ./internal/providers/duanxianxia ./internal/providers/eastmoney ./internal/sector ./internal/httpapi`；根目录执行 `npm --workspace frontend test -- --run`、`npm run build:frontend`。

## P5：内容采集、研究与收尾

- [x] **DS-P5-01 内容采集能力边界**：整理 `backend/internal/review/importer.go`、`automation.go`、`remote_daily.go`、`types.go` 和 `httpapi/review_diary.go`。定义文章正文、作者/发布者、原文 URL、发布时间/采集时间、内容哈希与权限状态的契约；公开 URL/公众号侧车、浏览器桥接/Hermes 和远程复盘清单分别适配，不将所有采集改为公共行情 HTTP 管道。依赖：P1。
- [x] **DS-P5-02 内容解析与采集编排分离**：将 URL 分类、供应商解析、浏览器采集响应归一化留在相应适配器；订阅调度、去重、同步状态和复盘聚合保持业务职责。保持已有登录态与配置生命周期、删除作者后的状态、远程校验哈希、失败/待发布差异；不重建订阅或自动恢复用户删除记录。依赖：DS-P5-01。
- [x] **DS-P5-03 研究证据链兼容**：核对 `stockanalysis/research_service.go`、`research_evidence.go`、`research_snapshot.go`、`httpapi/stock_research.go`、`review_market_data.go`、`review_validation.go`、`portfolio_inspection.go`、`portfolio_expectation.go` 及 `server.go` 的分析回调装配。持仓巡检/明日预期仍复用研究能力服务，保留任务生命周期和旧库；复盘验证的派生市场输入完成接线。行情、资料、公告/研报和内容证据保留来源与时间；LLM/Hermes 输出是分析产物，不能登记为行情供应商事实。已有研究快照和复盘库继续可读。依赖：DS-P5-02、P3、P4。
- [x] **DS-P5-06 知识取源与使用方接线**：从 `backend/internal/methodology/library.go` 抽离 GitHub 目录树和 Markdown 下载能力；内置内容种子、本地缓存、旧缓存回退、资料版本与库索引仍归 methodology。保留 `ContextForPrompt` 与 Hermes 技能/记忆同步，核对 `backend/internal/httpapi/ai_chat.go` 的知识输入接线，不能把历史经验标为实时事实。依赖：P1、DS-P5-01；知识缓存与注册模式先落实再迁移。
- [ ] **DS-P5-04 删除过渡接线并同步文档**：所有能力完成迁移后再移除旧装配/重复路由和失效供应商判断；保留必要旧 API/身份/配置适配。核对 `AGENTS.md`、`backend/docs/architecture.md`、`backend/docs/data-sources.md`、`docs/source-replacement-rollout.md`、`docs/development.md`、`docs/stock-detail-terminal.md`、`docs/stock-research.md` 与本方案；只更新职责和长期约束，不登记一次性排障记录。依赖：P2、P3、P4、DS-P5-03、DS-P5-06。
- [x] **DS-P5-05 完整验收与交付记录**：汇总每阶段实际检查、兼容变化、剩余替换供应商清单与回滚说明。默认离线测试不启用公网 live 开关；如后续单独启用公网检查，区分上游可用性与代码回归。依赖：DS-P5-04。

验收：市场数据与内容采集共享来源登记/观测规范，但保留各自访问与调度机制；用户持久化内容、凭据与登录态没有被删除或重置；过渡兼容和残余耦合有明确记录。

每阶段关闭前按下表审计入口，检查残余 Provider 直调与供应商导入；实际网络只能出现在适配器，消费入口必须经过相应能力/业务服务。暂存的兼容委托需标明清理任务。未启用的 `backend/internal/strategy/inflection/clickhouse.go` 本轮明确保留，不登记为默认公共源，也不扩大为公开 SQL 能力。

## 消费入口覆盖矩阵

| 消费入口 | 数据能力/业务 | 迁移任务 | 已有回归入口及需补验证 |
| --- | --- | --- | --- |
| httpapi/server.go、auction.go、stock_intraday.go | 普通/详情报价、竞价、K、历史分时 | DS-P2-01 至 DS-P2-05 | stock_detail_poll_routes_test.go、stock_intraday_test.go、auction_test.go；验证普通 API 与详情缓存差异 |
| httpapi/stream.go | WS 报价快照 | DS-P2-01、DS-P2-07 | httpapi/server_test.go、source_health_test.go 中 WS 场景；保持默认三秒/允许的刷新范围、quotes/error 消息、观测和断开取消 |
| httpapi/market_overview.go | 指数、行业、资金、融资、龙虎榜、期指、公告/研报 | DS-P2-06、DS-P3-01 至 DS-P3-08 | market_overview_test.go、market_index_cache_contract_test.go |
| sector/radar*.go、httpapi/theme_progress.go、theme_screen.go | 题材、成员、渐进快照 | DS-P4-02 至 DS-P4-06 | sector/radar*_test.go、httpapi/theme_*_test.go；通配名称表示现有该组测试 |
| httpapi/limit_up_ladder.go、market_emotion.go | 连板、情绪与后台历史采集 | DS-P4-01、DS-P4-05、DS-P4-06 | short_term_progress_test.go、market_emotion_test.go、market_emotion_cancel_test.go |
| httpapi/stock_analysis.go、stock_research.go | 个股量化/AI研究、补充检索和研究验证 | DS-P2-01、DS-P3-07、DS-P5-03 | stock_analysis_test.go、stock_research_test.go、stockanalysis/research*_test.go；保留采集年龄与 cutoff |
| httpapi/portfolio_inspection.go、portfolio_expectation.go、server.go 回调 | 持仓巡检、明日预期复用研究 | DS-P5-03 | httpapi/portfolio_inspection_test.go、portfolioinspection/expectation_test.go；补装配后取源和降级场景，不只测提示词 |
| httpapi/review_market_data.go、review_validation.go | 每日复盘市场输入及次日验证 | DS-P3-07、DS-P4-05、DS-P5-03 | review_market_data_test.go、review_diary_test.go、review/daily_validation_test.go；补新服务装配后的直采替换场景 |
| review/importer.go、automation.go、remote_daily.go | 导入、授权订阅、预整理内容 | DS-P5-01、DS-P5-02 | review/importer_test.go、automation_test.go、remote_daily_test.go |
| methodology/library.go、httpapi/ai_chat.go、mastery.go | 知识取源、提示上下文与知识同步 | DS-P5-06 | methodology/library_test.go、httpapi/ai_chat_test.go |
| httpapi/ladder_theme_ai.go 与 Hermes 显式联网 | 题材补充检索和模型加工 | DS-P4-05、DS-P5-03 | ladder_theme_ai_test.go；保留来源链接，不把生成文本当原始数据 |
| httpapi/source_health.go、source_probe.go | 只读观测、代表接口主动检测 | DS-P1-04、DS-P1-05 | source_health_test.go、source_probe_test.go；真实尝试和缓存/闸门跳过分开 |

表中简写后端业务路径均相对 `backend/internal/`，裸测试名归所在 httpapi 或对应业务目录；通配组只作为测试范围说明，不表示新增文件。矩阵是覆盖索引；本轮新增验证见实施记录，不表示表中每项都新增了独立测试。

已有测试入口：`review/importer_test.go`、`automation_test.go`、`remote_daily_test.go`、`store_test.go`、`daily_validation_test.go`；`methodology/library_test.go`、`httpapi/ai_chat_test.go`；`stockanalysis/research_test.go`、`research_store_test.go`；`httpapi/review_diary_test.go`、`review_market_data_test.go`、`stock_research_test.go`、`persistence_test.go`、`auth_origin_test.go`；前端 `ReviewDiary.test.tsx`、`StockResearchReport.test.tsx`、`StockAIAnalysisWorkspace.test.tsx`。

计划最终命令：在 `backend/` 执行 `go test ./...`；根目录执行 `npm --workspace frontend test -- --run`、`npm run build:frontend`。只有修改桌面启动、桥接或数据路径时，再执行 `npm --workspace desktop test`；Windows UI 布局验证按 `docs/development.md` 的准备条件使用 `./scripts/verify-workspace-layout.ps1`。不使用会停止进程的 `npm run restart` 代替验证。

## 每个新增供应商的接入清单

以下 ID 作为模板，接入具体来源时以 `DS-ADD-<来源ID>-NN` 实例化，不能仅实现客户端即标记接入完成。

- [ ] **DS-ADD-TEMPLATE-01 范围与覆盖**：声明真实实现能力、市场/周期/日期覆盖、凭据方式和公开接口限制，列出未支持能力；登记唯一稳定来源 ID。依赖：DS-P1-01。
- [ ] **DS-ADD-TEMPLATE-02 适配与身份**：实现能力接口，转换价格/数量单位、代码和时区；保留供应商原生身份，处理分页完整性、重复、空值和非法响应。新类型不泄漏到业务服务。依赖：上一项与 P0 契约。
- [ ] **DS-ADD-TEMPLATE-03 语义兼容**：逐字段说明复权、资金统计、热度、题材归属、历史覆盖等是否等价；不等价时确定独立展示、降级或拒绝替代。具备相同接口不代表可加入同一回退链路。依赖：上一项。
- [ ] **DS-ADD-TEMPLATE-04 策略与缓存**：显式加入某能力的策略候选，定义优先级、超时预算、降级字段和缓存键；禁止暗中更改其它能力或恢复退役来源链路。依赖：上一项。
- [ ] **DS-ADD-TEMPLATE-05 探针与观测**：添加代表接口探针和实际能力观测；缓存、取消、未尝试不得造成功状态。设置清单和业务页面解释实际来源及覆盖。依赖：上一项与 DS-P1-04、DS-P1-05。
- [ ] **DS-ADD-TEMPLATE-06 证据与验收**：用脱敏离线夹具验证单位、缺失、坏响应、历史日期、分页与兼容；运行受影响现有测试。需要实时确认时单独执行已授权 live 检查，记录验证日期与范围。依赖：上一项。
- [ ] **DS-ADD-TEMPLATE-07 灰度与回滚**：先在独立验证配置装配，确认 API、来源展示、缓存及研究证据，再切正式策略。保留旧策略和数据可读；停止使用新源后原策略可恢复。依赖：上一项。

## 各阶段回滚方式

| 阶段 | 回滚入口 | 数据处理要求 |
| --- | --- | --- |
| P0 | 回退契约/夹具提交，已有实现继续使用 | 不动用户数据 |
| P1 | 将快讯与目录接线恢复原 `server.go` 装配；保留旧 API 兼容 | 保留设置与来源观测记录；新目录不删除旧凭据 |
| P2 | 按单个价格能力恢复旧路由实现和原来源选择 | 新缓存命名空间停用即可；不得把新基准缓存读作旧基准；不恢复东方财富退役链路 |
| P3 | 每个小能力恢复原组合 Provider 的兼容门面 | 原历史数据、证券目录和查询接口保持可读；不得以恢复为名降级数据库结构 |
| P4 | 恢复原涨停合并/题材服务接线与旧融合算法 | 保留开盘啦快照和旧题材 ID；新融合缓存隔离版本，不批量删除已有数据库 |
| P5 | 恢复原 importer/automation/remote daily 接线 | 保留正文、作者、订阅、去重标识、登录态与研究快照；任何必要数据结构变更单独做可逆迁移 |

回滚前确认当前工作树的修改归属；优先撤回本阶段独立提交或改回具体装配，不能使用重置整个工作树、清空数据库或全局清理缓存来回滚。新旧链路切换不应启动重复采集，回滚后检查实际来源、错误状态与用户可见页面。

## 完成记录模板

每阶段追加记录实际结果，未执行的检查写明未执行；不要提前勾选任务。

```text
阶段 / 任务 ID：
实施提交或改动范围：
默认来源策略是否保持：
HTTP / 配置 / 数据兼容：
实际运行命令、工作目录及结果：
未执行检查与原因：
回滚入口与验证结果：
遗留问题 / 下一阶段依赖：
```

## 2026-10-02 架构实施记录

- 范围：contracts/registry/runtime/service/assembly；价格、市场总览、人气、期指与涨停编排；中立题材快照及成员路由；文章/浏览器/内容服务/远程归档/GitHub 适配；来源目录及前端设置。旧构造器/注入接口与题材存储生命周期保留为明确兼容层。
- 默认策略：保留七家公共来源原有主备、并行、合并、降级。退役东方财富价格/指数路由继续拒绝；没有引入新真实供应商或付费凭据。
- 新增验证：独立研报能力、注册快讯 API/研究/独立探针、注册严格复权来源、独立指数和期指会员来源、能力移除/禁用、空集合不恢复默认源、来源目录身份冲突、内容域名/授权传递、主题原生来源/错误身份/交易日/取消、旧题材库在移除后可读。原标题/归档哈希和旧订阅行为由既有回归验证。
- 最终审核修复：详情报价实际请求恢复唯一成功观测、缓存不续期；禁用能力不误报无关来源失败；供应商主动取消不再请求备用或记录故障；知识缓存按来源/集合/目录/版本验证，部分下载失败保留旧正文及旧 v4 清单兼容；空摘要保留自动摘要；题材私有码和强度缓存按来源隔离，个股研究按中立归因角色消费替换来源。
- 后端最终实际命令：从 `backend/` 使用 `../.runtime/tools/go/bin/go.exe test -json -count=1 ./...`，退出码 0；31 个有测试包通过、931 项通过、16 项因实时上游/运行时前提跳过。相关修复另有 HTTP/stockanalysis/service、methodology/review/githubknowledge、sector 定向回归。
- 前端实际命令：根目录 `npm.cmd --workspace frontend test -- --run`，57 文件、387 项通过；`npm.cmd run build:frontend`，TypeScript 与 Vite 构建通过，保留现有大 chunk 提示。
- 静态审查：契约/目录/预算无 HTTP、供应商或业务依赖，能力服务无真实网络请求或具体供应商导入，sector 无具体供应商导入；review/methodology 取源请求已抽到适配器。`git diff --check`（Windows CR-at-EOL 设置）通过，专项文档相对链接已检查。
- 未执行：公网实时检查、Go race（本机 CGO_ENABLED=0，未配置 C 编译器）、桌面打包/测试、真实浏览器和 Windows 布局验证。本轮未改桌面启动、鉴权或数据路径；不把离线测试当作上游长期可用保证。
- 数据和交付：验证仅用测试夹具/临时库，没有重置用户库、设置、登录态或 Hermes Home。按用户授权在审核与回归完成后提交并推送当前 fork 的 main，实际提交和远程确认结果在交付回复中记录。回滚通过恢复具体注册/路由或对应提交；未做整工作树重置或数据库清理。
- 后续：先完成保留治理项中与具体供应商替换有关的语义/错误/事件，再按 DS-ADD 模板替换、移除或新增。全新订阅平台还需对应业务授权和界面接线；运行中热切换尚未提供。

## 2026-10-03 数据源治理修复

- 数据真实性：财务缺失/真实零及扣非同比独立、普通/新股评分门控；腾讯行业完整输入才评分；融资保留市场覆盖/字段有效性，只比较完整相邻交易日；龙虎榜原生编码不冒席位数量；新情绪及研究来源由实际输入生成，不重写旧记录。
- 结果完整性：涨停字段presence及补值出处/抓取时间、深克隆；可选普通/渐进History保留成功空池与缺失日，无二次取数；HTTP/情绪不跳过空日，缺相邻日禁晋级。未知板数不冒首板，必要情绪字段不足不更新分数。
- 治理：东财JSON GET唯一有限重试、类型错误/HTTP状态/取消与超时/429冷却；行业/资金主备共享预算；公告正文4并发/总预算与内容状态。THS标签独立能力及可禁用路由，32日期/12小时缓存，查看者取消不影响其它查看者，HTTP200挑战页不是能力成功。
- 目录：真实槽位生成canonical列表并校验旧别名；不登记雪球未实现AuthorLinks、中金所伪历史能力；服务装饰器保留指数支持判断，探针继续独立直接检测七源。
- 兼容与边界：保持默认真实供应商、API与loopback鉴权、已有库/历史报告；没有引入凭据、Python或新源；后续按用户明确授权审核后提交/推送，实际结果以交付答复为准。未完成全量网络Attempt/Outcome、全部遗留缓存/错误统一；新执行状态目前集中THS标签链，不能泛称所有供应商已迁移。
- 实际验证：指定本地Go、GOPROXY/GOSUMDB=off，`go test ./... -count=1`及`go vet ./...`通过；THS共享生命周期`-count=10`通过。提交前审核补修龙虎榜AI字段mask、旧报告扣非同比显示、主源独立空日覆盖与单日超时partial边界；对应回归通过，后端全量/vet再次通过，前端全量58文件/393项与`npm.cmd run build:frontend`通过（现有大chunk提示保留）。`git diff --check`通过。
- 开发后端已用原Windows隔离Web脚本重启，health返回ok；本机浏览器融资页确认最新仅沪市覆盖、不再显示伪全市场骤降，变化为--，控制台无页面错误；来源目录THS标签和CFFEX单日能力正确。仅低频浏览器/接口样本，不表示长期稳定。Windows Go race未通过前提（CGO未启用），未运行桌面或完整布局回归；不伪报全业务实网通过。
