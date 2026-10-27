# Tasks

## 1. 判据层

- [x] 1.1 新增 `scripts/architecture.py`：十项度量、`--check`/`--write`、基线与零容忍两类口径；每项在脚本内写清「为什么这一项会滋生补丁」。
- [x] 1.2 生成并提交 `.architecture-baseline.json`（当前实测值）；确认每项都能用一次真实改动触发失败与下降提示。已实测两类失败：把 `describeAsset` 改回开关触发 `describe_asset_lines` 上升；规格引用路径随搬迁失效触发 `missing_spec_sources`。
- [x] 1.3 接入 `scripts/verify.sh` 的 `composition-static` 与 `make check`；`scripts/escapes.sh` 不重复实现同类计数。

  注：`make check-go` 经 `composition-static` 已含 `architecture`；实测 `go-format ok`、`boundaries ok`、`escapes ok: 0 known`、`architecture ok: 10 measures at baseline, 9 zero-tolerance at 0`。

## 2. 种类与定义

- [x] 2.1 度量 asset kind 的扇出与 `describeAsset` 行数，冻结进基线；在 `platform` 建立种类声明的读取入口（沿用 `pageui/widgets.json` 的 embed 模式），核心不新增 `case`。

  注：扇出改按「`platform/` 之外引用该 kind 的宿主文件数」计量——平台内一个 kind 一个扫描器是目标形状，拆文件不应让度量上升。`describeAsset` 651 → 58 行；19 个 case 拆成 `platform/asset_scanners.go` 的 `assetScanners` 表 + 每 kind 一个扫描器，共享排序、去重与重复能力校验留在原处。`go build ./...`、`go vet ./...`、`go test ./platform/...` 通过。未做：`widgets.json` 一类的 embed 声明（widget 契约已由该文件承担，asset kind 暂无同等外部消费者）。
- [x] 2.2 把 `Action` 的旧字段集合与 `Effective*` 合并函数数冻结进「双重形状」基线（`action_legacy_fields` = 8、`effective_merge_methods` = 4，只降）。
- [x] 2.3 `web/packages/build/src/catalog.examples.tsx` 改为只产 `transaction` 形状；`legacy_shape_producers` 归零。

  注：旧字段从结构体删除未做——历史发布映像只保存旧字段，需要先有一个「按旧形状解码、再在内存里编译」的独立入口，否则历史映像编译会丢字段。这是下一批的前置条件，已写进退役条件。

## 3. 依赖与规模

- [x] 3.1 边界规则写成硬检查：`platform` 不得引用 `internal/*`、`internal/*` 不得引用 `apps/*`（零容忍）。

  注：检查当场发现一处真实违例——`platform/rules.go` 引用 `internal/rules`。已把规则求值实现移到 `platform/rules`（3 个导入方，9 个文件搬迁），`platform` 不再引用 `internal/*`；`go test ./platform/...` 通过。
- [x] 3.2 根包四项规模、`Tenant` 方法数、`platform` 的 `reflect.`/`map[string]any`、各应用体量进基线。
- [x] 3.3 根包每个文件必须被域表认领。

  注：新增 `capabilities/server/domains.go`：263 个文件按 129 个前缀归入 15 个域（最大 `records` 46 文件，占 17%）。检查为三项：目录里每个文件的前缀都被认领、每个认领还有文件、任一域不得超过该包文件的五分之一。实测反向验证：新建未认领文件即失败。代价是该包多一个文件（147 前缀），基线的 `root_package.files/prefixes/singleton_prefixes` 因此上调 1——这是本 change 唯一一次允许上升，理由是它承载的正是「不许再有无主文件」这条判据。

- [x] 3.4 旧形状的读取点必须被记录且只能缩小。

  注：`legacy_action_readers` 记录 13 个仍同时懂两种形状的文件（`apps/build` 7 个、根包 3 个、`construction` 与 `platform` 各 1–2 个）。新文件读取旧字段即失败；缩小后必须同批下调。实测反向验证：新建一个读取 `a.Posts` 的探针文件立刻失败。

## 4. 前端

