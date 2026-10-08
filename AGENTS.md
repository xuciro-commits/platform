# AGENTS.md

AI 编程助手从这里开始；`CLAUDE.md` 只引用本文件。规则以本文件为准，其他文档不再复述。

## 项目

面向 FDE 与客户构建者的 AI 业务应用平台，产品形态对标 Palantir Foundry/AIP：语言中立的内核契约（`contract/`）、Go 宿主（`capabilities/server/`）、Web 工作区（`web/`）、验证应用（`apps/`、`solutions/`）。

读什么：[Intent](docs/Intent.md)（宗旨）→ [WorkQueue](docs/WorkQueue.md)（当前要做的事）→ 与任务相关的 [Platform](docs/Platform.md) 章节、[ADR](docs/ADR/) 与 [Apps](docs/Apps.md)。产品结构（Shell、应用门户、功能块）以 [ADR-0052（Shell/门户/投影）与 ADR-0053（构建者编辑器）](docs/ADR/0052-foundry-aligned-platform-experience.md) 为唯一现行版本。不要求通读全部 ADR。

## 工作方式

- 负责人的本次指令决定范围；没有新指令时取 WorkQueue 的第一项。先看 `git status`，保留他人改动。
- 一次做完一个可交付的整块，再做下一块；不自动追加邻接功能。做完说明：什么可用了、还缺什么、下一步。
- 编码期间只做必要的通过性检查；一个整块完成后跑一次适用检查（见下）。未运行的检查、未观察的 UI 不得报为通过。
- 外部产品的做法要对照本仓库证据判断后再采纳。

## 硬规则

1. **内核是契约。** `contract/` 不含领域词汇；Protobuf 不定义业务含义（ADR-0002）。内核变更先改规范与向量，再改 Go 及 Rust/TypeScript 边缘；`v1` 变更须 ADR。
2. **生成文件不手改**：`contract/go/gen`、`web/packages/kernel/src/gen/host.ts`（宿主 API 类型变化后在 `capabilities/server` 运行 `go run ./cmd/api-types`）、`web/apps/catalog/src/gen`（`node scripts/catalog.mjs generate`）。
3. **单一归属，先复用。** 应用只导入 `platformserver/platform`，跨应用走 `protocols/`；客户端只用 `@platform/ui` 与 `@platform/app`；缺的能力在归属方扩展，不在应用里自造表格/面板/审批/文件/外部调用。`scripts/boundaries.sh` 与 `scripts/escapes.sh` 守这条线，已知逃逸只减不增。UI 变更前 `node scripts/catalog.mjs search <关键词>` 查已有组件。
4. **应用不为自己改内核。** 参考应用用来证明平台能力，行业深度只取当前任务必需的最小变更；摩擦记入 WorkQueue。
5. **租户扩展只走受控路径**：受控定义、隔离编译/执行与版本化制品（ADR-0044），不在宿主直接执行租户脚本。
6. **最简代码。** 不保留永久重复路径或临时垫片；替代旧路径时删除旧路径及其文档。
7. **双语文案。** 产品界面文字英文原文 + 简体中文：应用 `i18n/zh-CN.json`，Web `t()` + 包内 `i18n.ts`。业务数据不翻译。
8. **文档各司其职，只写现状。** Intent 管宗旨；Platform 管架构与能力摘要；ADR 管决策与"As built"边界（合并更新，不追加日志）；WorkQueue 管顺序（完成即移除）；Testing 管检查方法；Apps 管可执行用法。不新增总结文档、第二队列或检查点日记。被取代的 ADR 在状态行写明被谁取代。
9. **提交信息**单句祈使句，注明 ADR/工作号。提交、推送、部署遵循已有授权。

## 代码导航

