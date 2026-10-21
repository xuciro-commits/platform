# ADR 索引

37 份 ADR，一份一个问题。**先在这里找到一份，再读那一份**，不要从头翻；历史原因去 Git，不留在文件里。

## 只读这 5 份就够开工

| 你要做什么 | 读 |
|---|---|
| 改产品形态、界面组织、构建者工作流 | [ADR-0018](0018-foundry-aligned-platform-experience.md) Shell、门户与工作区 · [ADR-0047](0047-application-studio.md) 应用设计台 |
| 改业务语义（对象、动作、规则、授权） | [ADR-0039](0039-business-transaction.md) 业务事务 · [ADR-0040](0040-one-rule-language.md) 规则语言 · [ADR-0044](0044-one-authorization-engine.md) 授权 |
| 改提交、恢复、发布、运行 | [ADR-0037](0037-submission-assets-and-host-boundaries.md) 提交与资产 · [ADR-0010](0010-accepted-results-and-tenant-recovery.md) 结果与恢复 |
| 动任何东西之前看边界 | [ADR-0038 §9](0038-build-the-capability-to-build.md#9-不可动的边界) 的 16 条 |
| 现在做什么、复杂度预算 | [WorkQueue](../WorkQueue.md) · [ADR-0064 §9](0064-architecture-subtraction-and-code-organization.md#9-复杂度预算) |

## 怎么读一份 ADR

1. **看状态行。** 一行说明起始日期、现行范围和并入了谁。状态行超过两行，说明这份 ADR 在替别的东西解释自己——那是要合并的信号。
2. **看取代范围，不看编号。** 只有同一问题、同一适用范围，较新的决定才覆盖较旧的。编号大不代表整篇作废。
3. **看证据边界。** "设计已接受 / 工程已实现 / 检查已通过 / 真实使用已验证"是四件事，不能互相推导；每份文末的 As built 只说实现与证据，不重复决定。

## 全部索引

| # | 决定 | 并入 | 什么时候读 |
|---|---|---|---|
| [0001](0001-kernel-as-contract.md) | 内核是语言中立的契约 | 原 0002、0012 | 改 `contract/` |
| [0003](0003-authority-migration-adopts-history.md) | 权威迁移完整采纳前任历史 | | 迁移权威 |
| [0005](0005-meaning-languages-api-contract.md) | 契约标识、租户词典与呈现分三层 | | 新增界面文案 |
| [0006](0006-decisions-across-apps.md) | 跨应用只走请求-应答，跨权威不原子 | | 想跨应用事务时 |
| [0007](0007-one-runtime-for-durable-work.md) | 持久工作只有一个运行时 | 原 0016 §13 | 任务、调度、持续 Flow |
| [0008](0008-ai-control-plane.md) | AI 治理平面与应用内 AI 是同一套运行 | 原 0054 | 模型接入、提议与接管 |
| [0009](0009-derived-content-declares-its-sources.md) | 派生内容声明来源，权限随来源收窄 | | 知识、派生读 |
| [0010](0010-accepted-results-and-tenant-recovery.md) | 已接受结果、租户恢复与 PG 运行状态 | 原 0063 | 改恢复逻辑 |
| [0014](0014-platform-catalog.md) | 资产目录六层、收录资格与单一归属 | | 改资产库 |
| [0018](0018-foundry-aligned-platform-experience.md) | Shell、门户、工作区与设计台的组织 | 原 0015、0016、0019 | 改界面结构 |
| [0020](0020-shared-ontology.md) | 共享本体：core 主数据、Interface 与扩展 | | 改共享对象 |
| [0022](0022-enterprise-modeling-layer.md) | 企业模型一份：UAF 词汇、视图与封存接口 | 原 0031、0034 | 改组织/人员 |
| [0024](0024-datasets-and-pipelines.md) | 连接、数据集、管道、匹配与回写 | 原 0023 | 改集成链路 |
| [0026](0026-business-core.md) | 账簿、单据编号与冲销 | | 会计语义 |
| [0028](0028-user-domain.md) | 用户域：成员、档案、偏好、生命周期 | | 改登录成员 |
| [0029](0029-build-layers.md) | 本地 / 提交前 / 发布三层，测试按层取舍 | 原 0017、0030 | 改 Makefile、删加测试 |
| [0033](0033-a-node-type-system.md) | 画布节点词汇与排布规则 | 原 0035 | 改画布 |
| [0037](0037-submission-assets-and-host-boundaries.md) | 唯一写入提交契约、资产生命周期与宿主角色 | | 改写入路径 |
| [0038](0038-build-the-capability-to-build.md) | 产品方向 + **16 条不可动边界** | | **动任何东西之前** |
| [0039](0039-business-transaction.md) | 业务事务：单一权威内的原子变更 | | 写动作 |
| [0040](0040-one-rule-language.md) | 条件、公式与决策表是同一门语言 | | 写规则 |
| [0041](0041-business-constraints.md) | 业务约束：模型自带的不变式 | | 写校验 |
| [0042](0042-business-version-and-time.md) | 业务版本与业务时间 | | 主数据随时间变化 |
| [0043](0043-record-sets-and-relations.md) | 记录集与关联是一等公民 | | 改关系 |
| [0044](0044-one-authorization-engine.md) | 四层判定合成一个授权决定 | 原 0025、0027 | 改权限 |
| [0045](0045-data-and-definition-evolution.md) | 数据与已发布定义的演进 | 原 0011 | 改已发布定义 |
| [0046](0046-application-artifact-and-runtime.md) | 应用是完整工件与运行形态 | | 改应用交付 |
| [0047](0047-application-studio.md) | 围绕一个应用构建的设计台 | | 改构建者界面 |
| [0048](0048-model-driven-ui.md) | 模型驱动界面、统一输入与专业组件 | 原 0036、0049、0021 | 加组件、改表单 |
| [0050](0050-builder-verification.md) | 构建中用真实样本验证 | | 改试跑 |
| [0051](0051-cross-system-coordination.md) | 出站效果、设备派发、观测与补偿共用一条通道 | 原 0004、0058、0059 | 对接外部系统 |
| [0053](0053-retiring-native-apps.md) | 原生参考应用的退出条件 | | 想删 `apps/*` 时 |
| [0055](0055-refusal-and-correction.md) | 拒绝、纠正与重复升级 | | 设计失败路径 |
| [0057](0057-packages-and-assembly.md) | 四层包、功能块与唯一一份依赖描述 | 原 0013、0056 | 划分包、改依赖 |
| [0060](0060-first-real-delivery.md) | 第一次交付：楼宇供暖；MES 为第一探针 | 原 0052 | 判断"要不要做" |
| [0061](0061-pluggable-protocol-packages.md) | 可插拔协议包：驱动、网关与接入 | | 加协议 |
| [0064](0064-architecture-subtraction-and-code-organization.md) | 架构减法、代码职责与**复杂度预算** | 原 0062 | 删代码之前 |

## 文档预算

ADR 的成本和代码一起被 `make budget` 检查：

- **一份 ADR 只回答一个问题。** 想写第二个问题，改归属的那份，不要追加一节。
- **状态行 ≤ 2 行。** 需要解释自己为什么还有效，说明该合并。
- **新 ADR ≤ 150 行。** 超过的通常是一份设计文档，不是决定。
- **写完即更新本索引。** 没有索引行的 ADR 等于不存在。
- **合并即删文件。** 被并入的内容按"决定保留、过程删除"处理，原文与实施流水去 Git 查；不保留 tombstone，不维护第二份历史索引。
