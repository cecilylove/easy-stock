# easy-stock 仓库开发指南

## 项目概览

本仓库是个人非商业使用的 A 股 AI 投研工作台二开仓库。后端使用 Go 1.26，前端使用 React、TypeScript 和 Vite，桌面端使用 Electron；Node.js 开发建议使用 22.x。前端、桌面端由根目录 npm workspaces 管理，Go 模块位于 `backend/`。

## 从任务找代码

入口总览：`frontend/src/App.tsx` 切换工作台；共享导航与布局见 `frontend/src/components/WorkspaceSidebar.tsx`、`frontend/src/workspace.css`，抽屉焦点与滚动管理见 `frontend/src/lib/use-modal-dialog.ts`；`frontend/src/lib/backend.ts` 封装 API 请求；`backend/internal/httpapi/server.go` 注册路由并组装 Provider；`backend/cmd/server/main.go` 启动本机服务。以下是首批调查位置，不是完整影响清单。

- 个股详情、行情、题材或数据源：从 `frontend/src/components/StockDetailWorkspace.tsx`、`frontend/src/components/MarketOverviewWorkspace.tsx` 或 `frontend/src/App.tsx` 的请求出发 → `backend/internal/httpapi/server.go` 的路由与 `market_overview.go` 等缓存/处理器 → `backend/internal/providers/`、`backend/internal/sector/` 及 `backend/internal/foundation/types.go` 的来源元数据。专业图表/盘口见 `frontend/src/components/ProfessionalKLineChart.tsx`、`StockQuoteSidebar.tsx` 和 `frontend/src/stock-terminal.css`；指标、刷新合并/坐标保持见 `frontend/src/lib/technical-indicators.ts`、`chart-updates.ts`、`use-stable-chart-scale.ts`。价格能力路由/严格复权见 `backend/internal/httpapi/kline_routing.go`、`kline_adjustment.go`，腾讯股票适配见 `backend/internal/providers/tencent/stock_kline.go`；年 K 聚合见 `kline_year.go`，腾讯独立指数路由见 `backend/internal/providers/marketoverview/index.go`，行业身份/成员完整性见 `backend/internal/sector/radar_identity.go`、`radar.go`；参考与边界见 `docs/stock-detail-terminal.md`。先找相应 Provider 测试、`backend/internal/httpapi/market_overview_test.go`、`kline_year_test.go`、`frontend/src/components/StockDetailWorkspace.test.tsx`、`frontend/src/lib/stock-detail.test.ts` 和 `frontend/src/lib/market-overview.test.ts`；来源、回退规则见 `backend/docs/data-sources.md`。
- 个股研究：从 `frontend/src/components/StockAIAnalysisWorkspace.tsx`、`frontend/src/lib/use-stock-research.ts` → `backend/internal/httpapi/stock_research.go` → `backend/internal/stockanalysis/research_service.go`、`research_store.go`。先查 `backend/internal/httpapi/stock_research_test.go`、`backend/internal/stockanalysis/research_test.go` 和 `frontend/src/components/StockResearchReport.test.tsx`；证据与任务约束见 `docs/stock-research.md`。
- 数据源状态：从设置内 `SettingsDrawer.tsx`、`SourceHealthPanel.tsx`、`SourceIntegrationCatalog.tsx`、`lib/source-health.ts` → `backend/internal/httpapi/source_health.go`、`source_probe.go`。清单、能力和作用提示共用 `lib/source-integrations.ts`，业务页保留实际来源与直达设置入口。`GET /sources` 只读已有记录，设置“刷新检测”通过 `POST /sources/check` 请求七家已实现来源的代表接口；手动检测与业务观测独立，不代表全部功能健康；新价格/指数/行业/资金尝试有能力级记录，缓存和未尝试能力不续期、不伪报成功。普通/渐进题材、热榜和期指分别经 `theme_progress.go`、`stock_hot.go`、`market_overview.go` 记录实际请求；缓存读取不续期。东方财富公共接口已参与默认取数与回退；预留凭据仅兼容存储，填写不启用额外能力。先查 `source_probe_test.go`、`source_health_test.go`、`stock_hot_test.go`、`theme_observation_test.go` 和 `sector/radar_observation_test.go`，取数、缓存及能力边界见 `backend/docs/data-sources.md`。
- 桌面启动或本地数据：从 `desktop/main.cjs` 的启动/运行环境 → `desktop/backend-process.cjs` 的进程、`desktop/preload.cjs` 的桥接与 `backend/cmd/server/main.go` 的监听/数据路径。研究库隔离/迁移见 `desktop/research-data.cjs`，更新备份见 `desktop/data-protection.cjs`；先查对应桌面测试，运行 `npm --workspace desktop test` 和受影响的后端测试。打包和运行时依赖见 `docs/development.md`，fork 默认关闭自动更新，自有源及备份约束见 `desktop/AUTO_UPDATE.md`。

其他页面可由 `frontend/src/App.tsx` 的组件入口和 `backend/internal/httpapi/server.go` 的路由注册定位；复杂实现按需查源码和专项文档，不复制完整调用图。