| 位置 | 内容 |
|---|---|
| `contract/` | K1–K9 规范、向量、生成类型、Go 参考实现；`lean/` 有界证明 |
| `capabilities/server/` | 宿主运行时（组件地图见 `doc.go`：Tenant = 身份 + 提交管线 + 各自持锁的组件；路由在 `routes_*.go`）；`platform/` 是应用唯一可导入的应用 API；`journal/` 持久化、`idp/` 身份提供方、`internal/host` 宿主内接口；`apps/` 平台应用（build/flow/ai/work/enterprise/knowledge/files/relations/core） |
| `apps/<id>/server/` | 自治业务应用；`protocols/` 跨应用协议；`solutions/` 组合宿主 |
| `web/packages/ui`、`web/packages/app` | 共享 UI Kit 与前端应用 API |
| `web/packages/build` | 构建者应用：Ontology / Workshop / Automate / AI Functions / Code / Projects / Releases |
| `web/packages/platform` | 治理与运维应用：Control Panel / Runs / Data Connection / Agents / AI / Knowledge / Host Console |
| `web/apps/workspace` | 唯一工作区：`session/`（身份）、`host/`（读取与决策）、`shell/`（Rail、Home、门户、Explorer、Lineage）、`chrome.tsx`（共享视图） |
| `web/apps/catalog`、`web/e2e` | Asset Library（资产库 + 代码沙箱）；浏览器路线 |

## 检查

唯一入口是根目录 `Makefile`（`make help`，ADR-0081），三层：**dev**（`make infra` / `make dev SOLUTION=` / `make web` / `make dev-light`）、**check**（提交前 `make check`；`make test-go PKG=./platform RUN=TestX` 跑改到的 owner 测试；`make e2e SPEC=page-notice` 跑一条浏览器路线）、**release**（`make verify` 完整证据，`make build`/`make images`/`make rehearse`/`make tag`）。按影响面选：Go 宿主 → `make check` + 对应 `make test-go`；应用/协议 → `scripts/verify.sh composition`；Web → `make check-web`（整块交付或发布节点再选一条 `make e2e SPEC=`）；纯文档 → 链接与 `git diff --check`。外观与手感靠截图和负责人走查，不写像素断言。CI 只在 `v*` 标签上跑，不要依赖它发现问题。

测试规范（docs/Testing.md「测试规范」）：浏览器 spec 用 `web/e2e/tests/kit.ts`，宿主测试用 `capabilities/server/testkit_test.go`；不手写登录/对象/页面/发布/`pb.Submission`，不提交压缩成一行的 spec。

## 沙箱（Arena 等离线代理环境）里怎么干活

网络只通 GitHub、npm、PyPI；Go 模块代理全封。仓库自带离线所需，照做即可：

