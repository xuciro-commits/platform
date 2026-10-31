---
level: 路线图
covers: []
read-when: 每批开始（选下一批）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)；本表把它拆成五个阶段。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

## 阶段

| 阶段 | 目标 | 状态 |
|---|---|---|
| P1 对齐 | 产品结构、导航、资源页、工作状态、血缘、项目角色、标记按 Foundry 对齐 | 完成；遗留并入 P3 |
| P2 内核收敛 | 资源内核、单一实现、服务包边界、薄验收 | 进行中 |
| P3 构建者闭环 | 黄金路径 F1–F10 每一步达到 4-interaction 与 5-experience 的标准 | 与 P2 交错 |
| P4 行业应用 | 用平台工具重建行业产品包，作为平台的探针 | 未开始 |
| P5 交付运营 | 产品依赖、受管项目、客户扩展、升级审阅 | 未开始 |

## 下一批

P2.1 薄验收骨架：F1 黄金路径的 Playwright 用例 + 接受结果重放一致性的 Go 不变量测试；`make verify` 一条命令运行（见 7-system/verification）。

## P2 内核收敛

| 编号 | 事项 | 完成标准 | 规范 |
|---|---|---|---|
| P2.1 | 薄验收骨架 | 见“下一批” | 7-system/verification |
| P2.2 | 资源内核 | 所有资源有 RID、项目、文件夹、版本、状态；路由以 RID 为键；项目覆盖不再从页面列表推导 | 6-domain §6.3、3-information §3.6 |
| P2.3 | 动作类型独立资源 | 动作类型有自己的记录、版本与资源页；对象类型只引用 | 6-domain §6.7 |
| P2.4 | 工作项、通知、评论内置 | 工作项是内置对象类型，由动作创建与关闭；内置评论动作；`apps/work` 删除 | 6-domain §6.10 |
| P2.5 | Expr 包与注入 | `internal/rules` 成为 `expr` 包；`platform/field_policy.go`、`entity.go`、`definition.go`、`kernel/records/record_domain.go` 改为经可注入的规则引擎调用 | 6-domain §6.12 |
| P2.6 | 授权包边界 | 上下文构造（`Tenant.authorizeDepth`）与决定函数（`platform/authz`）一并移入 `services/security`；`platform` 只留接口 | 7-system §7.4 |
| P2.7 | AI 代码归并 | `aigateway`、`internal/aistate`、`internal/agents`、`engine/agents`、`apps/ai`、`platform/ai.go` 归入 ai 包 | 7-system §7.2 |
| P2.8 | 删除流程残留 | 以 Flow 命名的宿主文件与 `flow.instance` 视图删除；逻辑流只作为函数引擎 | 2-product R7 |
| P2.9 | 行业代码出宿主 | `apps/core/books.go`、根目录 `books.go` 的总账逻辑移出平台 | 2-product R4 |
| P2.10 | 内置业务应用归位 | Knowledge → Agent Studio 检索源；Master data → 产品包；Organisation directory → Control Panel Organization | 4-interaction/workbenches |
| P2.11 | 服务包迁移 | 宿主按 7-system §7.2 的目标包逐个迁移，每次迁移删除旧路径 | 7-system §7.2 |
| P2.12 | 大文件拆分 | `pageui/manifest.ts`（9k 行）、`build_integrations.go`（3.6k 行）等超过 7-system §7.7 限额的手写文件按职责拆分 | 7-system §7.7 |
| P2.13 | 重放只应用接受映像 | 重放不再重新执行 submission、input、effect、usage、agent、delivery、job 等旧条目；旧条目经版本化迁移转为接受映像 | 7-system/README R6 |

## P3 构建者闭环

| 编号 | 事项 | 规范 |
|---|---|---|
| P3.1 | 审阅并保存的字段级差异与逐项放弃 | 4-interaction M18 |
| P3.2 | 并发编辑冲突提示 | 4-interaction §4.5 |
| P3.3 | 侧窗可调宽；⌘⇧S、`?` 快捷键一览；⌘S 全覆盖 | 4-interaction §4.2、§4.9 |
| P3.4 | 侧栏收敛：Control Panel 8 项与独立 Markings 分区；Monitoring、Data Connection 去除旧项；项目页移除 Business roles | 4-interaction/workbenches、3-information R5 |
| P3.5 | Search 分组与前缀；门户按四组并隐藏无权应用 | 3-information §3.3、§3.8 |
| P3.6 | 文件夹后端模型与 Files 文件夹树 | 3-information §3.7 |
| P3.7 | Discoverer 与项目级读取 | 6-domain/security §6.14 |
| P3.8 | 专项标记、标记资源、解除标记步骤 | 6-domain/security §6.15 |
| P3.9 | 数据集事务类型、流、媒体集、调度 | 6-domain §6.6 |
| P3.10 | 自动化执行身份与暂停；告警并入自动化 | 6-domain §6.9 |
| P3.11 | 函数编辑提议；查询并入函数或对象集 | 6-domain §6.8 |
| P3.12 | 分支从 main 更新、合并批准规则 | 6-domain/change §6.20 |
| P3.13 | Workshop 对象视图模块替换默认对象视图 | 4-interaction §4.3 D |
| P3.14 | Workshop 组件目录统一记录类组件；Dialog 只留 480 与 880 两种宽度 | 4-interaction/patterns |
| P3.15 | Object Explorer 分面、Search around、保存 | 4-interaction/workbenches |
| P3.16 | F1 就地入口：从数据集创建对象类型、从模板生成模块 | 5-experience F1 |
| P3.17 | 监听器、对象类型组、监控视图 | 2-product §2.2 |
| P3.18 | 条件构建器与对象集构建器各一个实现 | 4-interaction/patterns M10、M11 |
| P3.19 | 对象合并规则统一实现 | 6-domain §6.5 |
| P3.20 | 切换语言不整页刷新 | 7-system/web R6 |

## P4 行业应用

| 编号 | 事项 |
|---|---|
| P4.1 | 用平台工具重建 manufacturing（MES）为产品版本，替换 `deploy/packages/manufacturing*.json` |
| P4.2 | 同样重建 hospitality（PMS）与 heating |
| P4.3 | 每个探针发现的平台缺口回写 P2/P3 |

## P5 交付运营

| 编号 | 事项 |
|---|---|
| P5.1 | 产品依赖闭包、语义版本、依赖范围 |
| P5.2 | 受管项目与客户扩展、扩展属性 |
| P5.3 | 安装审阅、升级审阅、卸载 |
| P5.4 | Marketplace 目录、已安装、更新 |

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- `web/packages/platform` 为空目录；`cmd/compute` 只有 Dockerfile；`cmd/webhook-sink` 在接收端合并后仍保留。
- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- `journal/` 是持久化而非会计；改名为 `store` 或并入持久化包时一并处理（P2.11）。
- 界面中“动作”与“操作”混用，统一为“动作”（术语表）。
