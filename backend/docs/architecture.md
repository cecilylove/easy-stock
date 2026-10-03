# easy-stock 架构与数据基座

## 数据访问边界

后端把上游行情和内容转换为项目模型，提供本机 HTTP / WebSocket API，支持题材、连板、研究、持仓巡检、复盘和知识工作台。默认来源没有因本轮架构迁移而更换。

当前依赖路径为：HTTP/业务消费接口 → `datasource/service` 能力服务 → 注入的单源能力 → `providers` 适配器 → 上游。组合根 `httpapi/server.go` 通过 `datasource/assembly` 构造和校验来源，不让供应商反向依赖 HTTP 或业务实体。

| 模块 | 职责 |
| --- | --- |
| `internal/foundation/` | 标准行情/文章/题材模型、来源元信息、原生身份、质量及日期 |
| `internal/datasource/contracts/` | 报价、价格、指数、行业、资金、资料、快讯、内容等小接口；有类型的请求与错误 |
| `internal/datasource/registry/` | 不可变供应商描述、已实现能力槽位和独立探针登记 |
| `internal/datasource/assembly/` | 默认单源客户端构造、内容登记、显式路由及启动校验 |
| `internal/datasource/runtime/` | 尊重父上下文的请求预算与取消；不叠加全局重试/缓存 |
| `internal/datasource/service/` | 标准访问、价格选源/退避/严格复权/年 K、市场主备、人气并行、期指降级、涨停合并及内容分派 |
| `internal/providers/` | 上游协议、签名、分页、单源解析、单位转换和协议校验；旧组合入口只作兼容委托 |
| `internal/sector/` | 中立题材快照、完整 BoardRef 成员路由、映射、强度和融合 |
| `internal/stockanalysis/`、`portfolioinspection/` | 研究证据、指标、报告、验证、持仓巡检及任务归档 |
| `internal/review/` | 文章到 Post 的投影、订阅/授权状态、调度、去重、远程归档校验及复盘加工 |
| `internal/methodology/` | 知识种子、本地缓存/索引、版本和 Hermes 同步；GitHub 下载由适配器负责 |
| `internal/httpapi/` | 参数/鉴权/兼容响应、HTTP/WS 生命周期、场景缓存、业务组合根与观测投影 |
| `internal/hermes/` | 模型配置、进程/会话生命周期与提示调用 |
| `desktop/`、`frontend/src/` | Electron 运行隔离与 Token 桥接；工作台、图表及来源展示 |

`contracts` 可以依赖 `foundation`，后者不反向依赖数据源层。注册和预算不依赖访问服务。能力服务不导入具体供应商；集中装配允许导入供应商。旧 HTTP Provider 接口通过别名继续可注入，`MarketOverviewProvider` 等大接口保留为消费门面，供应商只实现对应的小能力。

## 注册、替换与移除

`Config.DataSources` 和 `DataSourceRoutes` 注入公共取数集合与路由；`ContentSources` 注入授权/内容集合。两者使用相同 Registry 类型，来源 ID 在合并目录中必须唯一。默认装配为七家公共来源和五项内容来源；内容不参加公共代表接口检测。目录分类与接入模式是描述信息，能力槽位和 Routes 才决定真实调用。

新增已有能力的来源：实现 `contracts` 中对应的小接口，在 assembly 注册 `Descriptor`、能力和适用探针，再将该能力的路由指向它。只提供研报的来源无需实现行情或融资；未支持能力不注册。期指 `FuturesSnapshot`、`FuturesMembers`、`FuturesConsensus` 可各自覆盖，`FuturesExchange` 仅保留默认三项同源的兼容写法。路由指向缺失、禁用或未实现能力会报告 `StartupError`，启动入口拒绝继续服务。移除来源时同步移除/替换相关路由；显式空注册集和空路由不会恢复默认客户端。

