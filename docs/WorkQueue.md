# 执行队列

只维护近期顺序与必要待办。宗旨见 [Intent](Intent.md)，目标见 [Platform §10](Platform.md#10-未来方向)，实现边界见对应 ADR；批次/检查规则见 [AGENTS](../AGENTS.md) 与 [Testing](Testing.md#检查选择与停止)。完成项移除，不记录提交过程与检查历史。

## 当前主线：整个平台的构建与操作体验

统一能力装配与 Platform Catalog 已形成工程入口，范围归 [ADR-0044 §12](ADR/0044-capability-fabric.md#12-实际构建边界) 与 [ADR-0045 §11](ADR/0045-platform-catalog.md#11-当前实现边界)。沿工坊→页面/流程→测试/发布→操作任务验证最大共享体验断点。WMS 是独立的受控定义装配验证应用，不新增行业专用源码应用。

收货探针已验证对象、关联、动作、Go/Wasm 和审批，用法归 [Apps](Apps.md#wms-受控装配探针)。应用设计台融合设计归 [ADR-0046](ADR/0046-application-studio-fusion.md)，外包编辑体验为目标，平台语义与 Go 执行保持统一。负责人已接受 [ADR-0047](ADR/0047-platform-composition-and-workspaces.md) 的功能架构与入口/组织取代原则，M0–M4、M6 已按 §10.2 收敛：M1 入口与 M2 应用与资源构建上下文（应用内打开页面/对象/逻辑并返回应用，共享资源库按获权、未被引用与后台自动化分视图，重复资产类型导航已删除）；M3 联合草稿候选与直接安装退场按 [ADR-0048](ADR/0048-joint-draft-candidates-and-direct-install-retirement.md) 落地（显式勾选集合与依赖查询、缺依赖列名阻塞、草稿来源/差异、原封存与原激活、production profile 拒绝直接安装旁路、完整 Module 导入与其预发布对象都改联合草稿）；M4 在固定持久环境上取得诊断与续接证据（真实 PostgreSQL 写入、快照重启续接、整套 Go 测试全绿；无 Docker 时的交付断言由 [deploy/local/rehearse-lite.sh](../deploy/local/rehearse-lite.sh) 的 walk/verify/backup 承担，Docker 专属部分仍归 [deploy/local/rehearse.sh](../deploy/local/rehearse.sh)），负责人明确免掉界面走查；M6 只留规范路径，地址译码支持政策已声明（原 view ID 与旧书签继续解析，退役 view 显式反馈）。M5（受控安装/升级、复合编辑、持续 Flow）按负责人指示一并落地：受控包安装/升级/排空/退役与命名空间贡献、候选封存与跨环境晋级、真实记录迁移、复合编辑、宿主控制台 API（§6.5）与 §13 方案 A 的批次 frame/每次调用通道都已实现并有测试（`capabilities/server/packages.go`、`environment.go`、`composite.go`、`hostadmin.go`、`apps/flow/continuous.go`、`compute_channel.go`），13 个 Go 模块本地全绿。平台 Review 的 AI 专项按 [ADR-0050](ADR/0050-model-accounting-and-run-scope.md) 落地并附复现测试，平台侧的"逐入口陈述保证、不宣称全平台恰好一次"与留给负责人的边界按 [ADR-0051](ADR/0051-entry-level-persistence-guarantees.md) 定案。基础设施的交付/轻量两种 profile 按 [ADR-0049](ADR/0049-delivery-and-lightweight-profiles.md) 完成 S1–S3（`Journals` 接口与单文件日志、本地文件字节、内置轻量 IdP；同一套契约测试在文件与真实 PostgreSQL 上通过，无 Docker 的单目录重启走查归 [deploy/local/rehearse-lightweight.sh](../deploy/local/rehearse-lightweight.sh) 的 walk/verify/backup）；S4（SQLite）按 D2 明确为后继后端（接口已就绪，等依赖方案）。ADR-0048 M3、ADR-0050 与 ADR-0051 已有实现；ADR-0047 的入口收口仍须以实际任务路线核对，不能由后端测试推定前端全部可用。原 F1–F6 缺口及以下交付事项保留。每次只推进一个活动批次，完成后移除该行；先修当前任务的共性阻塞，不追加 WMS 行业深度。

| 顺序 / 优先级 / 状态 | 批次与归属 | 可见结果 / 停止条件 | 适用检查 |
|---|---|---|---|
| 1 / P1 / 待执行 | #134：首个可配置数据接入；集成 owner、K8 宿主、工坊 | 构建者配置一个外部 JSON 数据源及字段映射，先预览/校验再沿原输入路径接入；重试不重复写入，失败可定位。只支持一个有界接入 profile，不铺连接器市场 | capabilities、composition、format、web；输入/游标重放 |
| 2 / P2 / 待执行 | #138、#132：通用表单条件联动；语义/页面/共享表单规范主人 | 用非收货的通用申请表，按类型切换一组字段的显示/必填；权限归原表单和动作，切换及拒绝保留输入。只支持一个有界条件，不新增租户脚本或行业审批逻辑 | capabilities、format、web；发布及必填校验 |

负责人已认可此前集中走查与 Go/Wasm 实例，不重新要求整体验收。沿可操作任务及必要检查推进；不回到模型评测、计算崩溃分支或底层证明的无限加固，不另开第二队列。

## 后续与暂缓

| 工作号 | 剩余范围 / 启动条件 |
|---|---|
| #141 | ADR-0047 的 M5 前端任务面仍缺：Publisher/Auditor 与项目委派入口、包安装/升级/排空/退役、环境晋级/迁移及独立宿主控制台。后端 API 已有，不能把 Go 测试通过写成界面任务完成；沿实际构建/交付断点分批接入，不新增执行主人 |
| #138、#132 | 多级具名选择、物料参数绑定及原动作的跨对象条件已有，用法归 Apps。单次预计量校验不等于累计核算；累计收货量/父单完结与受控聚合写入仍缺，当前不能保证累计超收限制或库存台账。审批候选的隔离 Work fixture、独立关系基数/删除语义及更广原子编辑按实际任务取必要部分 |
| #136、#131 | 整应用资源与发布、显式可选标量升级已有，用法归 Apps 与 ADR-0039。其他操作读取/新动作发布标识、重命名/AI 绑定变化、退役、通用升级兼容及客户扩展保留（跨环境晋级与真实记录迁移已按 ADR-0047 §11 本批实现：`environment.go` 的封存/晋级/迁移；来源适配与灰度策略仍按运行验收边界另定） |
| #130、#128 | 权限一致的表单与独立业务审批角色用法归 Apps。新派生/预览面的权限闭包，触及时决定所有者隐私读取语义 |
| #135、F-44 | 未接入的 Chat/Agent、私有状态、旧入口/历史与正式升级结果恢复；对应能力触及时收口 |
| #133、F-29 | W2 的真实模型任务质量、更广调试及不可逆协议/效果审批；不重开已收尾首批函数增量 |
| #134、F-28 | 首个接入见近期批次；更广映射、身份对账、血缘及外部应答事件化 |
| #137 | 后续修订/引用/提交/恢复证明随对应能力推进 |
| #124 | MCP 标准鉴权发现与 Resources：`/.well-known/oauth-protected-resource`（RFC 9728）与未鉴权 401 `WWW-Authenticate`，业务记录/具名读取的 `record://`、`read://` 资源化；现有 `POST /mcp` 的成员目录 tools 与 Bearer 鉴权已具备 |
| #129 | MES 签署归属、MES/PMS Fact/Submit 与报表的已知逃逸，触及时消除 |
| #121、#115 | 下一次集中体验核对隐私/审批/导入导出与 ERP 采购链，不重开开发 |
| #125、#127 | 行业深度暂缓，只取平台探针必要变更 |
| F-22 | 严格跨日志因果审计需求出现时再启动内核变更 |