## 开发与验证命令

以下命令已在 `package.json`、工作区包清单、`backend/go.mod`、`docs/development.md` 和发布 CI 中核对来源；列出命令不代表本次执行过。

- 根目录安装依赖：`npm ci`。
- Web 分别开发：`npm run dev:backend` 与 `npm run dev:frontend`；默认只监听本机 `127.0.0.1` 的 `20081` 与 `20073` 端口。`dev:backend` 使用 POSIX 环境变量写法，在 Windows 原生终端应按 `backend/cmd/server/main.go` 设置环境变量后从 `backend/` 运行 `go run ./cmd/server`；不要假设脚本可直接跨平台执行。
- Windows 原生 PowerShell：分别运行 `./scripts/dev-windows.ps1 -Service Backend`、`-Service Frontend`；桌面使用 `-Service Desktop`。脚本优先使用 `.runtime/tools/go/`，隔离 Web/桌面数据；当前终端加载环境使用 `. ./scripts/dev-windows.ps1`。准备条件见 `docs/development.md`。
- 后端测试：在 `backend/` 执行 `go test ./...`。
- 前端测试与构建：`npm --workspace frontend test -- --run`、`npm run build:frontend`。Windows 布局回归可在已有服务上运行 `./scripts/verify-workspace-layout.ps1`，前提和范围见 `docs/development.md`。
- 桌面主进程测试：`npm --workspace desktop test`；根目录 `npm test` 包含后端和前端测试，不包含桌面测试。
- `npm run restart` 会运行 `scripts/rebuild-restart.sh`，依赖 Bash、`lsof` 等工具，并会停止默认端口上的现有进程；使用前确认平台和端口归属，不要将它当成无副作用的验证命令。
- 桌面开发与发布依赖额外的 Hermes/Python 运行时；具体准备、平台和打包命令见 `docs/development.md` 与 `desktop/package.json`。

## 自测原则

- 按修改范围执行对应测试；跨前后端改动同时检查后端、前端与构建，涉及桌面启动或桥接时再运行桌面测试。
- 公网数据源实时测试是显式启用的额外检查，不属于默认单元测试；失败时区分上游可用性与代码回归。
- 只报告实际运行的命令与结果；未运行的构建、测试和平台验证不得写成通过。

## 项目特殊说明

- 本仓库当前开发目标是用户自己的 fork，远程 `origin` 指向 `cecilylove/easy-stock`，当前本地 `main` 跟踪 `origin/main`。开始改动和提交前核对当前工作树、分支、远程及未提交改动；不得依据旧文档中的 `upstream`、`oss-main` 或其他机器的工作树布局切换目标。仅在用户明确要求时推送；本地提交不等于允许推送远程 `main`。
- 项目采用 `LICENSE` 中的 PolyForm Noncommercial License 1.0.0；自用二开限非商业用途。分发修改版时保留许可证和要求的版权声明，商业使用需另外取得授权。
- 桌面端由 Electron 启动仅监听本机的 Go 后端，并用随机 Token 保护；Web 未配置 `A_STOCK_TOKEN` 时允许本机 CLI 和受信前端访问。后端启动入口强制校验 loopback Host，浏览器来源默认仅允许本机 `20073` 前端与后端自身；额外本机前端来源通过 `A_STOCK_ALLOWED_ORIGINS` 配置。Electron 的 `null` 来源仍须 Token；不得放宽成任意 Origin 或把无鉴权服务暴露到公网。
- 用户已明确授权本项目工作区内适合并行的任务默认使用智能体团队（Agent Teams），跨会话无需重复请求授权；由主智能体自行决定是否拆分及团队规模，按独立文件/模块分工，在共享任务中记录写入范围与顺序依赖，统一审查最终差异并验证，等待必要成员完成后交付。小任务和有顺序依赖的步骤串行处理；用户在当前会话明确要求不用团队时，以当前要求为准。
- 数据源目标是逐步替换东方财富全部能力；已退役的股票K/明确复权/指数快照与历史不重新纳入默认或备用链路。其它尚无可靠替代能力分阶段迁移，不能为全替换删掉功能或伪造数据；边界见 `backend/docs/data-sources.md`、`docs/source-replacement-rollout.md`。
- 本机设置、SQLite、日志、Hermes Home、模型密钥和浏览器登录态不入库；使用独立数据路径验证二开，避免覆盖已安装版用户数据。参见 `.gitignore` 与 `docs/development.md`。

## 文档自维护

每次交付前，检查本次改动是否改变运行入口、模块职责、关键跨模块边界、上述任务路由、验证命令或长期约束；若改变，在同次改动中核对源码并更新受影响条目。普通业务逻辑变化不更新本文件，也不把一次性排障记录写入。

维护时优先替换、合并或删除失效内容，不默认追加；根文件保持简短，复杂细节指向现有专项文档，不复制完整调用图。无法确认的路径与命令先查证，且在本文件与其他仓库文档产生相反指令时标出待同步文件并尽快同步，不能长期依赖警告。
