# easy-stock 项目准备与原仓库差异核对

核对日期：2026-09-30，Asia/Shanghai。以下差异来自当日实际拉取的 Git 对象及代码检查；这是固定快照，不代表此后的最新状态。

## 结论

- 原仓库：[jundizhou/easy-stock](https://github.com/jundizhou/easy-stock)。开发目标仍为你的 [cecilylove/easy-stock](https://github.com/cecilylove/easy-stock)；本机 `main` 跟踪 `origin/main`。
- 本地与 fork 远程当时同为 `b698f9b832ed41eaecec04834a3d678b1b641b79`，开始工作时无未提交修改。
- 原仓库 `main` 最新为 `df5d57fda4a1b9815026aa2fbd0cc7c0195250bb`，提交时间 2026-09-30 20:43:26 +08:00，内容为发布磁盘镜像大小及 Windows MCP 参数修复。
- 最新 tag `v1.3.0` 解引用后也指向 `df5d57f`，与原仓库最新 `main` **没有代码差异**。它是附注 tag，tag 对象 SHA `919aa36` 与实际代码提交 SHA 不同，不应据此判断代码不同。
- `v1.2.2 → v1.3.0` 共 13 个提交，116 个文件变化，增加 6462 行、删除 1639 行（启用 Git 重命名识别）。你的 fork 已包含英文 README 与 DeepSeek 默认模型调整两个提交，因而相对当前 fork 只有 11 个上游独有提交。
- 你的 fork 有 4 个独有提交；与上游最新的完整树差异为 161 个文件、增加 6322 行、删除 4623 行。此统计方向为 **fork → 上游**，包含上游尚未拥有的本地功能，不能理解为上游主动删除了你的功能。
- 合并预演报告一处文本冲突：`frontend/src/App.tsx`。自动合并成功不代表功能已经验证；双引擎改变了跨模块调用边界，后续需要完整回归。

初次比较阶段只获取比较引用，未合并上游、未切换分支、未提交或推送。后续已按用户授权选择性移植 UI 改进并归档，见 [实际纳入结果](upstream-adoption-assessment-2026-09-30.md#实际纳入结果2026-09-30)；本文 Git 数字仍对应比较时的固定快照。上游分支记录于 `refs/remotes/original/main`，tags 独立存于 `refs/original-tags/*`，没有覆盖 fork 的 tags。

## 项目结构与二开入口

| 层 | 职责和关键入口 |
| --- | --- |
| 前端 | React 19、TypeScript、Vite；`frontend/src/App.tsx` 管理工作台导航，`frontend/src/lib/backend.ts` 统一 API 和桌面桥接。 |
| 后端 | Go 1.26；`backend/cmd/server/main.go` 组装持久化路径、Runtime、调度器和服务，`backend/internal/httpapi/server.go` 注册 HTTP/WebSocket 路由。 |
| 数据基座 | `foundation` 定义报价、K 线及来源元数据；`providers` 适配第三方；`sector` 聚合题材，HTTP 层处理缓存、降级与渐进加载。 |
| AI 研究 | 当前 fork 使用 `internal/hermes`；个股研究由 `stockanalysis/research_service.go` 执行受限多阶段任务，并保存原始证据快照；持仓与复盘复用模型能力。 |
| 桌面 | `desktop/main.cjs` 启动随机本机端口及 Token 保护的后端，管理浏览器登录与微信 sidecar；`preload.cjs` 提供前端桥接。 |
| 数据持久化 | SQLite 存储复盘、个股研究、持仓巡检、情绪及题材快照；设置、Hermes Home、模型密钥和浏览器登录态属于本机运行数据。 |

目前主要页面：大 V 复盘、个股详情、个股分析、持仓 AI 巡检、短线连板、趋势题材、行情总览、游资心法、AI 对话和 Token 统计。行情、K 线与 AI 研究是不同调用链；普通行情查看无需模型密钥。

具体任务入口见 [AGENTS.md](../AGENTS.md)，数据源与回退见 [data-sources.md](../backend/docs/data-sources.md)，研究预算与证据边界见 [stock-research.md](stock-research.md)。

## v1.2.2 到原仓库最新的代码差异

| 范围 | 实际变化 | 二开影响与主要证据 |
| --- | --- | --- |
| 双引擎架构 | Hermes 专属公共执行层迁移至 `backend/internal/agent`，新增 Codex App Server 适配器、统一 Service、协议事件及配置协调保存。 | 后续新 AI 业务应依赖公共 Gateway/Prompter，而非直接绑定 Hermes；见 `agent/service.go`、`agent/codex.go`、`agent/config_transaction.go`。 |
| 任务与会话 | 后台多阶段任务启动时冻结运行引擎、模型、凭据及工具/Skill 配置；切换设置不自动取消旧任务。聊天新增 `agent_session_id` / `agent_model_key`，兼容旧 Hermes 历史。 | 合并时检查个股研究、持仓子任务、复盘任务的 context 传递及历史恢复；见 `agent.BindTask`、`stockanalysis/research.go`、`frontend/src/lib/chat.ts`。 |
| 模型协议 | Codex 使用原生 Responses；保存引擎/模型前检测连接能力，明确不支持时拒绝启用，认证/限流/网络失败与不支持分开处理。新增 Responses 模型目录解析及按模型声明的思考档位。 | 不能认为任意 Chat Completions 服务可直接用于 Codex；没有模型协议转换或静默引擎回退。见 `httpapi/settings.go`、`llm_models.go`、`agent/protocol.go`。 |
| 共享设置与密钥 | 引擎、模型档案、Skills/MCP 共用配置；继续沿用应用自有 Hermes Home。密钥默认圆点显示，点击查看按 profile 读取且禁止缓存；保存失败回滚运行配置。 | 需要保留配置迁移、失败回滚及密钥读取边界；见 `appsettings/store.go`、`httpapi/llm_api_key.go`、`SettingsDrawer.tsx`。 |
| 用量与结果 | 个股研究报告新增 `runtime`；Token 记录加入运行引擎归属，调用时与任务配置同步。 | 新增结果字段为可选；历史报告不能因切换引擎被重跑或标成当前引擎。见 `research_types.go`、`httpapi/token_usage.go`。 |
| 对话界面 | 改善聊天列表/正文独立滚动，移除 AI 聊天顶部栏，按新设置复核发送配置，保留模型与思考控制。 | 检查滚动、取消、审批/澄清、切换模型及从研究报告追问的交互；见 `AIChatWorkspace.tsx`。 |
| 工作台与报告 | 其他工作台顶部栏默认展开并可收起；个股、持仓、研究报告正文和布局调整。设置保存成功关闭设置页并显示 3 秒通知。 | 与本地新增个股详情导航和页面状态在 `App.tsx` 相交；CSS 自动合并后仍需检查窄屏、表格及报告。 |
| 桌面 Runtime | 打包准备增加 Codex 0.159.0 原生二进制锁、平台归档校验、版本与 App Server 检查、许可证与 NOTICE；开发启动准备两个 Runtime。 | 当前 fork 的开发脚本只需 Hermes；引入上游后应同步 Windows 启动脚本，补上 `ensure-codex-runtime.mjs`，不可只复制前端引擎按钮。 |
| 发布可靠性 | GitHub 下载与 OSS 更新分开发布；新 GitHub Release 先保持草稿，核对资产大小和 SHA-256 再公开；OSS 增加分片、超时、重试和公开文件/清单核验。 | 后续 fork 发布须配置自己的发布凭据和更新目标；本次未运行发布或打包流程。见 `.github/workflows/release.yml` 和发布脚本。 |
| 最新修复 | DMG 大小按应用实际文件体积、目录条目和余量计算；Windows stdio MCP 改用 `subprocess.run` 保留含空格/多行参数。 | 这两个修复包含在最终 `v1.3.0` tag 中，不能只取较早的版本号提交 `9c8aaed`。见 `desktop/scripts/dmg-size.mjs`、`agent/mcp_launcher.py`。 |
| 文档/默认模型 | 英文 README、双语入口；DeepSeek 默认模型调整。 | 这两个提交已在你的 fork 中，后续正常合并会共享历史，不必再次 cherry-pick。 |

以上重点直接核对了源码差异；版本定位同时参考 [v1.3.0 发布说明源文件](https://github.com/jundizhou/easy-stock/blob/df5d57fda4a1b9815026aa2fbd0cc7c0195250bb/.github/release-notes/v1.3.0.md) 和 [双 Runtime 设计](https://github.com/jundizhou/easy-stock/blob/df5d57fda4a1b9815026aa2fbd0cc7c0195250bb/docs/dual-runtime-design.md)。设计文档内旧 `oss-main` 工作树描述不是本机分支操作依据。报告未将上游描述的测试通过当作本机已执行的验证。

## 你的 fork 独有的改动

共同祖先为 `bd47fc3db1d366c69612b4ed36efe68c2a133268`。

| 提交 | 内容 | 合并必须保留的行为 |
| --- | --- | --- |
| `d15c289` | fork 专属 AGENTS 开发路由 | 当前 origin/main 目标、只在明确授权时推送、本机数据隔离及实际验证要求。 |
| `56f865f` | Windows 默认测试兼容 | 路径断言与平台条件；上游 Hermes → agent 的重命名需同步保留对应测试意图。 |
| `49ff3f9` | 被动数据源观测状态 | `/api/v1/sources` 不主动探测、10 分钟过期、实际请求成功/失败记录、明确未接入、缓存不制造新成功。涉及 `source_health.go`、行情/题材刷新与前端来源提示。 |
| `b698f9b` | 个股详情与竞价分时 | 独立个股详情页面；搜索、报价、多周期 K 线；09:15–09:25 竞价参考轨迹、历史/缺失状态、同股同日缓存、盘中刷新及失败退避。涉及 `auction.go`、`stock_detail_poll.go`、`StockDetailWorkspace.tsx` 与导航。 |

上游目前没有这套独立个股详情及被动来源观测实现。尤其竞价来源是参考轨迹，不能合并后变成虚构成交价，或拿上一交易日点/09:30 开盘价补当日竞价。

## 上游独有的 11 个提交

按较早到较新排列：

1. `14e82da`：放大正文，保留列表与标题尺寸。
2. `f8b5e1f`：改善个股及持仓报告可读性。
3. `7b58c1a`：Codex Runtime、共享 Agent 设置与思考控制。
4. `a07af29`：AI 聊天顶部栏默认收起（后续提交再次调整）。
5. `38038e3`：简化工作台顶部栏，并默认展开。
6. `874ac8d`：改善聊天滚动、简化工作区布局。
7. `e8f6e41`：移除 AI 聊天顶部栏。
8. `e25dda7`：合并双 Runtime 与聊天工作区实现。
9. `b3d7ea5`：设置保存后关闭并显示成功通知。
10. `9c8aaed`：v1.3.0 版本与发布可靠性。
11. `df5d57f`：DMG 容量与 Windows MCP 参数修复。

此列表包含合并提交；不要把 11 个提交理解为 11 套独立最终功能。顶部栏行为以最终树为准。

## 后续同步建议

是否需要同步及各提交优先级，见 [上游纳入必要性评估](upstream-adoption-assessment-2026-09-30.md)。当前建议先吸收设置反馈、报告排版和聊天滚动，双引擎按 AI 二开路线决定，发布流程暂缓。一旦决定采用双引擎，建议以当前 fork 为基础，在独立开发分支正常合并固定的 `df5d57f` / `v1.3.0`，保留双方历史。整体覆盖上游代码会丢失本地的个股详情与来源观测；仅选择几个 UI 提交则无法获得完整双引擎。

合并预演使用 `git merge-tree --write-tree HEAD refs/remotes/original/main`，退出码 1 表示存在内容冲突。它只写 Git 对象，不改变当前索引和工作文件。预演有 1 个文件、2 个冲突块，均位于 `frontend/src/App.tsx`：页面副状态/标题/说明和底部来源说明，本地 `stock-detail` 分支与上游 Agent/顶部栏文案相交；当前工作目录的 Git 状态并未进入合并。

后续需重点检查：

- `App.tsx` 同时保留 `stock-detail` 导航、来源状态以及上游顶部栏/通知逻辑。
- HTTP 路由、来源观测与单股轮询缓存仍按原行为运行。
- Hermes/agent 重命名后的 Windows 测试断言。
- Windows 开发入口准备两个 Runtime，隔离应用自有 Codex Home，不能读取用户全局 Codex 配置。
- 个股研究、持仓巡检、复盘、聊天使用统一 Agent Service，配置切换期间保留任务快照和旧结果。
- 前后端、桌面测试、构建及双 Runtime 原生集成验证；真实供应商连接需用实际模型配置测试。

## 本机环境与实际验证

| 项目 | 2026-09-30 实际结果 |
| --- | --- |
| Node/npm | 使用已有 Node 22.20.0、npm 10.9.3。 |
| Go | 安装官方 Windows amd64 Go 1.26.8 到 `.runtime/tools/go/`，校验下载 SHA-256；项目脚本优先加载它。 |
| Go 依赖 | 默认 `proxy.golang.org` 连接超时，设置用户 Go 环境 `GOPROXY=https://goproxy.cn,direct` 后下载成功；未修改 `go.mod`/`go.sum`。 |
| npm | `npm ci --foreground-scripts` 成功安装 554 个包；Electron 下载使用进程内 `ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/`，未升级锁文件。首次默认下载中断后完整重试成功。 |
| Hermes/Python | 项目脚本安装并校验 Hermes 0.21.3，CPython 3.11.15；uv 的 Python/缓存目录隔离至 `.runtime/tools/`，解决原有目录链接错误。 |
| 微信 sidecar | 固定源码 `043c2f9828401220a00b7b125686b334581745e0` 及运行依赖准备成功。GitHub tarball 直连失败后通过 Git 获取相同提交，不改版本。未进行微信账号登录。 |
| Go 测试 | `backend/` 下 `go test ./...` 全量通过；先前依赖下载失败属于网络问题，镜像修复后重跑通过。 |
| 前端测试 | `npm --workspace frontend test -- --run`：32 个测试文件、163 项测试通过。依赖首次安装失败时曾因工具缺失无法运行，安装完成后重新执行通过。 |
| 前端构建 | `npm run build:frontend` 通过；存在 Vite 大 chunk 提示，当前未为环境准备任务改动业务打包策略。 |
| 桌面测试 | `npm --workspace desktop test`：58 项，52 通过、6 项按 Windows/POSIX 条件跳过、0 失败。 |
| Web 启动 | 前端 `127.0.0.1:20073`，后端 `127.0.0.1:20081`，健康检查 `ok=true`；实际页面搜索 `600519` 成功显示新浪报价与日 K。 |
| Electron 启动 | Electron 38.8.6 窗口已创建，前端 renderer 启动，内置后端 `127.0.0.1:20000`、微信服务 `127.0.0.1:30000` 就绪；端口由当时空闲情况决定。 |
| 隔离数据 | Web 位于 `.runtime/web-data/`，桌面位于 `.runtime/desktop-data/`。模型状态为 Runtime 可用、密钥未配置；未执行付费 AI 请求。 |
| 修改检查 | `git diff --check` 通过。本次添加 Windows 开发入口与报告，同步更新开发文档和 AGENTS；未修改业务实现。 |

PowerShell 使用方法见 [开发文档](development.md) 和 [dev-windows.ps1](../scripts/dev-windows.ps1)。桌面启动由 Node 的 Electron CLI 保持子进程生命周期，避免直接调用 GUI exe 后终端提前结束而导致日志 EPIPE。

未运行正式安装器打包、发布、race 测试或全部公网数据源实时测试。页面获取真实行情只验证了当时该调用链，不能代表所有数据源均健康。

## 所有 tags 与最新代码的规模对比

共 19 个 tags，按版本降序排列。日期取对应代码提交的上海时间；差异方向为该 tag → `df5d57f`。提交数包含合并提交；增删行数识别重命名，但不表示语义改动大小。历史版本重点用于快速定位，不能当成最新版本仍沿用全部旧行为的保证，例如旧价格方案在后续 AI 主导研究中已经调整。

| Tag | 对应代码提交 | 提交日期（上海） | 到最新缺少的提交数 | 与最新代码规模差异 | 该版本重点 |
| --- | --- | --- | ---: | --- | --- |
| [v1.3.0](https://github.com/jundizhou/easy-stock/tree/v1.3.0) | `df5d57f` | 2026-09-30 | 0 | 0 文件，代码相同 | Codex/Hermes 双引擎、共享设置与思考控制、报告/对话排版、发布校验 |
| [v1.2.2](https://github.com/jundizhou/easy-stock/tree/v1.2.2) | `0470c34` | 2026-09-21 | 13 | 116 files changed, 6462 insertions(+), 1639 deletions(-) | 连板题材增强、非交易日处理、概念与 AI 明细默认折叠、期货共识表 |
| [v1.2.1](https://github.com/jundizhou/easy-stock/tree/v1.2.1) | `8d09a68` | 2026-09-19 | 17 | 127 files changed, 7478 insertions(+), 1679 deletions(-) | 题材/连板渐进加载、Token 归属与重复调用修复、思考能力、Windows 更新锁 |
| [v1.2.0](https://github.com/jundizhou/easy-stock/tree/v1.2.0) | `9122106` | 2026-09-16 | 23 | 169 files changed, 12604 insertions(+), 2335 deletions(-) | Hermes 0.21.3、思考过程展示、拐点分类和回测、主题切换、按模型统计 Token |
| [v1.1.0](https://github.com/jundizhou/easy-stock/tree/v1.1.0) | `31bafe6` | 2026-09-10 | 31 | 193 files changed, 16212 insertions(+), 4094 deletions(-) | AI 主导个股证据研究、评分 V3、Token 统计、证据引用与超时处理 |
| [v1.0.0](https://github.com/jundizhou/easy-stock/tree/v1.0.0) | `515de04` | 2026-09-06 | 48 | 230 files changed, 24967 insertions(+), 4445 deletions(-) | 首个 1.x：市场/复盘/持仓研究工作流、Hermes 0.19.0、跨平台桌面发行 |
| [v0.9.2](https://github.com/jundizhou/easy-stock/tree/v0.9.2) | `b969d05` | 2026-08-27 | 62 | 278 files changed, 30528 insertions(+), 4598 deletions(-) | 持仓次日预期、行业映射、东财/腾讯成分合并及腾讯分页 |
| [v0.9.1](https://github.com/jundizhou/easy-stock/tree/v0.9.1) | `986f0b9` | 2026-08-25 | 68 | 297 files changed, 32676 insertions(+), 4475 deletions(-) | 持仓 AI 巡检、后台逐股分析、组合风险、个股/组合评分 V2 |
| [v0.9.0](https://github.com/jundizhou/easy-stock/tree/v0.9.0) | `a746650` | 2026-08-25 | 76 | 306 files changed, 35023 insertions(+), 4129 deletions(-) | 行业趋势与开盘啦融合雷达、题材去重、滚动日志与请求关联 |
| [v0.8.0](https://github.com/jundizhou/easy-stock/tree/v0.8.0) | `485cfb0` | 2026-08-25 | 81 | 322 files changed, 37335 insertions(+), 4475 deletions(-) | 新股分析流程、人气股榜单、热点涨幅校验、复盘长图作者脱敏 |
| [v0.7.0](https://github.com/jundizhou/easy-stock/tree/v0.7.0) | `c8c19f2` | 2026-08-17 | 86 | 341 files changed, 39673 insertions(+), 4438 deletions(-) | 现价与日 K 证据、价格计划一致性、两融余额图、非商业标题 |
| [v0.6.1](https://github.com/jundizhou/easy-stock/tree/v0.6.1) | `dca8f24` | 2026-08-17 | 92 | 350 files changed, 40755 insertions(+), 4436 deletions(-) | 短线量化锚点、题材归因纠错、PolyForm 非商业许可 |
| [v0.6.0](https://github.com/jundizhou/easy-stock/tree/v0.6.0) | `9821db5` | 2026-08-17 | 94 | 354 files changed, 41739 insertions(+), 4700 deletions(-) | 价格决策区间、个股/题材新闻、导航与移动端调整 |
| [v0.5.1](https://github.com/jundizhou/easy-stock/tree/v0.5.1) | `f80c7e1` | 2026-08-14 | 98 | 358 files changed, 44759 insertions(+), 4599 deletions(-) | 事件驱动题材归因、公告正文、题材共振、复盘文章管理 |
| [v0.5.0](https://github.com/jundizhou/easy-stock/tree/v0.5.0) | `581c25a` | 2026-08-13 | 102 | 367 files changed, 46179 insertions(+), 4559 deletions(-) | 多周期 K 线、龙虎榜席位、复盘时间窗、多模型与长图导出 |
| [v0.4.0](https://github.com/jundizhou/easy-stock/tree/v0.4.0) | `f14f524` | 2026-08-13 | 118 | 370 files changed, 49238 insertions(+), 4296 deletions(-) | 桌面更新、Skill/MCP、行情列排序、龙虎榜 AI 证据 |
| [v0.3.0](https://github.com/jundizhou/easy-stock/tree/v0.3.0) | `5331b4d` | 2026-08-12 | 128 | 380 files changed, 56159 insertions(+), 5162 deletions(-) | 行情总览、跨市场/资金/研报、Markdown、Runtime 打包校验 |
| [v0.2.0](https://github.com/jundizhou/easy-stock/tree/v0.2.0) | `8ff1326` | 2026-08-11 | 133 | 387 files changed, 62508 insertions(+), 5377 deletions(-) | 官方复盘自动同步、多作者总结、供应商预设与模型发现 |
| [v0.1.0](https://github.com/jundizhou/easy-stock/tree/v0.1.0) | `4f7bade` | 2026-08-09 | 136 | 390 files changed, 64126 insertions(+), 5580 deletions(-) | 首个桌面版：个股 AI、题材/连板、复盘、个人方法论与 Hermes |

完整逐文件状态、增删行数、相邻版本变化、各版本提交列表和合并预演输出见 [机器可核对的差异清单](verification/upstream-comparison-2026-09-30.json)。

复核命令（仓库根目录）：

```powershell
git rev-parse HEAD origin/main refs/remotes/original/main 'refs/original-tags/v1.3.0^{}'
git rev-list --left-right --count HEAD...refs/remotes/original/main
git diff --stat refs/original-tags/v1.3.0 refs/remotes/original/main
git diff --stat refs/original-tags/v1.2.2 refs/original-tags/v1.3.0
git diff --name-status -M HEAD refs/remotes/original/main
```

这里的引用是本次获取的本地快照；今后重查最新上游时应重新 fetch 并记录新的 SHA。`origin` 仍属于你的 fork，没有新增或修改远程推送目标。
