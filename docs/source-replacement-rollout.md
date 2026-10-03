# 东方财富部分能力替代：四阶段交付与边界

## 策略

按能力选择来源，不整体关闭东方财富。保留涨跌停/炸板池、目录概念字段、基本面、公告/研报、融资、龙虎榜等仍可取数的能力；mootdx 未通过本机报价/K验证，不加入正式链路。已存在的用户未提交改动在本轮保留，无新增Token/付费要求。

## 最新范围：价格退役，第一批公司资料/财务迁至新浪

价格三类已迁出：默认股票K新浪→腾讯、明确none/qfq/hfq腾讯、指数快照/历史腾讯独立。2026-10-03第一批公司主营/简介和已披露财务新增新浪适配并设主源，东财仅作显式整份备用；主源完整无东财请求，失败/覆盖不足才回退，不混报告期或字段。实网四股样本与隔离消费者投影结果见本页下方及数据源说明。以下价格阶段及历史采样证据保留不改写。竞价、原始目录/分类、事件池、公告/研报等仍保留，并非本批已替代。

架构迁移后，能力契约与默认注册/路由位于 `backend/internal/datasource/`，单源协议仍在 `providers`。默认策略与本页历史公网证据保持；新增适配器须注册、配置路由并验证口径。详细当前职责见 [数据架构](../backend/docs/architecture.md)，完成与保留项见 [改造清单](data-source-refactor-checklist.md)。显式价格接口可接受注册且启用的严格复权源 ID，默认仍为腾讯；不会恢复东财价格/指数能力。

## 第一批公司资料/财务迁移验证（2026-10-03）

- 新浪官方财务端点 `quotes.sina.cn/.../CompanyFinanceService.getFinanceReport2022`，公司资料 `vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/{code}.phtml`；匿名公开请求，不读取Cookie、模型凭据或登录态。
- 新注册适配器+Company路由实网四股：600519.SH/000001.SZ/920002.BJ/920045.BJ，资料和财务均为新浪、无东财网络请求；中报有效指标11/10/11/11，银行毛利不适用；披露日8/15、8/15、8/3、8/21，来源/日期保留到分析和研究快照。原始公开证据仅存Git忽略 `.runtime/sina-company-migration-20261003/`。
- 显式公司live测试使用真新浪资料、其它能力脱网夹具及内存库，6次新浪请求打通快速分析HTTP/研究证据/持仓共享入口；不调用AI、不运行全市场刷新。命令和范围见 [live tests](../backend/docs/live-tests.md)。
- 失败/缺字段/旧季度/未知披露日/银行不适用/取消/预算/禁用fallback/历史报告与截止证据均有离线回归；完整主源不请求东财，部分数据按整份选择，绝不跨源拼字段。
- 官网公开成功不是授权SLA或稳定性保证；当前官方日期为日粒度，不冒精确盘中披露时间，非历史PIT/修订仓库。公告研报及其它东财功能本批不替换。回滚仅调整Business/Fundamentals及其fallback路由，不改用户库或历史报告。

## 阶段1：默认路由和失败边界

- 默认个股 K 为新浪 → 腾讯 none（仅支持的股票日/周/月），每次返回单一供应商快照，不拼历史。
- 主备请求分配剩余预算，200空/非法OHLCV不算成功，取消不记供应商失败，未支持能力不尝试也不记失败。
- 默认/指定K各自按来源、市场、周期和调整隔离；连续两次失败后30–120秒能力退避，不关闭其他接口。
- 东财内部重试等待响应取消，不额外消耗后备预算。
- 新链路使用结构化 SourceObservation + capability；设置展开能力记录可见单接口失败，即使该供应商其他接口成功。缓存命中不生成新观测。

## 阶段2：腾讯股票 K 和明确复权

- GET `/api/v1/quotes/kline` 原symbol/period/adjust兼容，使用 `provider=source|tencent`；`provider=eastmoney` 明确400退役，不静默映射腾讯。
- 缺省provider＋指定adjust改为腾讯；这是明确的默认契约迁移，不宣称与历史东财同qfq/hfq算法/因子/锚点等价。旧东财历史报告保持原来源，不重标腾讯。
- 腾讯精确解析day/qfqday/hfqday、week/qfqweek/hfqweek、month/qfqmonth/hfqmonth；请求key缺失不退raw。
- 沪深已支持板块：SH60主板、SZ00主板、SZ30创业板量原生手→股；SH688科创原生股保留。未知前缀/北交所/分钟股票K不宣称腾讯支持。有效源内报价量额证据若矛盾拒绝转换。
- 响应包含actual basis/adjustment、股票身份、日期、单位与字段掩码。历史缺成交额不借当日报价补，新浪真实分钟amount仅存在时可用。
- 年K从同一来源、口径、单位月K聚合；已知字段取交集，月换手率不当年换手，首年未知昨收不冒用月昨收，负/零合法复权价保留；none交易价须正。
- 前端来源、复权独立选择；basis改变重新加载/计算图表，量单位优先使用metadata，防止再次×100；分时补点仅同日同basis。
- `asof`历史因子回溯未实现，显式拒绝，不把截断当前复权历史当历史时点复权。

## 阶段3：行业、成员、资金保真

