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
| `capabilities/server/` | 宿主运行时；`platform/` 是应用唯一可导入的应用 API；`internal/host` 宿主内接口；`apps/` 平台应用（build/flow/ai/work/enterprise/knowledge/files/relations/core） |
| `apps/<id>/server/` | 自治业务应用；`protocols/` 跨应用协议；`solutions/` 组合宿主 |
| `web/packages/ui`、`web/packages/app` | 共享 UI Kit 与前端应用 API |
| `web/packages/build` | 构建者应用：Ontology / Workshop / Automate / AI Functions / Code / Projects / Releases |
| `web/packages/platform` | 治理与运维应用：Control Panel / Runs / Data Connection / Agents / AI / Knowledge / Host Console |
| `web/apps/workspace` | 唯一工作区：`session/`（身份）、`host/`（读取与决策）、`shell/`（Rail、Home、门户、Explorer、Lineage）、`chrome.tsx`（共享视图） |
| `web/apps/catalog`、`web/e2e` | Asset Library（资产库 + 代码沙箱）；浏览器路线 |

## 检查

统一入口 `scripts/verify.sh <step>`：`contract`、`formal`、`capabilities`、`composition`、`web-check`、`web`、`pms`、`mes`、`deploy`、`format`、`ci`。按影响面选：Go 宿主 → `capabilities format`；应用/协议 → `composition format`；Web → `web-check`（整块交付或发布节点再选一条 `web` 浏览器路线）；纯文档 → 链接与 `git diff --check`。外观与手感靠截图和负责人走查，不写像素断言。

## 沙箱（Arena 等离线代理环境）里怎么干活

网络只通 GitHub、npm、PyPI；Go 模块代理全封。仓库自带离线所需，照做即可：

1. **Go**：`cd /home/user/platform && bash scripts/sandbox-go.sh`，然后 `export PATH=/home/user/.go-toolchain/go/bin:$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache`（脚本会打印这一行）。工具链来自 `tools/go/`（找不到则 `pip download go-bin`），依赖来自 `capabilities/server/vendor/`（已提交，别跑 `go mod tidy`/`go mod vendor`）。`go build ./...`、`go vet`、`go test` 都在 `capabilities/server/` 目录下跑；`apps/*/server`、`solutions/*` 是独立模块、依赖未 vendor，只能 `gofmt`，编译交给本地。
2. **Web**：`cd web && corepack pnpm install --frozen-lockfile`（npm 可达，约 5 秒），检查按包跑 `corepack pnpm exec tsc -p web/packages/<pkg>`；`pnpm -r typecheck` 会 OOM（2 核）。无浏览器：Playwright 不可用，UI 只能 tsc + 代码走查，报告时写明"未在浏览器观察"。
3. **宿主类型变了**：`go run ./cmd/api-types`（在 `capabilities/server/`）再生成 `web/packages/kernel/src/gen/host.ts`，否则 `TestAPIContract` 失败。文案变了：补 `i18n/zh-CN.json` / 包内 `i18n.ts`，否则 `TestLanguages` 失败。
4. **轻量验证**：`go test ./apps/<改动包>/... ./platform/...` + 根包按名跑 `go test . -run 'TestLanguages|TestAPIContract'`；全量根包测试约 40 秒，`TestRecordsAtScale` 计时抖动可忽略。
5. **沙箱会被重置**：`/home/user/platform` 以外（上传文件、`~/.gocache`）全丢，分支 HEAD 可能回退到旧树。每个整块做完立刻 `git commit && git push origin <会话分支>`；重置后 `git fetch && git reset --hard origin/<分支> && git clean -fd`，再跑第 1 步。需要长期保留的外部文件（规范、工具）提交进仓库（`docs/standards/`、`tools/`）。
6. **分支纪律**：只在会话分支上提交与推送，**不要 merge**（任一方向）；需要 main 上的内容用 `git checkout origin/main -- <路径>` 复制后正常提交。
   - **交接以固定提交为准**：交出分支前先提交并推送全部待交付改动，注明来源分支 HEAD、上次已取回的 main SHA、改动路径；未确认接入 main 的分支工作不能被 main 版本覆盖或 reset。
   - **本地集成**：由负责人授权的本地代理在 main 按路径合成。以上次双方确认的内容基点区分两边独有文件与重叠文件，重叠文件三方合成；生成物由工具重生成。路径复制不建立 Git 祖先关系，不能把 `git merge-base` 当作最近同步点，也不能只看提交号宣称已经同步。
   - **反向取回后再开工**：本地代理推送整合后的 main SHA 并给出回执后，分支代理确认自己的来源改动全部保留，再按路径取回该 main SHA（包含代码、测试、文档、生成物），提交并推送到自己的分支；在没有新开发改动时，以两边 tree SHA 相同作为同步确认，并回报分支提交号。完成确认后再开始下一块；后续新增提交按下一次交接处理。
7. **写文件**：编辑工具的相对路径以仓库根为准；拿不准就用绝对路径。
