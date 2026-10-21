# 执行队列

只放当前要交付的工作。整个项目的收敛范围与文档分工归 [ADR-0062](ADR/0062-platform-consolidation.md)；产品宗旨归 [Intent](Intent.md)，应用构建方向与边界归 [ADR-0038](ADR/0038-build-the-capability-to-build.md)，第一次真实交付归 [ADR-0060](ADR/0060-first-real-delivery.md)，能力现状归 [Platform](Platform.md)。本队列不复制各 ADR 的实现记录，不保存完成记录。

## 当前主线：XXXL 重构（总纲见 ADR-0064 §10）

每一块按"量基线 → 合并实现 → 迁移调用方 → 删旧路径 → 检查 → 预算锁数字"走完，**做完才移出**。块内不新增 ADR，范围只在本队列与 ADR-0064 §10 各写一次。

复杂度预算已生效：`make budget`（含于 `make check`）守入口、菜单项、ADR 份数与文档字数，只减不增；**新增任何入口、菜单项或文档前，先删等量**。

| 块 | 目标 | 接受标准 | 起手证据 |
|---|---|---|---|
| **X1 一个应用一处身份** | `data-connection`、`agents` 由两个包各声明一次，运行期按 id 合并；构建者看到 13 项 / 4 组，且「Connections」（数据连接）与「Connectors」（事件连接器）并列 | 身份字段只声明一次，另一包只贡献视图与菜单；两个连接概念不再同名；`workspaces.spec.ts:217` 同批更新；`ui.definitions` 降到 18 | `web/packages/build/src/index.tsx:128`、`web/packages/platform/src/index.tsx:125` |
| **X2 构建者工作台分职责** | `web/packages/build/src/workshop/module-import/compile.ts` 882 行、`workshop/editor.tsx` 718 行混合解析、映射、诊断与渲染 | 编译与诊断可单测，不经 React；编辑器的草稿/撤销仍只有一份 | `compile.test.mjs` 已存在，可作回归基线 |
| **X3 外壳与记录视图** | `ui/shell/Workspace.tsx` 935 行（Rail、页签、布局持久化、命令面板、未保存保护）；`ui/records/Records.tsx` 865 行 | 每个职责一个模块；`Workspace.test.tsx`（377 行）不改断言即通过 | 现有测试即 oracle |
| **X4 宿主平铺包分组** | `capabilities/server` 423 个文件同层（10 万行） | 按协调职责分组（接受、记录、发布、运行、协议），先搬再改；`boundaries.sh` 与 `escapes.sh` 不变松 | `doc.go` 的组件地图即目标分组 |
| **X6 死代码与死词条** | 已删 526 条无引用词条（本轮）；剩余短词条需与动态 `t(变量)` 现场逐一核对 | 删除前给出该词条的唯一可达路径或确认不可达 | 本轮脚本口径：21+ 字符且全仓无字面量 |
| **X7 前端性能** | 首页排序已改预计算名次；其余热点：记录表全量重渲、`useRead` 轮询粒度、画布布局重算 | 有前后测量才声称改善，否则写"未测量" | `ui/records`、`graph/*/layout` |
| **X8 E 的交付验证** | PostgreSQL 按需工作集代码已实现，完整交付验证未完成 | 真实 PG 上的整块恢复与两宿主演练 | ADR-0063、ADR-0064 §8 |

X1 与 X6 有用户可见收益，优先；X2–X4 是结构性减量主体；X8 是既有承诺，不能被新块挤掉。

**X5 已移出（2026-10-20，后端同批）**：`apps/build/public.go` 249 → 23 行。原文件 122 条别名里，只有 13 个名字被模块外的 `solutions/*` 读取（`AppType`、`ID`、`ObjectType`、`PageType`、`Builder`、`User`、`SchemaPublish`、`SchemaRelease`、`SchemaHandOver`、`SchemaFunctionCall`、`TestPlan`、`TestPlanType`、`TestStep`），其余 109 条在本仓无任何引用；模块内 255 个文件本来就直接用 `internal/construction`。同批修掉两个后端实际缺陷：`tenantLane` 在 `submit` 持有期间被回调读重入会自锁（`TestAcceptedRelationGraphIsPrivateUntilCommit` 因此在 main 上挂满 10 分钟），以及 `joint_draft_test.go` 把"一次流程运行 = 一行 `flow.instance`"写死（实际一次运行留下根实例与步骤实例，共 3 行）。预算新增四个总量指标：`code.goProduction`（138,529 行）、`code.goFiles`（694）、`code.tsProduction`（56,974 行）、`code.tsFiles`（637），前后端手写生产代码从此只能变小。

