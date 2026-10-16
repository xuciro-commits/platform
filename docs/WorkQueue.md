# 执行队列

只放当前要交付的工作。产品口径、依赖与不可动的边界归 [ADR-0098](ADR/0098-build-the-capability-to-build.md)；第一次交付的判据归 [ADR-0122](ADR/0122-first-real-delivery.md)；既有 SRS/SOW 的结构整改承诺已由 [ADR-0097](ADR/0097-submission-assets-and-host-boundaries.md) 收口；能力现状归 [Platform](Platform.md)。本队列不复制这些内容，不保存完成记录。

## 当前：第一次真实交付（楼宇供暖）

当前仍是开发阶段。范围包括门禁，数据分发包括推送、看板报表与导出/API。平台工程先完成，真实使用不作为继续开发的前置条件。

楼宇供暖、暖通、门禁、数据分发、健康诊断和 AI 的现行范围归 [ADR-0122](ADR/0122-first-real-delivery.md)。系统上线、行业资产和缺口清单仍是同一次交付的三件产出。具体楼栋、现场设备、物业角色和保留目标在接入时补齐；资料中的旧范围不撤销负责人已确认的门禁要求。

`industry.heating`、两份模拟配置和 Modbus 点表接入用于开发验证；客户实际上线、客户变更与第二城市记录保持未验证。负责人使用与独立构建者体验各按原 ADR 记录，不用模拟或自动测试冒充。规模优化先按确定的目标密度测量，原生应用按 ADR-0114 在等价替代和历史责任明确后退出。

两宿主酒店 `8495`、制造 `8490` 的版本、健康、页面资源和原数据卷随 main 更新维护，不另作为交付块。

## 之后：构建业务应用的能力（ADR-0098）

方向与边界归 [ADR-0098](ADR/0098-build-the-capability-to-build.md)（含 §9 不可动的边界、§12 交付指引），依赖关系与关键路径归该 ADR §8。**档位是替换不是增量**（ADR-0100 §1.5）：新能力落地时同批冻结并登记退出它取代的旧表达，不保留两套规则。

**第二探针与第二次交付**（[ADR-0122 §5](ADR/0122-first-real-delivery.md)）：

- **MES 第二探针**：[ADR-0113](ADR/0113-mes-baseline.md) 的八个范围与反例清单，用于域中立性的第二证据。
- **第二次交付**（候选：大疆的 MES + WMS）：**可能是别人的领域**，可能不由本平台完成；它证明**武器库可移交**，不发生则记为未验证，不作验收要求。

**ADR-0100 起的能力块按依赖表推进**（[ADR-0098 §8](ADR/0098-build-the-capability-to-build.md#8-后续-adr-与依赖顺序)），只保留当前施工块。已交付能力的实现与证据边界归各自 As built，不另占队列位。完整业务事务与旧动作替换的实现、完整单据和恢复证据归 [ADR-0100 As built](ADR/0100-business-transaction.md#6-as-built--完整业务事务)；通用规则编辑器已由 ADR-0109 接入，不再作为未完成施工项。统一 Go 协议包、独立网关、模拟接入与 S7 本地报文读取的实现及边界归 [ADR-0123 As built](ADR/0123-pluggable-protocol-packages.md#11-as-built)；ThingsBoard Gateway、国产网关与通信库是研究参考，未选为必须部署的运行框架。真实上线、行业包与客户判据保持原停止条件。

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

统一依赖描述、能力声明、反向查询、能力发现与旧实现替换的工程结果归 [ADR-0118 As built](ADR/0118-blocks-and-dependencies.md#as-built)。外部观测读取、窗口规则、结果入账、自动判定和原始点不入日志的实现/证据归 [ADR-0120 As built](ADR/0120-observations-and-timeseries.md#as-built)，真实采集器与 TSDB 仍是部署前置条件。设备派发、状态回读、人工处理与不安全指令不自动重发的工程结果归 [ADR-0121 As built](ADR/0121-device-dispatch.md#as-built)，现场控制器与具体协议仍须真实环境接入。业务拒绝、纠正历史、重复升级与暂停裁决的实现和检查范围归 [ADR-0117 As built](ADR/0117-refusal-and-correction.md#5-as-built)。跨系统多方确认、未知结果、源回读、补偿与恢复的实现/证据边界归 [ADR-0112 As built](ADR/0112-cross-system-coordination.md#5-as-built)，不建立跨权威事务。AI 提议、普通事务采纳/审批、接管、应用用量与候选评测归 [ADR-0116 As built](ADR/0116-ai-and-human-in-applications.md#5-as-built)。构建者草稿试跑、版本化合成样本、多成员对照与原测试计划的实现和隔离 fixture 边界归 [ADR-0111 As built](ADR/0111-builder-verification.md#5-as-built)。专业图、矩阵和工作站配置的工程边界与独立作者/负责人验收归 [ADR-0110 As built](ADR/0110-professional-components.md#5-as-built)，原生 MES 仍保留。模型绑定与应用工作区现状归 ADR-0109 / ADR-0108；负责人体验验收保持待验证，不占工程施工项。应用工件与运行/恢复证据归 ADR-0107。

## 收口规则

- 原 W0 全平台盘点、独立 W5 清理阶段和 XXL/L 编号移出队列；必要核查和删除由对应代码改动一并完成。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、本轮协议包之外的更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0047 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
