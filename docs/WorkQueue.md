# 执行队列

只放当前要交付的工作。需求和验收归 [SRS/SOW](Platform-Refactoring-SOW.md)，提交与资产边界归 [ADR-0097](ADR/0097-submission-assets-and-host-boundaries.md)，声明式业务决定语义与无代码 MES 等价重建归 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)，能力现状归 [Platform](Platform.md)。本队列不复制这些内容，不保存完成记录。

## 当前：声明式业务决定体系与无代码 MES 等价重建（ADR-0098）

负责人已明确下一阶段主线：改造 Platform 的业务语义表达与执行体系，把业务规则的定义权从原生 Go 代码转移到可版本化、可验证的应用定义上，让 Builder 能把基础能力组合成符合领域不变量的完整业务决定，并通过无代码等价重建原生 MES 证明 FDE 与业务构建者可脱离后端源码构建完整企业应用。架构设计与分块边界见 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)。

| 顺序 | 交付块 | 停止条件 |
|---|---|---|
| 1 / 当前块 | **B1 动作决定核（守卫 + 富值源 + 条件分支流转）。** 扩展 `buildapi.Condition` 支持多跳引用路径（含 `input.<ref>.<path>`）、集合算子（`in`/`not in`/`contains all`/`contains any`）与关联集合量词（`all`/`none`/`any`/`count`）；统一 `Set` 值源支持关联路径、`formula:<expr>`、`table:<name>.<col>` 与 `step:first/next`；新增 `Action.Branches` 按条件分支决定目标状态与赋值；同步更新 `ActionTypeEditor`、`host.ts` 与中英文词典。 | `TestActionDecisionCore`（含 `CheckReplay`）、`TestAPIContract`、`TestLanguages` 与 `make check` 通过。 |
| 2 | **B2 多记录原子副作用（批量拆分创建 + 父子联动上卷）。** 扩展 `Action.Creates` 支持动态条数 `Count`、`Foreach`、`$index`/`$count` 与 `split` 整数余数均摊原语；新增 `Action.Updates` 支持在同一决策内按量词守卫更新关联父/目标记录的状态与 `rollup:sum/count` 汇总字段，并触发下游 Flow；同步更新 `ActionTypeEditor`。 | `TestActionMultiRecordEffects`（含 1-to-N 拆分、末件完工自动完成父单并汇总产量、`CheckReplay`）与 `make check` 通过。 |
| 3 | **B3 工艺版本快照、职责分离会签与路径组织范围。** 支持按关联引用路径解析的动作级组织单元范围守卫（`Caller.Units`）、有序工序回退校验（`reworkable`）及双人异义电子签名（21 CFR Part 11 Quorum）配方与编辑器装配。 | `TestActionQuorumAndScope`（含 `CheckReplay`）与 `make check` 通过。 |
| 4 | **B4 无代码 MES 等价重建与端到端证明。** 仅使用 `build` 定义完整构建 MES（产品、版本化工艺路线与工序参数、工作中心与设备能力、工单拆批下达、能力匹配开工、工序流转与末步完工、NC 挂起与双人签名处置、父单自动完工汇总与 ERP 确认闭环），不写行业 Go 代码。 | `TestBuilderMESEquivalence`（全链路业务不变量、候选封存激活、快照与 `CheckReplay`）及 `make check` 通过。 |
| 并行保留 | **W4 独立接手与体验验收。** 接手者按 [Apps 两行业路线](Apps.md#两行业交付与接手操作) 完成受支持交付、升级与故障诊断；负责人走查共享输入、运行输出和真实 AI 人工接管。 | 独立人员完成操作、负责人确认使用效果，SRS §7.3 有可复核结果后移除此项。 |

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

## 收口规则

- 原 W0 全平台盘点、独立 W5 清理阶段和 XXL/L 编号移出队列；必要核查和删除由对应代码改动一并完成。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0047 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