**Go 侧在本沙箱可完整验证**（此前"只能编译 `internal/acceptance`"的限制已解除）：`.build/sandbox-go/go.work` 用 65 条 `replace` 指向 `.build/sandbox-go/replace/*` 的 GitHub 源码包，配 `GOPROXY=off` 后 `go build ./...`、`go vet ./...` 在 12 个模块（capabilities、contract、6 个 apps、2 个 protocols、2 个 solutions）上全部通过，`go test -count=1 ./...` 在 capabilities（根包 516 个测试，1,464 s）、两个 solutions 与 crm/csm/erp/hcm 通过；`pms` 7 个、`mes` 1 个用例在 main 上即失败（与本轮改动无关，`git stash` 后同样复现），未计入通过。该目录只在本机存在，不进 Git；Go 侧删除仍以这三条命令的结果为准。

## 保留交付：MES 工单旧界面退出

负责人确认通用页面与原工单界面等价后，删除 `ShopOrders`、专用表单及活动渲染注册；旧链接继续由已发布的通用替换映射解析。保留 MES 数据、原动作、权限、草稿及历史读取，不以自动浏览器检查代替负责人确认。

技术转型已实现部分与实际限制分别归 ADR-0063、ADR-0054、ADR-0048、ADR-0037、ADR-0053；已完成的工程项不再占队列。真实 FDE 三项任务的人工耗时比较、真实模型与生产容量未观察时继续明确标注，不用自动测试时长替代。

## 保留交付：楼宇供暖与既有平台整理

围绕现有供暖应用的构建、发布、运行和维护任务修复实际阻碍与重复职责，不横向增加资产种类或新的工作台。已完成的整理归各 ADR 的 As built；不把文件搬迁或检查通过当作全平台整理完成。

当前仍是开发阶段。范围包括门禁，数据分发包括推送、看板报表与导出/API。平台工程先完成，真实使用不作为继续开发的前置条件。

楼宇供暖、暖通、门禁、数据分发、健康诊断和 AI 的现行范围归 [ADR-0060](ADR/0060-first-real-delivery.md)。系统上线、行业资产和缺口清单仍是同一次交付的三件产出。具体楼栋、现场设备、物业角色和保留目标在接入时补齐；资料中的旧范围不撤销负责人已确认的门禁要求。

`industry.heating`、两份模拟配置和 Modbus 点表接入用于开发验证；客户实际上线、客户变更与第二城市记录保持未验证。负责人使用与独立构建者体验各按原 ADR 记录，不用模拟或自动测试冒充。规模优化先按确定的目标密度测量，原生应用按 ADR-0053 依实际使用与已承诺范围退出，保留必要历史。

两宿主酒店 `8495`、制造 `8490` 的版本、健康、页面资源和原数据卷随 main 更新维护，不另作为交付块。

## 后续交付与既有能力

应用构建方向与边界归 [ADR-0038](ADR/0038-build-the-capability-to-build.md)（含 §9 不可动的边界），依赖归该 ADR §8。整个项目收敛按 ADR-0062 推进。**替换不是增量**（ADR-0039 §1.5）：同批处理被替代的普通执行路径，不保留两套规则。

**第二探针与第二次交付**（[ADR-0060 §5](ADR/0060-first-real-delivery.md)）：

- **MES 第二探针**：[ADR-0052](ADR/0052-mes-baseline.md) 的八个范围与反例清单，用于域中立性的第二证据。
- **第二次交付**（候选：大疆的 MES + WMS）：**可能是别人的领域**，可能不由本平台完成；它证明**武器库可移交**，不发生则记为未验证，不作验收要求。

已交付能力的实现与证据边界归各自 As built，主题入口见 [ADR-0062 §1.2](ADR/0062-platform-consolidation.md#12-现行主题入口)，不另占队列位。真实上线、行业包与客户判据保持 ADR-0060 的停止条件；既有 SRS/SOW 承诺与 ADR-0037 的实现边界继续保留。

每次只推进一块；块内按实际依赖迁移。已经通过且未受影响的证据直接复用。检查按 [Testing](Testing.md) 选择，代码合入 main 后沿既有同步协议更新两宿主，保留原数据卷。

## 队列规则

- 旧 W0/W5 与 XXL/L 编号不恢复为另一套队列；整个项目收敛以 ADR-0062 为范围，本次负责人确认的架构清理主线按 ADR-0064 的职责与退出条件实施，顺序仅在本队列维护。
- 已完成、被替代、仅待一般体验走查的旧条目不再作为工程欠账；确有阻断时修复当前任务，不另起专项。
- XMI/UAF/EPC 深度、画布新形态、跨设备排布、本轮协议包之外的更多连接器、行业交易/排产、插件市场、第二数据库及更多形式化证明不在当前施工范围。ADR-0064 的 UAF 标准解析移到生成阶段属于现有语义整理，不增加 XMI 实例导入或扩展标准语义。现有支持边界留在原 ADR，新需求到来再决定是否排队。
- 外部分支持续 Flow 候选不自动成为集成任务；当前七节点实现与边界修复已在 main，剩余验收以 ADR-0016 / SRS 为准。
- SRS 已承诺的任务与历史/权限/恢复保证保留；登记未来可能性不增加实施范围。只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
