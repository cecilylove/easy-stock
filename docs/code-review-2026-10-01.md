# 全仓代码审查：2026-10-01

审查基线：`main`，`7ebd3c52c06979f790f33c76d72d5569ed54cdcf`。主智能体与三个子智能体分别检查跨模块边界、后端、前端、桌面及发布代码，确认 **13 项：3 项 P1、10 项 P2**。2026-10-01 已全部修复并完成回归。下文原始证据与行号对应审查基线；交付状态与持久测试见末尾修复记录。

P1 表示应优先处理的数据安全、权限或二开版本保护问题；P2 表示会在指定条件下造成错误数据或功能不可用的缺陷。顺序兼顾影响和本项目当前使用场景。

## 问题索引

| ID | 级别 | 问题 |
| --- | --- | --- |
| D1 | P1 | 自定义共享备份目录可能被清理掉其他项目的文件夹 |
| S1 | P1 | 无 Token 的 Web 服务接受不可信 Origin 修改设置 |
| D2 | P1 | fork 桌面包默认仍从原作者下载更新，安装后覆盖二开 |
| B1 | P2 | K 线主源返回空数据时跳过健康备用源 |
| B2 | P2 | 主来源慢故障耗尽总预算，备用来源不能实际请求 |
| B3 | P2 | 取消一次盘中情绪刷新，会缓存取消错误 10 分钟 |
| B4 | P2 | 来源故障把已有题材强度由正常值覆盖为零 |
| F1 | P2 | 行情总览切换类别时，旧请求覆盖新类别及 AI 证据 |
| F2 | P2 | 切换复盘日期后，明日预期按钮仍打开上一日报告 |
| F3 | P2 | 趋势旧报价覆盖更新快照，并继续标注为实时 |
| F4 | P2 | 五日相对强度的股票、题材比较口径不一致 |
| D3 | P2 | 桌面研究数据库遗漏路径映射，脱离隔离目录及备份 |
| D4 | P2 | Windows 路径大小写绕过备份目录包含检查 |

## 确认问题与证据

### D1：备份清理可能误删其他项目目录

位置：[data-protection.cjs:130](E:/code/project/easy-stock/desktop/data-protection.cjs:130)，删除位置 [137](E:/code/project/easy-stock/desktop/data-protection.cjs:137)。

- 触发：将支持的 `A_STOCK_UPDATE_BACKUP_DIR` 指向已有共享目录，再创建更新备份。
- 原因：保留策略把所有非 `.partial-` 子目录当应用备份，没有验证归属或 manifest，然后递归删除超过保留数量的目录。
- 复现：新建四个只有合成内容的 `unrelated-project-*` 目录；创建应用备份并保留三份后，两个无关目录实际被删除。没有访问真实用户目录。
- 修复方向：使用应用专属子目录，只清理已验证的应用备份；未知目录保持不动。

### S1：本机 Web 服务的跨域写操作缺少来源限制

位置：[auth.go:9](E:/code/project/easy-stock/backend/internal/httpapi/auth.go:9)、[auth.go:20](E:/code/project/easy-stock/backend/internal/httpapi/auth.go:20)、[server.go:446](E:/code/project/easy-stock/backend/internal/httpapi/server.go:446)。

- 触发：默认 Web 开发模式没有 `A_STOCK_TOKEN`，不可信页面获准访问本机服务。
- 原因：服务反射任意 Origin，允许 PUT/DELETE 等预检；Token 为空即放行所有 API。监听 loopback 不构成浏览器来源隔离。
- 复现：隔离内存设置中的 OPTIONS 返回 204 并允许 `https://untrusted.example`；同来源无鉴权 PUT 返回 200，实际修改模型配置。测试没有改变真实设置。
- 边界：这是服务端请求复现，不是浏览器端绕过本地网络访问授权的演示；现代浏览器可能额外要求本地网络访问权限。随机 Token 保护的正常桌面服务不受该无 Token 条件影响。
- 修复方向：校验受信 Origin/Host，并让 Web 敏感操作也要求本机会话凭据；保留合法前端和 Electron 的访问方式。

### D2：原作者更新会覆盖 fork 桌面版本

位置：[update-feed.cjs:1](E:/code/project/easy-stock/desktop/update-feed.cjs:1)、[main.cjs:306](E:/code/project/easy-stock/desktop/main.cjs:306)、[main.cjs:456](E:/code/project/easy-stock/desktop/main.cjs:456)。

- 触发：打包 fork 后没有运行时更新源覆盖，用户接受较新的上游更新。
- 原因：默认更新源仍是原作者 OSS，发布链接仍是 `jundizhou/easy-stock`。仅在构建时覆盖地址也不够，运行时 `setFeedURL` 会重新取环境变量或默认值。
- 证据：实际 resolver 返回 `https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop`；代码允许 Windows 安装该更新。未执行下载或安装。
- 影响：更新后的程序来自上游包，当前二开功能会被替换。当前 Web 开发服务不受此安装路径影响。
- 修复方向：默认关闭 fork 更新，或打包固定 fork 自有更新源、版本标识和发布链接。

