# 执行队列

只放当前要交付的工作。需求和验收归 [SRS/SOW](Platform-Refactoring-SOW.md)，提交与资产边界归 [ADR-0097](ADR/0097-submission-assets-and-host-boundaries.md)，全栈声明式业务语义与三层工程收口归 [ADR-0098](ADR/0098-declarative-business-decision-semantics.md)，超越 Palantir 的六步心流应用构建工作台归 [ADR-0099](ADR/0099-intent-driven-application-studio-beyond-palantir.md)，能力现状归 [Platform](Platform.md)。本队列不复制这些内容，不保存完成记录。

## 当前：ADR-0098 三层工程门槛收口与 ADR-0099 六步心流工作台改造

ADR-0098 的 MVP 主干与第一批正确性加固（集合外键/版本下推与有界扫描保护、`Do/After` 原子封存、受控创建禁绕过、在途工艺版本固定、`TestBuilderMESEquivalence`）已落地，但距离“客户能够安全、独立、长期维护一套完整 MES”仍必须依次完成以下**三层工程收口块**与 **ADR-0099 工作台重构**：

| 顺序 | 交付块 | 停止条件 |
|---|---|---|
| 1 / 当前块 | **G1 第一层门槛收口：业务决定完备正确性（Business Decision Correctness）。** ① 将 `Quantifier` 子条件 `Where` 的标量与范围谓词完整编译下推至 `Query.Domain`，补齐声明式唯一性约束校验；② 在 `Action.Updates` 中强制校验目标对象生命周期的合法起始状态（`From`）与字段写权限边界；③ 将主数据版本快照从字段约定收敛为显式的版本化引用绑定契约。 | 覆盖超页集合唯一性/全称校验、非法父状态跃迁拒绝、跨版本工艺演进与恢复的宿主测试及 `make check` 通过。 |
| 2 | **G2 第二层门槛收口：真全栈声明式配置（True Declarative Configuration）。** ① 将 `Input.Filter` 的跨引用路径与集合条件接入服务端受控候选查询，消除客户端单页过滤局限；② 将 `PageProcessFlow` 正式接入 Workshop 页面契约（`page_widgets`、独立 Widget 插件与检查器、跨引用表格列），用真实装配的 Workshop 页面完成 MES 操作台配置并移除通用记录页上的隐式字段推导。 | Workshop 页面契约测试、MES 操作台候选封存激活测试与 `make check` 通过。 |
| 3 | **G3 第三层门槛（ADR-0099 Wave 1）：六步心流主干与领域蓝图画布（Step 1 & 2）。** 按 [ADR-0099](ADR/0099-intent-driven-application-studio-beyond-palantir.md) 重构构建器入口为六步心流主干；实现「领域蓝图与状态机画布」（卡片内联建字段/明细列、拖拽连线自动生成双向关系与有序步进契约、消灭跨对象发布死锁、底部常驻实时样本数据表）。 | 构建器画布单测、类型检查与 `make check` 通过。 |
| 4 | **G4 第三层门槛（ADR-0099 Wave 2 & 3）：四段式可视化决定画布、语义直绑操作台与多角色彩排沙箱（Step 3–6）。** ① 将 `ActionTypeEditor` 重构为四段式可视化决定画布，用级联路径胶囊（Pill Picker）彻底替换手写 `step:/split:/rollup:/append:` DSL 字符串，接通底部实时样本试跑；② Workshop 支持按对象语义路径直绑组件（隐藏底层 PageVariable/QueryPlan 管道）与多角色故事板彩排沙箱（Role-Play Storyboard）。 | 从空白项目按六步心流零代码搭出完整 MES 并通过全链路彩排与 `make check`。 |
| 并行保留 | **W4 独立接手与体验验收。** 接手者按 [Apps 两行业路线](Apps.md#两行业交付与接手操作) 完成受支持交付、升级与故障诊断；负责人走查共享输入、运行输出和真实 AI 人工接管。 | 独立人员完成操作、负责人确认使用效果，SRS §7.3 有可复核结果后移除此项。 |

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

## 收口规则

- 原 W0 全平台盘点、独立 W5 清理阶段和 XXL/L 编号移出队列；必要核查和删除由对应代码改动一并完成。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0047 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