- 已有腾讯行业强度、新浪三类资金主源继续使用，不包装成新数据供应商。
- `BoardRef`携带provider/native_code/dimension，行业及fusion存明确身份，legacy仅真正缺p/d字段可推断；空/null不走兼容猜测。
- fusion与纯industry统一成员路径，BK不送腾讯pt接口。目录匹配候选、原生成员、开盘啦领涨独立分组，不合并后称全量。
- MemberSet标kind、total、returned、complete、has_more、scope/method；腾讯分页200、上限500，重复/失败/超过范围不冒称complete。
- HTTP `complete`仍为候选池阶段就绪，不代表行业全集；新增 `membership_complete/membership_scope`表达原生成员完整性。
- 行级known字段区分缺失、null、--、非有限与有效0；价格存在不证明五日涨幅存在。评分、界面、排序与AI prompt共同遵守。
- 新浪板块总净流入不是东财主力净流入；不同列明确标名。东财bkzj降级仅正确维度/有效主力字段，排序不足标requested/effective sort。

## 阶段4：指数全球补缺与竞价评估

- 指数取数仅腾讯，空/失败也不触发东财；NDX=nasdaq100、IXIC=nasdaq_composite，legacy nasdaq仍NDX。
- 全球目录未覆盖的ID通过partial/missing_ids说明，不用相近指数代替。中国指数使用完整报价时间；foreign timestamp偏移未确认保留NativeTimestamp且unknown，不套宿主或纽约时区。
- 日/周/月指数历史用交易日期标签（UTC序列化，不代表当地开盘时刻）；移除东财后指数分钟不支持，提前拒绝且不记上游失败。首根无前收时涨幅未知，排序后计算因果涨幅，各行mask保留。
- 竞价候选没有证实同语义独立备用，未接入新浪/腾讯普通分时当竞价；东财失败返回unavailable、无点或同股同日旧点stale。不会造09:15–09:25参考价或成交量。

## 本机代表上游验证

2026-10-01（休市），直接fresh provider/上游请求，无应用缓存/其他供应商代答；成功只证明该次接口及样本，不保证未来或盘中实时性。

- 腾讯股票：万科day none/qfq/hfq、茅台week qfq/month hfq有效；额外科创688300/创业300750 raw+同响应quote量额验证单位差异。原始数据未复制成历史amount。
- 新浪：minute/day/week/month四请求有效，分钟实际amount在源响应存在。
- 腾讯：行业强度5条、真实行业成员5/52（不完整）有效；新浪stock/industry/theme资金各5条有效。
- 全球候选：NDX与IXIC报价/历史标的分别有效，其余部分核心报价有效；日经/韩国/台湾/德国/法国未取得独立可靠补缺覆盖。
- 竞价候选：腾讯普通分时起始交易时段、新浪minute非独立盘前轨迹；东财盘前连接关闭，未宣布替代成功。

原始证据在Git忽略目录 `.runtime/price-source-validation.md`、`.runtime/price-kline-evidence-20261001-174756/`、`.runtime/price-sina-evidence-20261001-180128/`、`.runtime/price-board-unit-evidence-20261001-182112/`、`.runtime/sector-source-evidence.json`、`.runtime/index-auction-evidence.json`。早期PowerShell解析提示失败保留为工具错误，不归类为上游故障。

## 2026-10-03 公司资料与财务第一批迁移验收

- 官方目标仅新浪，本批没有替换腾讯或新增供应商。主营/简介：[新浪公司资料](https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/600519.phtml)；财务：[新浪结构化接口](https://quotes.sina.cn/cn/api/openapi.php/CompanyFinanceService.getFinanceReport2022?paperCode=sh600519&source=gjzb&type=0&page=1&num=1)。两端点本轮直接官网读取HTTP200，字段含自身公司身份、明确主营/简介，以及报告期20260630/披露20260815/合并累计CNY。
- 注册适配器+默认 `Company` 路由 fresh 实网四股600519.SH、000001.SZ、920002.BJ、920045.BJ，共8次官方请求；资料来源均sina:business、财务均sina:financials；非银行11项有效指标，银行10项+毛利N/A。报告披露日期依次8/15、8/15、8/3、8/21，无东财网络请求或回退。
- 显式live衔接检查 `TestLiveSinaCompanyMigrationReachesHTTPResearchAndHolding` 通过，茅台6次新浪官方请求经真实HTTP快速分析、研究快照及持仓共用分析器传递；财务来源/披露日期和公司原文链接保留。无关价格/题材/资讯为确定夹具、库为内存，不调用模型、不使用密钥/登录态，不把这些夹具当行情验证。
- 回退离线覆盖：新浪失败或必要指标不足才整份东财备用、主源完整无备用请求、旧报告不覆盖有效新报告、取消不回退、父预算内给备用预留、bank N/A不当0、未知披露不静默退旧季度；披露晚于截止时间时证据和评分基线共同排除。未知资料更新时间/旧东财披露日期不伪造。
- 本机忽略证据 `.runtime/sina-company-migration-20261003/official-final-samples.json`、`http-holding.json`；每次公开样本成功仅证明当次覆盖，不承诺SLA/盘中稳定/完整历史PIT修订库。
- 后端全量与vet通过；前端58文件/394项和生产构建通过（现有大chunk警告保留）；未运行CGO race、桌面打包/启动或全工作区布局回归。最终审核及提交推送结果以交付回复为准。

## 验收

离线测试覆盖多周期精确key、合法负复权价、supplier身份、单位不重复转换、失败预算/取消、指数标的/分钟时刻、缺字段与真实零、成员分页/候选隔离、UI来源切换和AI缺值。本轮通过情况以交付答复中的实际执行结果为准，不用该文档替代测试日志。更多全历史/因子asof、完整概念成员、全球缺项、竞价独立备用和盘中持续稳定性仍需后续有效来源与交易时段验证。