1. **Go**：`cd /home/user/platform && bash scripts/sandbox-go.sh`，然后 `export PATH=/home/user/.go-toolchain/go/bin:$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache`（脚本会打印这一行）。工具链来自 `tools/go/`（找不到则 `pip download go-bin`），依赖来自 `capabilities/server/vendor/`（已提交，别跑 `go mod tidy`/`go mod vendor`）。`go build ./...`、`go vet`、`go test` 都在 `capabilities/server/` 目录下跑；`apps/*/server`、`solutions/*` 是独立模块、依赖未 vendor，只能 `gofmt`，编译交给本地。
2. **Web**：`cd web && corepack pnpm install --frozen-lockfile`（npm 可达，约 5 秒），检查按包跑 `corepack pnpm exec tsc -p web/packages/<pkg>`；`pnpm -r typecheck` 会 OOM（2 核）。无浏览器：Playwright 不可用，UI 只能 tsc + 代码走查，报告时写明"未在浏览器观察"。
3. **宿主类型变了**：`go run ./cmd/api-types`（在 `capabilities/server/`）再生成 `web/packages/kernel/src/gen/host.ts`，否则 `TestAPIContract` 失败。文案变了：补 `i18n/zh-CN.json` / 包内 `i18n.ts`，否则 `TestLanguages` 失败。
4. **轻量验证**：`make check-go` 不可用时（无 pnpm/buf）直接 `go build ./... && go vet ./...`；`go test ./apps/<改动包>/... ./platform/...` + 根包按名跑 `go test . -run 'TestLanguages|TestAPIContract'`；全量根包测试约 40 秒，`TestRecordsAtScale` 计时抖动可忽略。
5. **沙箱会被重置**：`/home/user/platform` 以外（上传文件、`~/.gocache`）全丢，分支 HEAD 可能回退到旧树。每个整块做完立刻 `git commit && git push origin <会话分支>`；重置后 `git fetch && git reset --hard origin/<分支> && git clean -fd`，再跑第 1 步。需要长期保留的外部文件（规范、工具）提交进仓库（`docs/standards/`、`tools/`）。
6. **分支纪律与双代理同步协议**（分支代理 = Arena 会话分支；集成代理 = 负责人授权的本地代理，在 main 上工作。双方都遵守，目标是不丢任何一行已提交代码）：
   - **永不 merge / cherry-pick / rebase / reset 到对方分支**（任一方向）。跨分支取内容只有一种方式：按路径复制，然后正常 commit/push 到自己的分支。路径复制不建立 Git 祖先关系，所以 `git merge-base` 不是同步点，提交号相同与否也不说明已同步。
   - **内容基点 B**：双方每轮以同一个「上次已确认同步」的提交为比较基点（上一轮回执里写明的 main SHA 或分支 SHA）。本轮一切「谁改了什么」都用 `git diff --name-only B <对方HEAD>` 与 `git diff --name-only B HEAD` 算，不凭记忆。
   - **交接（分支 → main）**：分支代理先 commit/push 全部待交付改动，回执写明：冻结 HEAD、本轮基点 B、`git diff --name-only B HEAD` 的路径清单。集成代理只拿这个冻结 HEAD。
   - **集成（main 上）**：集成代理按路径合成——仅一边变化的文件逐字节取该边，两边都变的文件以 B 为 base 三方合成（`git merge-file`），生成物（`host.ts`、Catalog json 等）用工具重生成而不是手拷。不删除任何一边的业务修复。回执写明：main 提交 SHA、冲突文件及解法、测试边界。
   - **取回（main → 分支，常态）**：回执到达时分支通常已有新提交——分支代理不停工等回执。分支代理先核对 main 确实含自己冻结 HEAD 的全部路径，然后同样按路径三方取回：M = `git diff --name-only B <main-SHA>`，S = `git diff --name-only B HEAD`；M∖S 直接 `git checkout <main-SHA> -- <路径>`，M∩S 三方合成，生成物重生成；提交并推送。
   - **同步确认不是 tree 相同**，而是：`git diff --name-only <main-SHA> HEAD` **恰好等于**本轮新工作的路径清单（分支回执里列出这份清单与新的冻结 HEAD）。只有分支没有任何新工作时，才退化为 tree SHA 相同（此时也可以用 `git restore --source=<main-SHA> --staged --worktree -- .` 整树复制再提交；有新工作时**不做整树覆盖**）。
   - **下一轮基点**：取回后，该 main SHA 成为新的 B；分支在此之上的提交就是下一次交接的内容。
   - **大扫除期间（ADR-0080，直到其 §3 各波全部关闭）的补充**：结构调整只在分支上做。集成代理在 main 上发现的修复，**不改已搬家的文件**，而是把修复以"路径 + 补丁"形式随回执发给分支代理，由分支代理落到新位置；每波结束分支代理交接一次，取回后基点 B 前进到该 main SHA。回执里的 M∩S 若含被搬家的文件，以分支侧（新路径）为准，main 侧改动由分支代理手工落位。
   - **本地集成交付必须更新运行环境**：每次 main 代码更新后运行 `deploy/local/update.sh`，重建并核对酒店 `8495`、制造 `8490` 两台宿主的 Git 版本、健康和页面资源，再给回执。工作台创建的 WMS 等应用共用所属宿主入口，不另建应用专用容器或固定端口；保留原数据卷。
   - 注意 fetch/pull 只把对象拉进仓库，不等于工作树已同步；沙箱重置后先 `git fetch && git reset --hard origin/<会话分支>` 恢复自己的分支，再按上面取回。
7. **写文件**：编辑工具的相对路径以仓库根为准；拿不准就用绝对路径。
