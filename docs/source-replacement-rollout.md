# 东方财富部分能力替代：四阶段交付与边界

## 策略

按能力选择来源，不整体关闭东方财富。保留涨跌停/炸板池、目录概念字段、基本面、公告/研报、融资、龙虎榜等仍可取数的能力；mootdx 未通过本机报价/K验证，不加入正式链路。已存在的用户未提交改动在本轮保留，无新增Token/付费要求。

## 最新范围：价格链路已退役东财

用户进一步授权逐步替换全部东财能力，本轮先移除三类价格能力：默认股票K新浪→腾讯、明确none/qfq/hfq统一腾讯、指数快照/日周月历史Tencent独立。以下阶段描述反映当前实现；历史公网采样证据保留不改写。尚未找到等价替代的竞价和专属研究/资讯继续保留，并明确它们不是本轮已移除项。

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

## 验收

离线测试覆盖多周期精确key、合法负复权价、supplier身份、单位不重复转换、失败预算/取消、指数标的/分钟时刻、缺字段与真实零、成员分页/候选隔离、UI来源切换和AI缺值。本轮通过情况以交付答复中的实际执行结果为准，不用该文档替代测试日志。更多全历史/因子asof、完整概念成员、全球缺项、竞价独立备用和盘中持续稳定性仍需后续有效来源与交易时段验证。
