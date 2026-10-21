# ADR 索引

64 份 ADR 不等于 64 个现行决定。这个文件是唯一的入口：**先在这里找到一份，再读那一份**，不要从头翻。

## 只读这 5 份就够开工

| 你要做什么 | 读 |
|---|---|
| 改产品形态、界面组织、构建者工作流 | [ADR-0018](0018-foundry-aligned-platform-experience.md) Shell 与门户 · [ADR-0047](0047-application-studio.md) 应用设计台 |
| 改业务语义（对象、动作、规则、授权） | [ADR-0039](0039-business-transaction.md) 业务事务 · [ADR-0040](0040-one-rule-language.md) 规则语言 · [ADR-0044](0044-one-authorization-engine.md) 授权 |
| 改提交、恢复、发布、运行 | [ADR-0037](0037-submission-assets-and-host-boundaries.md) 提交与资产 · [ADR-0010](0010-accepted-results-and-tenant-recovery.md) 结果与恢复 |
| 动任何东西之前看边界 | [ADR-0038 §9](0038-build-the-capability-to-build.md#9-不可动的边界) 的 16 条 |
| 现在做什么 | [WorkQueue](../WorkQueue.md)（不是 ADR） |

## 怎么读一份 ADR

1. **看状态行。** 它写明继承、局部取代、完全取代。状态行超过两行，说明这份 ADR 在替别的东西解释自己——那是要修的信号。
2. **看取代范围，不看编号。** 只有同一问题、同一适用范围，较新的决定才覆盖较旧的。编号大不代表整篇作废。
3. **看 As built。** "设计已接受 / 工程已实现 / 检查已通过 / 真实使用已验证"是四件事，不能互相推导。
4. **历史命令不作数。** ADR 里的"下一步先做 X"只记录当时的顺序；当前顺序唯一归 WorkQueue。

## 状态

- **现行**——今天仍指挥开发。
- **部分取代**——被点名的那一节失效，其余仍有效；正文相关位置已标注后继。
- **历史**——只解释"当时为什么这样"，不再指挥新开发；实现仍按现行 ADR 与代码。

## 全部索引

| # | 决定 | 状态 | 什么时候读 |
|---|---|---|---|
| [0001](0001-kernel-as-contract.md) | 内核是语言中立的契约，不含业务词汇 | 现行 | 改 `contract/` |
| [0002](0002-no-shared-edge-core-yet.md) | 暂不建共享 Rust 边缘核心 | 现行（不做） | 有人提议重写边缘 |
| [0003](0003-authority-migration-adopts-history.md) | 权威迁移完整采纳前任的历史 | 现行 | 迁移权威 |
| [0004](0004-outbound-effects.md) | 出站效果：意图入账、投递可恢复 | 现行 | 外部调用、Webhook |
| [0005](0005-meaning-languages-api-contract.md) | 业务语义、多语言与 API 契约的分层 | 现行 | 新增界面文案 |
| [0006](0006-decisions-across-apps.md) | 跨应用只走请求-应答；跨权威不原子 | 现行 | 想跨应用事务时 |
| [0007](0007-one-runtime-for-durable-work.md) | 持久工作只有一个运行时 | 现行 | 任务、调度、重试 |
| [0008](0008-ai-control-plane.md) | AI 治理控制平面：配额、评测、追踪 | 现行 | 模型接入与成本 |
| [0009](0009-derived-content-declares-its-sources.md) | 派生内容声明来源，权限随来源收窄 | 部分取代（授权合成口径→[0044](0044-one-authorization-engine.md)） | 知识、派生读 |
| [0010](0010-accepted-results-and-tenant-recovery.md) | 已接受结果与租户恢复 | 部分取代（PG 部署恢复安排→[0063](0063-postgresql-runtime-state.md)） | 改恢复逻辑 |
| [0011](0011-minimal-definition-release.md) | 定义的最小发布机制（候选/激活/绑定） | 部分取代（"只接受可选标量"→[0045](0045-data-and-definition-evolution.md)） | 改发布 |
| [0012](0012-first-kernel-proof.md) | K4 幂等性的有界形式化证明 | 现行 | 改内核幂等 |
| [0013](0013-capability-fabric.md) | 能力装配：原生 Block、Wasm 函数、统一流程 | 现行 | 加受控计算 |
| [0014](0014-platform-catalog.md) | Platform Catalog：体验规范与资产库 | 现行 | 改资产库 |
| [0015](0015-application-studio-fusion.md) | 应用设计台融合（1,257 行） | **历史**（顶层结构→[0018](0018-foundry-aligned-platform-experience.md)/[0019](0019-ide-workspaces.md)，算子图→[0040](0040-one-rule-language.md)） | 只查当年取舍 |
| [0016](0016-platform-composition-and-workspaces.md) | 平台功能架构、入口与前端收敛 | 部分取代（§5–§6、§9 入口清单→[0018](0018-foundry-aligned-platform-experience.md)） | 查功能域与术语 |
| [0017](0017-delivery-and-lightweight-profiles.md) | 交付 profile 与轻量 profile | 现行 | 部署形态 |
| [0018](0018-foundry-aligned-platform-experience.md) | Shell、应用门户、菜单与功能块 | 现行 | 改界面结构 |
| [0019](0019-ide-workspaces.md) | IDE 式工作区与 Shell 页签模型 | 部分取代（构建者内部导航→[0047](0047-application-studio.md)） | 改页签、资产库 |
| [0020](0020-shared-ontology.md) | 共享本体：core 主数据、Interface、扩展 | 部分取代（扩展冲突语义→[0045](0045-data-and-definition-evolution.md)） | 改共享对象 |
| [0021](0021-handheld-scan-print.md) | 手持、扫码与打印 | 现行 | 现场设备界面 |
| [0022](0022-enterprise-modeling-layer.md) | 企业模型：UAF 词汇、组织投影 | 现行（有界） | 改组织/人员 |
| [0023](0023-connections-and-source-profiles.md) | 连接与四种 Source profile | 现行 | 接数据源 |
| [0024](0024-datasets-and-pipelines.md) | 数据集、管道、匹配、分析、回写 | 现行 | 改集成链路 |
| [0025](0025-markings.md) | Markings（有界）与 Integrator 角色 | 现行 | 字段级密级 |
| [0026](0026-business-core.md) | 账簿、单据编号、冲销 | 部分取代（动作/编号表达→[0039](0039-business-transaction.md)） | 会计语义 |
| [0027](0027-tenancy-and-authorization.md) | 租户隔离、设置与权限管理 | 部分取代（授权判定→[0044](0044-one-authorization-engine.md)） | 改租户/角色 |
| [0028](0028-user-domain.md) | 用户域：成员、档案、偏好、生命周期 | 现行 | 改登录成员 |
| [0029](0029-build-layers.md) | 构建分层：本地 / 提交前 / 发布 | 现行 | 改 Makefile |
| [0030](0030-tests-that-earn-their-keep.md) | 测试只留长期不变量 | 部分取代（固定八条浏览器路线→[0062](0062-platform-consolidation.md)） | 删/加测试 |
| [0031](0031-view-identity-and-one-drawing-across-modules.md) | 视图有身份，一张图能画几个模块 | 现行 | 改画布模型 |
| [0032](0032-mes-craft-master-data.md) | 工艺主数据：路线可复用、工序号即身份 | 现行 | MES 验证场景 |
| [0033](0033-a-node-type-system.md) | 画布的节点类型词汇 | 现行 | 改画布 |
| [0034](0034-sealed-standard-interfaces.md) | query / ref / write 三类封存接口 | 现行 | 改企业模型 SDK |
| [0035](0035-elk-canvas-layout.md) | 树用整洁排布，ELK 只排非树与流程 | 现行（待验收） | 改布局 |
| [0036](0036-unified-form-input-and-inspector-editing.md) | 统一输入：业务表单、动作参数、Inspector | 现行 | 改表单/属性面板 |
| [0037](0037-submission-assets-and-host-boundaries.md) | 提交契约、资产生命周期、宿主角色收敛 | 现行 | 改写入路径、宿主 |
| [0038](0038-build-the-capability-to-build.md) | 产品方向 + **16 条不可动边界** | 部分取代（首次交付对象→[0060](0060-first-real-delivery.md)） | **动任何东西之前** |
| [0039](0039-business-transaction.md) | 业务事务：单一权威内的原子变更 | 现行 | 写动作 |
| [0040](0040-one-rule-language.md) | 一门规则语言（条件、公式、决策表） | 现行 | 写规则 |
| [0041](0041-business-constraints.md) | 业务约束：模型自带的不变式 | 现行 | 写校验 |
| [0042](0042-business-version-and-time.md) | 业务版本与业务时间 | 现行 | 主数据随时间变化 |
| [0043](0043-record-sets-and-relations.md) | 记录集与关联，集合是一等公民 | 现行 | 改关系 |
| [0044](0044-one-authorization-engine.md) | 一个授权引擎：四层合成一个决定 | 现行 | 改权限 |
| [0045](0045-data-and-definition-evolution.md) | 数据与定义的演进、迁移 | 现行 | 改已发布定义 |
| [0046](0046-application-artifact-and-runtime.md) | 应用工件与运行形态 | 现行 | 改应用交付 |
| [0047](0047-application-studio.md) | 应用设计台：围绕一个应用构建 | 现行 | 改构建者界面 |
| [0048](0048-model-driven-ui.md) | 模型驱动的界面构造 | 现行 | 加组件 |
| [0049](0049-professional-components.md) | 专业业务组件：企业级表达 | 现行 | 加复杂组件 |
| [0050](0050-builder-verification.md) | 构建者验证：用真实样本证明能跑 | 现行 | 改试跑 |
| [0051](0051-cross-system-coordination.md) | 跨系统协同：确认、补偿、对账 | 现行 | 对接外部系统 |
| [0052](0052-mes-baseline.md) | MES 八个范围与反例清单 | 部分取代（验收对象→[0060](0060-first-real-delivery.md)） | 第二探针 |
| [0053](0053-retiring-native-apps.md) | 原生参考应用的退出条件 | 现行 | 想删 `apps/*` 时 |
| [0054](0054-ai-and-human-in-applications.md) | AI 提议、检查点与人工接管 | 现行 | 做 AI 功能 |
| [0055](0055-refusal-and-correction.md) | 拒绝、异常与纠正 | 现行 | 设计失败路径 |
| [0056](0056-blocks-and-dependencies.md) | 功能块衔接与依赖真理 | 现行 | 改依赖图 |
| [0057](0057-packages-and-assembly.md) | 包的四层：平台/能力/行业/应用 | 现行 | 划分包 |
| [0058](0058-observations-and-timeseries.md) | 观测与时序：外部存储，只取结果 | 部分取代（协议范围→[0061](0061-pluggable-protocol-packages.md)） | 接时序数据 |
| [0059](0059-device-dispatch.md) | 设备与黑盒目标：派发、回读、人工介入 | 部分取代（协议补充→[0061](0061-pluggable-protocol-packages.md)） | 对接设备 |
| [0060](0060-first-real-delivery.md) | 第一次真实交付：楼宇供暖 | 现行 | 判断"要不要做" |
| [0061](0061-pluggable-protocol-packages.md) | 可插拔协议包：驱动、网关、接入 | 现行 | 加协议 |
| [0062](0062-platform-consolidation.md) | 平台整体收敛与**文档分工** | 部分取代（减法细节→[0064](0064-architecture-subtraction-and-code-organization.md)） | 想知道该写在哪 |
| [0063](0063-postgresql-runtime-state.md) | PostgreSQL 作为正式运行状态与兼容恢复 | 现行 | 改存储 |
| [0064](0064-architecture-subtraction-and-code-organization.md) | 架构减法、代码职责与**复杂度预算** | 现行（当前主线） | 删代码之前 |

## ADR 预算

新增一份 ADR 的成本很高，所以有硬上限，和代码一起被 `make budget` 检查：

- **一份 ADR 只回答一个问题。** 想写第二个问题，改归属的那份，不要追加一节。
- **状态行 ≤ 2 行。** 需要解释自己为什么还有效，说明该合并或该退场。
- **新 ADR ≤ 150 行。** 超过的，通常是一份设计文档，不是决定。
- **写完即更新本索引。** 没有索引行的 ADR 等于不存在。

合并、取代、退场时：旧文件保留（历史要靠它解释），状态行写明被谁取代，本索引改状态。