### B1：主源空 K 线跳过备用源

位置：[client.go:183](E:/code/project/easy-stock/backend/internal/providers/eastmoney/client.go:183)、[server.go:797](E:/code/project/easy-stock/backend/internal/httpapi/server.go:797)。

- 触发：东方财富响应 `rc=0,data=null` 或空 klines，新浪有有效 K 线。
- 原因：空切片和 nil error 被视作成功，立即返回；详情分钟线虽在外层把空数据转成错误，也不会再次走备用。
- 复现：真实客户端连接本机模拟来源，返回 HTTP 200 `{"data":[]}`，健康新浪实际请求次数为 0。
- 修复方向：主源结果为空时尝试备用；两者确无数据时保留准确的无数据语义。

### B2：慢主源耗尽备用预算

位置：[server.go:806](E:/code/project/easy-stock/backend/internal/httpapi/server.go:806)、[stock_detail_poll.go:110](E:/code/project/easy-stock/backend/internal/httpapi/stock_detail_poll.go:110)。

- 原因：主、备用共用一个截止时间。详情总预算 12 秒，主源单次 HTTP 超时 15 秒；普通总预算 20 秒也可能被主源及重试用尽。
- 复现：本机慢主源和健康备用，缩短总预算至 100 ms；主源超时后备用请求次数为 0，返回 deadline exceeded。默认预算由同一调用链和配置核对，未等待公网故障。
- 影响：快失败能降级，慢故障不能降级；还可能把没有真正收到请求的备用来源记为失败。
- 修复方向：给主源独立预算，为备用留时间，同时保留用户取消传播。

### B3：取消刷新会造成 10 分钟错误缓存

位置：[market_emotion.go:91](E:/code/project/easy-stock/backend/internal/httpapi/market_emotion.go:91)、[101](E:/code/project/easy-stock/backend/internal/httpapi/market_emotion.go:101)、[115](E:/code/project/easy-stock/backend/internal/httpapi/market_emotion.go:115)。

- 触发：无成功快照的首个盘中情绪请求被取消；用户重新进入页面。
- 原因：取消错误与成功结果使用同样的 10 分钟 TTL，新请求直接返回缓存错误。已有快照时也会被标陈旧并阻止刷新。
- 复现：第一次返回 context.Canceled；第二次使用正常上下文、可成功 loader，仍返回取消错误，loader 调用次数为 0。
- 修复方向：区分用户取消、服务超时和供应商故障；取消不得建立长期负缓存。

### B4：缺数据被误算成题材零强度

位置：[radar_strength.go:55](E:/code/project/easy-stock/backend/internal/sector/radar_strength.go:55)、[46](E:/code/project/easy-stock/backend/internal/sector/radar_strength.go:46)、[radar_fusion.go:167](E:/code/project/easy-stock/backend/internal/sector/radar_fusion.go:167)。

- 触发：此前计算成功；刷新时成分/行情失败，但题材保留只有名称和代码的龙头。
- 原因：非空股票名单被当作可计算数据；无有效涨幅算出零且返回成功，覆盖旧缓存，再作为有效指标参与融合。
- 复现：成功强度缓存从 `90/90` 被覆盖为 `0/0`，随后保留至少 10 分钟。
- 修复方向：以有效数值和样本数判断可计算性，区分缺失与真实零，故障时保留可用旧结果及降级标记。

### F1：行情总览旧请求写入当前类别

位置：[MarketOverviewWorkspace.tsx:183](E:/code/project/easy-stock/frontend/src/components/MarketOverviewWorkspace.tsx:183)、[257](E:/code/project/easy-stock/frontend/src/components/MarketOverviewWorkspace.tsx:257)。

- 触发：行业资金请求较慢，切换到个股资金并先收到新结果；旧行业结果稍后返回。研究分类共用状态有同类风险。
- 原因：没有取消、请求序列或查询身份校验，多个类别共用 flows/research/moduleMeta。
- 复现：当前类别始终 stock-flow，新 stock_new 被迟到 industry_old 覆盖。AI 证据直接使用这些共享状态。
- 修复方向：按完整查询键隔离结果，并在写入前校验请求身份；AI 只使用当前键有效结果。

### F2：持仓明日预期沿用前一复盘日任务

位置：[PortfolioTomorrowExpectation.tsx:23](E:/code/project/easy-stock/frontend/src/components/PortfolioTomorrowExpectation.tsx:23)、[84](E:/code/project/easy-stock/frontend/src/components/PortfolioTomorrowExpectation.tsx:84)。

