# 执行队列

只放当前要交付的工作。需求和验收归 [SRS/SOW](Platform-Refactoring-SOW.md)，提交与资产边界归 [ADR-0097](ADR/0097-submission-assets-and-host-boundaries.md)，全栈声明式业务语义与零代码行业应用等价重建归 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)，能力现状归 [Platform](Platform.md)。本队列不复制这些内容，不保存完成记录。

## 当前：全栈声明式业务语义与零代码 MES 等价重建（ADR-0098）

负责人已明确下一阶段主线：改造 Platform 的全栈业务语义表达与执行体系，把数据建模、后端业务决定、动态上下文表单、前端领域操作台与流程可视化、应用专属记录路由的定义权从原生 Go 与 React 代码全部转移到可版本化、可验证的应用定义上，让 FDE 与零代码背景的业务玩家不接触任何前后端代码即可从头到尾构建完整企业应用，并通过全栈零代码等价重建原生 MES（`apps/mes/server` + `web/packages/mes`）完成证明。架构设计与六层模型见 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)。

| 顺序 | 交付块 | 停止条件 |
|---|---|---|
| 1 / 当前块 | **B1 领域本体扩展与动作决定核（Ontology v2 + Decision Core）。** ① `buildapi.Field` 新增 `tags`（多值集合）与 `lines`（结构化内嵌明细行），`Action` 支持 `New: true`（自定义业务创建动作）；② `Condition` 支持 `record.<path>`、`input.<ref>.<path>`、`in / not in / contains all / contains any` 与关联集合量词（`all / none / any / count`）；③ 统一 `Set` 值源支持路径、`formula:<expr>`、`table:<name>.<col>` 与 `step:first/next`；新增 `Action.Branches` 按条件分支决定目标状态与赋值；④ 更新 `ObjectTypeEditor` / `ActionTypeEditor`、`host.ts` 与中英文词典。 | `TestActionDecisionCore`（含 `CheckReplay`）、`TestAPIContract`、`TestLanguages` 与 `make check` 通过。 |
| 2 | **B2 多记录原子副作用、会签与路径范围（Multi-Record Effects, Quorum & Scope）。** ① `Action.Creates` 支持动态条数 `Count`、`Foreach`、`$index`/`$count` 与 `split:<total>:<count>` 整数余数均摊原语；② 新增 `Action.Updates` 支持在同一决策内按量词守卫更新关联父/目标记录的状态与 `rollup:sum/count` 汇总字段，并触发下游 Flow；③ 支持按关联引用路径解析的动作级组织单元范围守卫（`Caller.Units`）；同步更新 `ActionTypeEditor`。 | `TestActionMultiRecordEffects`（含 1-to-N 拆分、末件完工自动完成父单并汇总产量、会签放行、`CheckReplay`）与 `make check` 通过。 |
| 3 | **B3 动态上下文动作表单与跨记录带参触发（Context-Aware Action Forms & Triggers）。** ① `buildapi.Input` 与 `platform.Field` 支持参数条件显隐/必填（`When`）、动态默认值（`DefaultFrom`）与基于当前 `record.<path>` / `input.<name>` 的引用参数动态候选过滤（`Filter`：能力包含、工序次序、唯一未占用过滤）；② `@platform/app` 的 `PayloadFields` / `ActionDialog` / `InlineActionForm` 接通动态候选过滤、条件显隐与默认值；③ 支持表格行/按钮跨对象带参预填触发目标动作与行级条件显隐。 | 后端输入契约测试、前端动态表单单测与 `make check` 通过。 |
| 4 | **B4 声明式业务操作台与工序泳道可视化（Declarative Operational UI & Process Flow）。** ① 新增声明式 `ProcessFlow`（工序/流程泳道图）页面组件（底层复用 `<FlowSteps>`），支持绑定步骤集合、工作中心泳道与当前活动记录在途位置/状态/异常叠加；② `Table` 支持跨引用路径列（Lookup Columns）与顶栏预设状态视图切换器（`ViewPresets`）；③ 支持内嵌明细行展示（`RecordLines`，含工艺参数 `setpoint/range/limit/note` 格式化）与基于当前记录引用字段的动态关联面板。 | 页面契约测试、Workshop 组件检查与 `make check` 通过。 |
| 5 | **B5 租户应用专属记录视图路由、AI 绑定与零代码创作配方（App Routing, AI Advice & Recipes）。** ① `build.app` 支持声明 `opens`（点击记录引用自动打开应用内装配的专属 Workshop 操作台页面），在 `tenantApps.tsx` 与 `useOpenRecord` 接通；`build.object` 支持声明 `advice`；② 在 `ActionTypeEditor` 与 `Workshop` 中内置面向业余玩家的四大业务决定配方与操作台页面模板。 | 应用路由与配方装配测试及 `make check` 通过。 |
| 6 | **B6 全栈零代码 MES 等价重建与端到端证明（Full-Stack Zero-Code MES Equivalence Proof）。** 不写一行 Go 或 React 代码，完全通过 `build` 定义构造并发布完整 MES 应用（涵盖后端全部领域不变量 + 前端工艺泳道页、计划单转单页、批次列表页、SFC 执行操作台、动态开工/返工弹窗、双人签名处置与 ERP 确认闭环）。 | `TestBuilderMESEquivalence`（含 `CheckReplay`、候选激活与恢复）、前端路线验证与 `make check` 通过。 |
| 并行保留 | **W4 独立接手与体验验收。** 接手者按 [Apps 两行业路线](Apps.md#两行业交付与接手操作) 完成受支持交付、升级与故障诊断；负责人走查共享输入、运行输出和真实 AI 人工接管。 | 独立人员完成操作、负责人确认使用效果，SRS §7.3 有可复核结果后移除此项。 |

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

## 收口规则

- 原 W0 全平台盘点、独立 W5 清理阶段和 XXL/L 编号移出队列；必要核查和删除由对应代码改动一并完成。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0047 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
