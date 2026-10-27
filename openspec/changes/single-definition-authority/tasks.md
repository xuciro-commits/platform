# Tasks

## 1. 判据层

- [ ] 1.1 新增 `scripts/architecture.py`：十项度量、`--check`/`--write`、基线与零容忍两类口径；每项在脚本内写清「为什么这一项会滋生补丁」。
- [ ] 1.2 生成并提交 `.architecture-baseline.json`（当前实测值）；确认每项都能用一次真实改动触发失败与下降提示。
- [ ] 1.3 接入 `scripts/verify.sh` 的 `composition-static` 与 `make check`；`scripts/escapes.sh` 不重复实现同类计数。

## 2. 种类与定义

- [ ] 2.1 度量 asset kind 的扇出与 `describeAsset` 行数，冻结进基线；在 `platform` 建立种类声明的读取入口（沿用 `pageui/widgets.json` 的 embed 模式），核心不新增 `case`。
- [ ] 2.2 把 `Action` 的旧字段集合与 `Effective*` 合并函数数冻结进「双重形状」基线。
- [ ] 2.3 `web/packages/build/src/catalog.examples.tsx` 改为只产 `transaction` 形状；确认旧形状在该文件的生产者归零。

## 3. 依赖与规模

- [ ] 3.1 边界规则写成硬检查：`platform` 不得引用 `internal/*`、`internal/*` 不得引用 `apps/*`（当前均为 0，防退化）。
- [ ] 3.2 根包四项规模、`Tenant` 方法数、`platform` 的 `reflect.`/`map[string]any`、各应用体量进基线。
- [ ] 3.3 根包每个文件必须被 `doc.go` 的域表认领；未认领即失败（先按现状登记，再逐个归域）。

## 4. 前端

- [ ] 4.1 `EdgeClient` 抛结构化 `HttpError`（status/path/code），保留人类可读消息；`apiErrorMessage` 语义不变。
- [ ] 4.2 删除 `App.tsx`、`shell/legacy.ts`、`govern/package-preparation.tsx` 的 `/HTTP \d{3}/` 判定，改用结构化状态；web 侧该正则计数归零。
- [ ] 4.3 `web/packages/ui/src/shell/route.ts` 增加视图参数表；`chrome.tsx` 的视图注册与 `shell/legacy.ts` 的退役目标必须落在表内。
- [ ] 4.4 `String(error)` 剩余使用冻结进基线。

## 5. 单一权威与门

- [ ] 5.1 `docs/Architecture.md` 增加兼容读取登记表（三条解码器、格式、所有者、退役条件），删除与 AGENTS/spec 重复的规范性复述。
- [ ] 5.2 检查：`legacy*` 文件集合与登记表完全一致；spec 的 `Requirement:` 标题全仓唯一；spec 来源路径存在。
- [ ] 5.3 新增 `.github/workflows/check.yml`（`pull_request` + `workflow_dispatch` 跑 `make check`）；`release.yml` 不变。
- [ ] 5.4 `AGENTS.md` 检查入口表补一行架构判据；Architecture 代码地图指向新脚本。

## 6. 集中验证

- [ ] 6.1 `make check-go`（构建、vet、gofmt、边界、逃逸、架构判据）全绿。
- [ ] 6.2 `make check-web`（生成一致性、Catalog、全包 tsc、受影响包单测）全绿。
- [ ] 6.3 `make check-specs`（含归档）通过；本 change 的差量在归档时同步进 `runtime/host`、`platform/business-models`、`experience/workspace`、`runtime/deployment`。