内容文章依据登记的 `ArticleHosts` 路由；浏览器采集、作者链接、授权正文、远程归档和知识目录/文档是不同能力。登录态与订阅状态仍由原业务管理；`enabled` 不代表已登录。显式内容配置使用安全能力包装，禁用/移除返回 Unsupported，不触发旧构造器恢复默认网络源。

注册在启动时完成，没有插件加载、运行中热切换或用户设置里的任意路由 DSL。新增业务能力仍需扩展契约和消费者。新文章域名可通过 ArticleHosts 接入；全新订阅平台涉及授权配置/订阅 UI 和业务校验，需要同步扩展，不能仅登记名字就启用。接口一致不保证语义一致：供应商的复权、资金统计、榜单排名、历史覆盖和原生板块成员必须通过契约验证后才能替换。具备实现的目录能力由实际槽位生成，旧描述别名显式映射校验；CFFEX只注册单日快照/会员/共识，不注册伪历史能力。

## 策略、证据与兼容

默认股票 K 新浪 → 腾讯；明确复权由腾讯按自身基准提供；指数使用腾讯独立链路。东方财富股票 K、明确复权和指数能力在路由校验中保持退役，其余未可靠替代能力继续保留。具体默认策略见 [数据源说明](data-sources.md)。

价格路由、复权校验与年 K 聚合集中到能力服务；指数支持范围可由独立供应商声明。后台情绪价格采集复用同一路由服务。题材快照新增来源元信息，新路由校验供应商身份；旧 SQLite JSON 无该字段时保留原开盘啦归属，不重标历史数据。`kpl:`、`industry:`、`fusion:` 及旧评分响应字段继续可读。

缓存按原消费场景保留：详情轮询、WS、总览、研究和题材持久刷新闸门不套统一 TTL。缓存命中、未尝试和取消不续期来源健康；手动探针与业务观测独立。价格/指数/行业/资金等能力记录真实来源，浏览器/内容通过各自任务和授权状态展示。本轮没有把所有遗留协议改成统一 Attempt/Result 事件；无观测不能断言未取数。

`providers/marketoverview`、`hotstock.Client.HotStockRanks`、`futuresposition.Client.Trend`、`duanxianxia.NewLimitUpProvider` 保留薄委托兼容入口。题材快照 SQLite 和五分钟刷新闸门仍保留在 duanxianxia 的历史生命周期服务中，但它接收中立能力，sector 不导入该供应商。THS席位标签已从东财明细取数中拆为独立适配器/路由，由访问服务可选装饰原始明细；失败不破坏明细，禁用不请求THS。东财JSON GET重试保留唯一所有者，结构化错误和429冷却已落实；行业/资金主备预算预留备用时间，公告正文有界并发。涨停补字段记录字段来源/抓取时间，History/ProgressiveHistory保留成功空日及缺日；普通/渐进HTTP与情绪按真实覆盖、相邻交易日消费。财务有效性和派生评分显式区分；其它全量网络事件/错误转换及遗留缓存/评分角色清理仍是后续治理，不能叠加第二层真实请求。

## 本机与模型运行时

Electron 分配 loopback 端口、生成随机 Token、启动 Go 子进程并等待 `/api/health`；HTTP 和 WS 使用原鉴权。保持 loopback Host、受信 Origin、桌面 Token 和用户数据隔离，入口见 [开发说明](../../docs/development.md)。

模型密钥、配置和会话仍由 Hermes 管理。显式联网分析保留实际链接和检索范围；模型输出不是供应商市场事实。未接入默认装配的 `strategy/inflection/clickhouse.go` 保持原用途。

## 实施与验证

[架构方案](../../docs/data-source-architecture-plan.md) 说明契约与长期约束；[改造清单](../../docs/data-source-refactor-checklist.md) 记录完成项、保留项和后续接入模板。默认单元测试模拟上游；公网样本只能证明当次请求，不承诺长期可用性。供应商替换单独遵循 [替换计划](../../docs/source-replacement-rollout.md)。
