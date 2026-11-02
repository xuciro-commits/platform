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

整体收尾 P2–P5：贯通资源与权限、构建与行业产品包、客户交付运营；全部完成后结束本轮。

## P2 内核收敛

| 编号 | 事项 | 完成标准 | 规范 |
|---|---|---|---|
| P2.2 | 资源内核 | 所有资源有 RID、项目、文件夹、版本、状态；路由以 RID 为键；项目覆盖不再从页面列表推导 | 6-domain §6.3、3-information §3.6 |
| P2.3 | 动作类型独立资源 | 动作类型有自己的记录、版本与资源页；对象类型只引用 | 6-domain §6.7 |
| P2.8 | 删除流程残留 | 以 Flow 命名的宿主文件与 `flow.instance` 视图删除；逻辑流只作为函数引擎 | 2-product R7 |
| P2.9 | 行业代码出宿主 | `apps/core/books.go`、根目录 `books.go` 的总账逻辑移出平台 | 2-product R4 |
| P2.10 | 内置业务应用归位 | Knowledge → Agent Studio 检索源；Master data → 产品包；Organisation directory → Control Panel Organization | 4-interaction/workbenches |
| P2.11 | 服务包迁移 | 宿主按 7-system §7.2 的目标包逐个迁移，每次迁移删除旧路径 | 7-system §7.2 |
| P2.13 | 重放只应用接受映像 | 重放不再重新执行 submission、input、effect、usage、agent、delivery、job 等旧条目；旧条目经版本化迁移转为接受映像 | 7-system/README R6 |
| P2.14 | 验收补齐 | 内核不变量 K4–K6；F2–F10 随相关批次逐条加入 | 7-system/verification |

## P3 构建者闭环

| 编号 | 事项 | 规范 |
|---|---|---|
| P3.8 | 专项标记、标记资源、解除标记步骤 | 6-domain/security §6.15 |
| P3.11 | 函数编辑提议；查询并入函数或对象集 | 6-domain §6.8 |
| P3.19 | 对象合并规则统一实现 | 6-domain §6.5 |
| P3.28 | 每位构建者独立的工作状态：草稿在保存前对他人不可见，分支上按人分开 | 6-domain/change §6.19 |

## P4 行业应用

| 编号 | 事项 |
|---|---|
| P4.1 | 用平台工具重建 manufacturing（MES）为产品版本，替换 `deploy/packages/manufacturing*.json` |
| P4.2 | 同样重建 hospitality（PMS）与 heating |
| P4.3 | 每个探针发现的平台缺口回写 P2/P3 |

## P5 交付运营

| 编号 | 事项 |
|---|---|
| P5.3 | 安装审阅、升级审阅、卸载 |

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- `web/packages/platform` 为空目录；`cmd/compute` 只有 Dockerfile。
- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- `journal/` 是持久化而非会计；改名为 `store` 或并入持久化包时一并处理（P2.11）。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。
