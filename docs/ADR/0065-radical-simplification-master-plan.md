# ADR-0065 — 架构激进简化总纲：XXXL 级平台重构与三分之一代码削减

<a id="adr-0065"></a>

状态：**决定已接受，正在实施**（2026-10-21，负责人指令 XXXL 级重构）。本文是 ADR-0062/0064 之后的第一份总体重构纲领，统领后续全部 XXL 级专项 ADR。本文定义“为何必须激进简化、简化到什么程度、如何度量、如何分阶段实施”；具体实现分工归各专项 ADR，不在本文重复。

取代关系：本文**不取代** ADR-0062 的全项目收敛范围、不取代 ADR-0064 的审计裁定与删除条件、不取代 ADR-0001/0006/0037/0038 等的硬边界。本文是它们的**执行放大器**：将“收敛、减法、归属清晰”的原则推向可度量的 XXXL 级工程。后续专项 ADR（0066–0072）均为本文的子项，其状态行须引用本文编号与边界。

## 1. 为什么必须是 XXXL 级

### 1.1 规模事实（核对提交 b96793a）

| 维度 | 规模 | 问题 |
|---|---|---|
| `capabilities/server/*.go` | 423 个文件平铺在根包 | 无模块边界，任何文件可读写 Tenant 的 40+ 字段 |
| `capabilities/server/platform/*.go` | 270 个文件平铺 | 162 个 `page_*.go` 各自为一个 widget 的校验，共享 `Section`/`PageDocument` 但无分组 |
| `capabilities/server/apps/build/*.go` | 103 个文件 | 与 `internal/construction` (43 文件) 的职责重叠：前者是 app 包装，后者是纯编译，但类型别名层 `public.go` 使两套定义并存 |
| `capabilities/server/internal/host/*.go` | 13 个文件仅为接口 | 每类消费 1 接口，宿主仅为满足接口而暴露方法 |
| `capabilities/server/accepted_*.go` | 21 个结果族文件 | 每族自带封套、校验、持久化协调的复制 |
| `web/packages/build/src/i18n.ts` | 4 300 行翻译表 | 与 `web/packages/app/src/i18n.ts` (1 071 行) + `web/packages/platform/src/i18n.ts` (1 110 行) 的键空间重叠，无共享键表 |
| `web/packages/build/src/workshop/page-editor/` | 30+ 文件 | `draft.ts` / `diagnostics.ts` / `canvas-layout.ts` / `compatibility.ts` 各自维护一套“草稿→校验→预览” |
| `web/packages/build/src/workshop/module-import/` | 50+ 文件 | `application.ts` / `flows.ts` / `compile.ts` / `diagnostics.ts` 各自解析同一 workshop.json |

上述不是“文件太多”的审美问题，而是：

1. **同一职责有两个实现**：`apps/build` vs `internal/construction`；`knowledge.go` vs `knowledge_vectors.go`；`aicall.go` 的 `Message/Tool/ChatRequest` vs `platform.Message/Tool/ChatRequest`。
2. **一个职责被拆到多处**：`Tenant` 结构体 40+ 字段跨 15 个文件读写；`Section` 的 30+ widget 校验分散在 80 个 `page_*.go` 但共享同一 `Check()` 入口。
3. **转接层无附加价值**：`public.go` 249 行纯别名；`aicall.go` 6 个 `type X = platform.X`；`internal/host/blueprint.go` 9 行仅为一个接口。

### 1.2 目标：手写生产代码净减 1/3

基准以 `make code-size` 的 `production` 类为口径（排除 `tests` / `generated` / `build/examples` / `data/docs/config`）。当前手写生产代码约 160k 行（Subtraction §12 实测 159 924 行，2026-10-19）。目标净减 **≥ 53k 行**。

达成路径不是机械删行，而是：

- 消除重复职责（一份实现服务同一能力）。
- 删除无效转接层（别名、空接口、重复验证）。
- 合并碎片化校验与状态（一次读取、一处判定）。
- 退役已完成迁移的兼容路径。

不以格式化、生成物、文档、测试断言删除充数。不以牺牲已承诺能力、历史兼容、审计语义换取数字。

### 1.3 度量与门槛