- 触发：同一组件切换复盘日期，新日期 latest 返回 null 或持仓不匹配。
- 原因：不清理旧 job/reportOpen；主按钮只判断 report_available，不校验日期。
- 复现：日期从 09-29 改为 09-30，最新任务为空；点击仍打开 09-29 报告，没有创建 09-30 任务。
- 修复方向：按日期、后端和持仓身份隔离任务，切换时清旧状态并校验返回结果。

### F3：旧趋势报价覆盖更新快照并标为实时

位置：[App.tsx:925](E:/code/project/easy-stock/frontend/src/App.tsx:925)、[short-term.ts:221](E:/code/project/easy-stock/frontend/src/lib/short-term.ts:221)。

- 原因：mergeQuotes 丢掉 trade_time/meta；liveQuotes 不过期，报价对象存在就认定实时。
- 触发：连接中断、离开工作台、跨日后刷新成分快照，但该股票没有新 WS 报价。
- 复现：更新成分快照 price=11，被历史报价 price=9 覆盖，输出 live=true；旧时间和陈旧标记没有保留。
- 修复方向：保留来源/时间，以交易日和 TTL 校验，比较快照新旧，断线后准确标记旧报价。

### F4：五日相对强度使用不同收益口径

位置：[short-term.ts:266](E:/code/project/easy-stock/frontend/src/lib/short-term.ts:266)、[594](E:/code/project/easy-stock/frontend/src/lib/short-term.ts:594)。

- 原因：股票收益为五个间隔复合收益，题材基准却跳过五日数组的第一日，并简单相加其余四日。
- 复现：单成分题材连续五个间隔各 +10%，股票与题材实际完全一致，仍算出相对领先 `21.051` 个百分点，生成“领先21.1个百分点”的证据。
- 影响：领导力评分和角色/风险证据存在系统性偏差。
- 修复方向：股票与题材使用相同日期间隔及复合收益口径，显式处理缺失日期。

### D3：研究库遗漏桌面数据目录映射

位置：[main.cjs:362](E:/code/project/easy-stock/desktop/main.cjs:362)、[backend main.go:27](E:/code/project/easy-stock/backend/cmd/server/main.go:27)。

- 原因：buildRuntimeEnv 映射其他数据库，却遗漏 A_STOCK_RESEARCH_DB，Go 自行回到系统配置目录；更新备份只包含 Electron userData。
- 复现：实际函数受控执行不含该变量；合成旧版场景 Electron 选择 desktop，Go 默认分支选择 easy-stock。
- 影响：自定义/旧版数据目录的研究记录突破隔离并漏出备份；默认全新安装通常一致，显式继承研究库路径及当前 Windows 启动脚本能规避。
- 修复方向：补齐研究库路径，制定已有错位记录迁移方案。

### D4：Windows 大小写绕过备份目录检查

位置：[data-protection.cjs:29](E:/code/project/easy-stock/desktop/data-protection.cjs:29)。

- 触发：用户数据 `C:\Users\Wlh\Data`，备份根设置为 `c:\users\wlh\data\backups`。
- 原因：字符串包含检查区分大小写，Windows 默认文件系统不区分；实际位于数据内的备份目录被放行。
- 复现：实际函数接受上述路径。没有执行自我递归复制或耗尽磁盘。
- 影响：备份创建的 staging 目录会进入被复制的 userData，递归包含自身，直到路径/文件系统失败。
- 修复方向：使用平台一致的包含判断和真实路径检查，并覆盖 junction/symlink 别名。

## 原始审查验证与覆盖

- `backend/`：`go test ./...`、`go vet ./...` 成功。
- 前端：`npm --workspace frontend test -- --run`，35 个文件、171 项通过；`npm run build:frontend` 成功，已有超过 500 kB chunk 提示。
- 桌面：`npm --workspace desktop test`，58 项中 52 通过、6 项平台相关跳过，无失败。
- 主智能体实际重跑后端四组、前端四组、桌面合成场景；另运行跨域权限复现。复现脚本 PASS/exit 0 代表观察到当前错误行为，不表示问题已经修好。
- 后端临时测试通过 Go overlay 注入；前端用实际源码转译及可控 hook/API harness；桌面用实际函数及合成目录。均没有改生产源码或使用真实账号/设置/数据库。
- 后端覆盖启动、HTTP/API、全部 Provider 家族、题材、研究、持仓、复盘、情绪、设置、Hermes、基础工具、策略和资料库；前端覆盖全部工作台及 hooks/lib/配置；桌面覆盖生命周期、桥接、更新、备份、浏览器登录、运行时与打包，另检查 scripts、integrations 和 .github。
- 此轮为全仓索引扫描、重点源码审查和确定性复现；不声称每一行都经过逐行证明。未进行公网来源稳定性、真实模型工具、跨平台安装/更新验证。