- [x] 4.1 `EdgeClient` 抛结构化 `HttpError`（status/path/code/detail），人类可读消息保持原样；`apiErrorMessage` 语义不变。
- [x] 4.2 删除 `App.tsx`、`shell/legacy.ts`、`govern/package-preparation.tsx`、`govern/system.tsx` 的 `/HTTP \d{3}/` 判定，改用 `HttpError.unauthenticated/forbidden/missing/unavailable`；`http_text_matches` 归零。

  注：`govern` 的包准备测试新增一条反向断言——消息里写着 `HTTP 403` 的普通错误**不得**被判为无权，证明判定来自状态而非文本。
- [x] 4.3 `web/packages/ui/src/shell/route.ts` 之外新增 `web/apps/workspace/src/shell/views.ts` 声明 29 个视图及其参数；`chrome.tsx` 注册的 20 个视图与 `shell/legacy.ts` 的退役目标必须落在表内；`keepParams` 只放行目标视图声明过的参数。三项检查（`undeclared_views`、`undeclared_retired_params`）为 0。
- [x] 4.4 `String(error)` 剩余使用冻结进基线（23 → 22，只降）。

## 5. 单一权威与门

- [x] 5.1 `docs/Architecture.md` 增加兼容读取登记表（三条解码器、退役格式、所有者、退役条件），并明确本文只解释结构、不复述规则。
- [x] 5.2 检查：`legacy*` 文件集合与登记表完全一致（`legacy_registry_drift`）；spec `Requirement:` 标题在现行规范内唯一（`duplicate_requirements`，归档与 change 差量不参与）；spec 来源路径存在（`missing_spec_sources`）。三项均为 0。
- [x] 5.3 新增 `.github/workflows/check.yml`（`pull_request` + `workflow_dispatch` 跑判据、边界、逃逸、宿主构建与 vet、格式、web 类型、spec 校验）；`release.yml` 不变。
- [x] 5.4 `AGENTS.md` 增加硬规则 11（判据先于重构）与检查入口表一行；Architecture 代码地图指向新脚本。

## 6. 集中验证

- [x] 6.1 宿主侧：`go build ./...`、`go vet ./...`、`go test ./platform/...`、`go test ./apps/build -run 'TestAsset|TestRelease|TestCandidate'` 通过；`scripts/verify.sh format`、`scripts/boundaries.sh`、`scripts/escapes.sh`、`scripts/architecture.py` 全绿。
- [x] 6.2 Web 侧：`pnpm -r run typecheck` 9 包全过；`pnpm -r run test` 全过（`kernel` 13、`ui` 29、`app` 4 文件 34、`govern` 5 文件 19、`build` 142、`catalog` 1、`catalog-app` 11）；`node scripts/catalog.mjs check` 通过（生成物已按工具重生成）。
- [x] 6.3 `make check-specs`（含归档）通过；本 change 的差量在归档时同步进 `runtime/host`、`platform/business-models`、`experience/workspace`、`runtime/deployment`。

  注：未在浏览器观察任何界面；本次改动不涉及渲染结构，只涉及错误判定与路由声明，浏览器证据留待下一次界面路线。

## 6.4 仍未做的两件与它们的前置条件

- **从结构体删除 8 个旧字段**：需要先决定「历史输入重放时对象如何字节级保留旧形状」。今天重放会把解码后的对象重新编码写回，若字段从结构体消失，写回的字节就变了，与「恢复只应用已保存映像、历史字节不重写」冲突。选项是把旧形状保留为解码器私有字段并在重放时原样回写（等价于今天的可观测行为，但结构体只有一种对外的形状），或明确做一次存储格式迁移。这不是机械改动，必须由负责人选一条。
- **`internal/construction` 的对象模型迁到 `platform.Entity`**：5,372 行、根包 59 个文件与 `apps/build` 71 个文件的导入面。`root_construction_importers` 已把这一项量化并冻结在 59，任何增长都会失败；下降需要一个独立的 change，先把 `platform.Entity` 扩到能承担对象模型，再逐文件迁移。

## 已知遗留（不是本 change 引入）

- `TestDependencyDiscoveryIncludesOriginalDatasetConfigurations` 在本次改动前的 HEAD 上同样失败（已用 `git stash` 对照确认）：`CLEAN` 数据集没有被管道写入。未在本 change 内修复，也未报告为通过。
- 未运行完整 `make verify`：形式化证明需要 Lean 工具链、组合与部署路线需要容器，沙箱不具备；因此本 change 只声明上面列出的检查范围。