| 检查 | 口径 |
|---|---|
| `python3 scripts/code-size.py --output .build/code-size.json` | 唯一规模口径，需 `cloc`；无 `cloc` 时以 `wc -l` 的生产 Go/TS 估算，提交信息注明 |
| `scripts/verify.sh format composition-static` | 边界检查必须通过（新增内部包反向依赖检查见 ADR-0064 §5.5） |
| `go vet` / `go test` / `pnpm typecheck` | 受影响包的回归必须通过 |
| 人工走查 | 完整构建任务：空白应用→对象→动作/规则→页面→流程→试跑→发布→修改（ADR-0062 §4） |

每块 XXL 交付必须报告：删除文件清单、净减行数（`code-size` 前后）、边界检查与测试结果。

## 2. 架构愿景：从平铺到能力分组

### 2.1 目标依赖方向

```
HTTP / 部署 / 组合入口 → 租户提交/恢复/运行协调 → 宿主消费者角色
       → 记录模块 (internal/records) / 接受模块 (internal/acceptance)
       → 纯编译模块 (internal/construction) / 企业模块 (internal/enterprise)
       → 平台应用 (apps/*) → 公共应用 API (platform/) → 内核契约 (contract/)
```

禁止：`platform/` 反向依赖 `internal/host` 或具体 `apps/*`；`internal/*` 反向依赖宿主根包；应用间直接 import。

### 2.2 目录重整（分阶段，非一次性建空目录）

| 阶段 | 内容 | 归属 ADR |
|---|---|---|
| 1 | 消灭纯转接层：`apps/build/public.go`、`aicall.go` 别名、`internal/host` 单方法文件 | 0066 |
| 2 | 合并重复校验：`page_*.go` 按 widget 家族分组；`accepted_*.go` 公共封套抽为 `internal/acceptance/envelope` | 0067 |
| 3 | 收敛知识与向量：`knowledge.go` + `knowledge_vectors.go` → `internal/knowledge` | 0068 |
| 4 | 拆解 Tenant God Object：`host.go` + `operations.go` + `effects.go` → `tenant/` 协调器 + 组件 owners | 0069 |
| 5 | 前端统一草稿与校验：`workshop/page-editor` 合并 `draft.ts`/`diagnostics.ts`/`compatibility.ts` | 0070 |
| 6 | 合并 workshop import：50 文件 → `workshop/import/` 单一编译管线 | 0071 |
| 7 | 统一 Journal 存储：`journal/postgres.go` + `journal/file.go` + `journal/memory.go` 共享 `State` 接口 | 0072 |

每阶段迁移一个模块即删除旧实现，不长期双写。每阶段后通过 `composition-static`。

### 2.3 性能原则

重构的性能收益来自减少重复工作，不是新增缓存层：

- 记录历史只算一次（ADR-0064 §4.3 已部分实现），不在多处重算 `recordHistory()`。
- 向量与全文索引只切一次变更记录，不在每次搜索时遍历全租户（`knowledge.go:305 dirtyRecords` 已部分优化，仍可合并）。
- 唯一约束私有变化检查复用 `records.UniqueIndex`，不全表扫描（`business_constraints.go` 待合并）。

不以“性能”为名引入复杂优化；先有 benchmark 证据再加复杂度。

## 3. 不可动边界（重申）

继承 ADR-0038 §9 全部 16 条 + ADR-0064 §3.1 9 条。特别强调：

1. 内核契约（`contract/`）不动；向量先行。
2. 单权威单写者、世代检查、私有决定→持久接受→纯应用的顺序不动。
3. 已发布定义、历史字节、幂等答复、在途实例的读取责任不动。
4. 权限判定、发布审批、效果投递语义不动。
5. 新增内部包不得扩大公开 API；`platform/` 仍是应用唯一可导入的面。

## 4. 实施纪律

- 一次完成一整块 XXL，不跨块半完成提交。
- 先删纯别名与无消费者代码，再合并重复职责，最后拆 God 对象。
- 每次提交注明 ADR-0065 + 子 ADR 编号。
- 每块完成后 `git commit && git push origin <分支>`；沙箱重置后按 AGENTS.md §6 协议取回。

## 5. As built（本轮已交付）

- [ ] 目标 `production` 从 ~160k → ~107k 行（-33%） — 多轮分期，当前记录见各子 ADR 的 As built
- [x] 本文作为 XXXL 纲领已接受，后续 7 份 XXL 已立项（0066–0072）
- 具体删除与测量归各子 ADR；本文不重复

## 6. 关联 ADR

- ADR-0062：全项目收敛范围与文档分工（本文的范围父项）
- ADR-0064：审计裁定、删除条件、目标目录组织（本文的直接前置）
- ADR-0066–0072：各 XXL 专项（本文的子项）
