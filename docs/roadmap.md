---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

## 里程碑

从现在到终局（[1-enterprise §1.1](1-enterprise.md)）的四个里程碑，按顺序。这里只写成立的标准；条目只为当前里程碑排期，后面的里程碑到达时再拆。

| 里程碑 | 终局阶段 | 成立的标准 |
|---|---|---|
| 收敛 | 平台成形 | “当前批次”第一至三段的完成标准全部成立 |
| 自举 | 行业应用 | “当前批次”第四段完成：`deploy/packages/` 全部是 DevOps 导出的产品版本，系统产品包随宿主预装 |
| 可交付 | 交付运营 | F 系列全部在 PostgreSQL 上通过；“安装 v1 → 客户扩展 → 升级 v2”演练不阻断；备份恢复演练只应用已接受映像；格式纪元冻结（C26） |
| 首个客户 | 交付运营 | 一个客户租户安装一个行业应用，并在保留扩展的前提下升级一次 |

代理的自治级别随里程碑提高，级别与前提见 [method 自治阶梯](method.md#自治阶梯)：收敛期间补齐 2 级的前提，自举完成时具备 4 级的前提。

## 当前批次

本轮是结构收敛：内核回到 `kernel`、领域只留一套模型、平台开始自举。四段按顺序做，前一段的完成标准成立才进入下一段。收敛期间产品矩阵冻结（[2-product §2.2](2-product.md)）：不新增工具应用与资源类型，只修缺陷。每条完成时 `make check` 通过，K1、K2、K3、K13 的断言不改写（删除纪元前断言除外）。

### 一、格式纪元

目标规范：[7-system §7.6–7.7](7-system/README.md)、[7-system/verification §7.8](7-system/verification.md)、[6-domain R9](6-domain/README.md)、[1-enterprise §1.2](1-enterprise.md)。形状已由棘轮锁住（`scripts/shape-baseline.json`，每条完成时把基线调低）；本段删掉为不存在的历史保留的路径，让后面要搬的代码变少。


### 二、内核回到 kernel

目标规范：[7-system §7.2–7.3](7-system/README.md)、[concurrency](7-system/concurrency.md)。完成标准（整段）：`host` 不超过 1.5 万行且没有 `acceptance_*.go`；`kernel` 不导入任何服务；K1、K2、K3、K13（含 `-race`）不改断言通过。

- [ ] N3 一个拥有者接口：决定、应用、分叉、读取四个方法；分叉已是 `platform.App.Fork(ledger)`（读取映像传入分叉账本，暂存决定与安装传入原账本），快照、恢复与快照格式已是 `platform.App` 的方法；`AcceptedStateApp`（4 个拥有者的状态映像，应用与恢复语义不同，如 Console 应用时结束停用成员的会话）与 `AcceptedInputApp`（只有 AI）是少数拥有者的真实能力，不给其余拥有者写空实现，改为注册时按类型登记；两种运行时都实现的扩展已并入 `platform.Runtime`，只由暂存决定实现的能力已是 `platform.StagedDecision` 句柄；拥有者角色（身份、目录、链接、模型、任务、流程、智能体、术语、观察者、监听）已由 `host/owner_roles.go` 按启用的拥有者一次登记；余下 `DecisionDeclarationRuntime`（只有保留发布执行覆盖）与约 247 处探测（多为服务与宿主之间的可选接口）继续按注册时类型收敛；`AcceptedStateApp`、`AcceptedInputApp` 与资产生命周期拥有者的登记要等读取映像与暂存视图不再逐个替换 `apps` 才能持有实例。内核只有一个实现，`KernelFactory` 与其七个接口已删除，Ledger 直接持有 `contract/go/kernel` 的具体类型。服务不再自建 Ledger：`platform/ledger.go` 的接受语义与 `contract/go/kernel` 的实现并入 `kernel`，`Error` 等值类型进 `platform`，`contract/go` 只留协议生成代码。各服务经此接口持有自己的状态：`host.Tenant` 余下的 65 个字段（副作用队列 `outbound` 与 `endpoints` 及与工作板、配额共用的 `opsMu`，各服务的拥有者绑定与运行状态）随各服务实现该接口移出；之后读取映像不再复制整个 `Tenant`，随之进 `kernel.Sequence`。
- [ ] N4 一个提交入口：`submit` 五层合为一个函数加选项结构；幂等只留映像预检与车道内提交两处；`quarantine` 只留应用不一致与位置不一致两处，其余改为拒绝或运行失败。
- [ ] N5 接受路径迁入 `kernel`：`host/acceptance_*.go` 中属于接受语义的部分进 `kernel`，属于某类资源的部分进它的服务；`host` 只剩 HTTP、身份、租户装配与后台调度。

### 三、领域模型收敛

目标规范：[6-domain R8](6-domain/README.md)、[7-system §7.2、§7.5.1、§7.7](7-system/README.md)。完成标准（整段）：`platform` 不超过 8 千行且只含 SDK 契约；7-system 的十条依赖差距清零；包名与目录名不一致为 0。

- [ ] U1 动作引擎归位：`platform/ledger.go` 的规则引擎与 `entity.go` 的转换移入 `services/actions`，按动作类型的规则与提交条件表达；`Entity`、`Transition`、`Ledger` 不再是公开类型。
- [ ] U2 业务事务出平台：`platform/transaction.go` 的 `BusinessTxn`、`TxnPost`、`TxnJournal` 改由产品包的本体与动作表达，平台只留通用的原子编辑集。
- [ ] U3 页面模型归位：`platform/page_*` 移入 `services/apps`；`platform` 只留页面组件契约的生成入口。
- [ ] U4 发布路径一张表：record 族与 versioned 族的发布步骤移入拥有它的服务并实现同一组钩子；`services/resources` 只编排，删除 Build 的“兼容公共类型”。
- [ ] U5 拆散 `build`：`services/resources/authoring` 中各资源类型的编辑移入拥有它的服务；`resources` 只留 RID、项目、文件夹、工作状态、分支、提案。
- [ ] U6 改名：16 个包改为目录名；内核与服务的公开标识改用术语表的词，`candidate` 只留给发布候选。纯改名，单独成批，不夹带行为改动。

### 四、自举

目标规范：[1-enterprise §1.1](1-enterprise.md)、[2-product R8](2-product.md)、[verification K8](7-system/verification.md)。前三段完成后解除产品矩阵冻结。

- [ ] S1 自举一级：在构建租户里用 Ontology Manager、Workshop、Logic、DevOps 重新产出 heating、hospitality、manufacturing 三线产品包并发布版本，替换手工维护的 JSON。做不出来的地方登记为平台差距，不在 JSON 里绕过。完成标准：`deploy/packages/` 的每个文件都是 DevOps 导出的产品版本。
- [ ] S2 自举二级：工作项、审批、通知收件箱、监控视图改为系统产品包并随宿主预装，删除对应 Go 与手写前端；K8 改为预装断言。逐个做，每个先在包里做到与现有界面等价再删代码。

## 条件触发

目标规范见 [6-domain/packages](6-domain/packages.md)。以下能力等待真实需求，不作为本轮待交付工作。

- C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。
- C26 第一个客户租户装载前冻结格式纪元：此后任何格式变化只经版本化迁移（[7-system §7.6](7-system/README.md)）。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- 审批退役与重定向是死路径（6-domain）：`platform.Caller.CheckApprovalRetirement` 没有运行时实现，`work.approval.retire` 总被拒绝，因此只由它产生的 `redirected` 审批、`services/work/redirect.go` 的重定向作业与 `ApprovalRequest` 的重定向字段从不生效（实现随旧租户迁移机制一并删除）。删除 `services/work/retirement.go`、`redirect.go`、`ApprovalRetirementOwner`、`ApprovalRetirementRuntime` 与相关字段、状态和文案。
- “隔离”一词指三件事：租户数据隔离、租户因不一致转为只读、函数的隔离计算；规范与界面文案尚未分开命名。