复现与子报告保存在本机忽略目录 [review-2026-10-01](E:/code/project/easy-stock/.runtime/review-2026-10-01)。后端工具注入隔离、复权口径差异和设置 slice 浅复制仍需进一步验证，没有计入 13 项确认问题。

## 修复交付记录

所有 13 项均已修复。三个子智能体分别修复后端、前端、桌面/发布，主智能体修复 Web 来源权限并独立复核、整合；发现报价交易时间与抓取时间混比后追加修正，研究库内部/外部备份统一为 SQLite 一致性快照。

| ID | 修复结果 | 持久回归位置 |
| --- | --- | --- |
| D1 | 清理仅认应用标记、合法目录名和完整清单；未知目录、链接和旧备份保留 | `desktop/test/data-protection.test.cjs` |
| S1 | 在预检/鉴权/WS 升级之前限制浏览器 Origin；可执行入口拒绝非 loopback Host；默认本机前端/CLI 保持可用，Electron null 来源需 Token | `backend/internal/httpapi/auth_origin_test.go` |
| D2 | fork 默认关闭更新、拒绝原作者 OSS；自有构建源持久保存到包内，默认发布只需 GitHub | `desktop/test/update-feed.test.cjs`、`release-metadata.test.cjs` |
| B1 | 空 K 线视为主源失败并尝试备用，两源均空时明确报错 | `backend/internal/httpapi/kline_fallback_test.go` |
| B2 | 主源最多 6 秒且不超过剩余预算一半，备用在父预算内最多 10 秒；未调用/取消不误报备用故障 | `backend/internal/httpapi/kline_fallback_test.go` |
| B3 | 调用者取消/超时不写入情绪错误缓存，不延长缓存期限 | `backend/internal/httpapi/market_emotion_cancel_test.go` |
| B4 | 当日/五日分别判断有效样本，缺值不作为零分；失败窗口保留已成功值，真实零分参与融合 | `backend/internal/sector/radar_strength_failure_test.go` |
| F1 | 请求取消、代际与查询身份校验；查询变化立即隐藏旧数据并禁用旧 AI 证据 | `frontend/src/lib/latest-request.test.ts`；实际组件异步集成另见本机脚本 |
| F2 | 按日期/后端重建会话，持仓、成本、仓位和风格均参与任务匹配，失配报告不复用 | `frontend/src/lib/portfolio-expectation.test.ts`、`components/PortfolioTomorrowExpectation.test.tsx` |
| F3 | 保留报价元数据；断线、陈旧、跨日及 TTL 校验；用各来源抓取时间比较快照先后，交易时间独立校验 | `frontend/src/lib/live-quotes.test.ts` |
| F4 | 股票/题材在同样五个交易间隔按复利比较，短历史使用相同可用间隔 | `frontend/src/lib/short-term.test.ts` |
| D3 | 研究库映射 userData；旧库一致快照迁移保留原库且不覆盖目标，失败继续原库并提示；内外研究库均快照备份 | `desktop/test/research-data.test.cjs` |
| D4 | Windows 大小写与真实路径包含校验，创建根目录后再验证，拒绝 junction 别名 | `desktop/test/data-protection.test.cjs` |

最终验证（修复后的工作区）：

- `backend/`：`go test ./...`、`go vet ./...` 和服务可执行文件构建通过。
- 前端：`npm --workspace frontend test -- --run`，39 个文件、182 项通过；`npm run build:frontend`（TypeScript + Vite）通过。既有主包超过 500 kB 提示仍在。
- 桌面：`npm --workspace desktop test`，71 项中 65 通过、6 项平台相关跳过，无失败；包含 Windows 真文件锁、大小写/junction、内部活跃 WAL 与外置库备份回归。
- 改动的桌面 JavaScript 语法检查通过，发布工作流 YAML 解析通过；`git diff --check` 通过。
- 实际行情总览组件的受控异步集成脚本通过：旧行业响应即使忽略取消也不能覆盖当前个股资金，加载前和切换后 AI 立即禁用。
- 本机 Web 启动后，前端/后端与默认受信 Origin 均返回 200；不可信 Origin 请求和 PUT 预检均返回 403。仅用只读请求验证运行服务，没有修改真实设置。

限制：未执行真实 macOS/Windows 安装包构建、安装升级或云发布；公网来源和真实模型调用没有新增稳定性保证。题材强度失败时保留的旧成功窗口沿用原缓存保留机制，没有独立跨交易日过期标记，不能据此判断当前行情已恢复。原审查的三项待验证线索未作为确认问题，本次不声称已消除所有潜在缺陷。

并行协作偏好和改变的权限、研究库、更新发布边界已同步到 `AGENTS.md`、`docs/development.md`、`desktop/AUTO_UPDATE.md` 与 `backend/docs/data-sources.md`。
