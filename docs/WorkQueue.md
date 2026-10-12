# 执行队列

只放当前要交付的工作。需求和验收归 [SRS/SOW](Platform-Refactoring-SOW.md)，提交与资产边界归 [ADR-0097](ADR/0097-submission-assets-and-host-boundaries.md)，全栈声明式业务语义与零代码行业应用等价重建归 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)，能力现状归 [Platform](Platform.md)。本队列不复制这些内容，不保存完成记录。

## 当前：独立接手与体验验收（W4 & ADR-0098 走查）

ADR-0097 W1–W4 与 ADR-0098 B1–B6（复合本体 `tags/lines` 与自定义创建动作 `Action.New`、跨对象集合包含與量词守卫、富值源与条件分支流转、多记录动态拆批与父单聚合上卷、动态上下文动作表单 `When/DefaultFrom/Filter`、工序泳道流转图 `RecordStepFlow`、应用专属记录路由 `Opens`、业务决定配方及 `TestBuilderMESEquivalence` 全链路重放证明）已完成工程实现与自动化验证；剩余工作为真实浏览器体验走查与独立接手验收。工程证据与支持边界见 [ADR-0097 §7](ADR/0097-submission-assets-and-host-boundaries.md) 与 [ADR-0098 §5](ADR/0098-declarative-business-decision-semantics.md)。

| 顺序 | 交付块 | 停止条件 |
|---|---|---|
| 1 / 待走查与独立验收 | **完成 ADR-0098 构建器体验走查与 W4 独立接手验收。** 负责人走查 `ActionTypeEditor`（条件分支、量词守卫、关联更新、决定配方）、动态过滤动作表单、工序泳道流转图及共享输入/AI 人工接管；接手者按 [Apps 两行业路线](Apps.md#两行业交付与接手操作) 完成交付与诊断。只修实际阻断。 | 负责人确认使用效果、独立人员完成操作后移除此项。 |

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

## 收口规则

- 原 W0 全平台盘点、独立 W5 清理阶段和 XXL/L 编号移出队列；必要核查和删除由对应代码改动一并完成。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0047 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
