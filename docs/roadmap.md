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
| P5 交付运营 | 产品依赖、受管项目、客户扩展、升级审阅 | 进行中 |

## 下一批

整体收尾 P2–P5：贯通资源与权限、构建与行业产品包、客户交付运营；全部完成后结束本轮。

## P2 内核收敛

| 编号 | 事项 | 完成标准 | 规范 |
|---|---|---|---|
| P2.9 | 行业代码出宿主 | `apps/core/books.go`、根目录 `books.go` 的总账逻辑移出平台 | 2-product R4 |
| P2.10 | 内置业务应用归位 | Knowledge → Agent Studio 检索源；Master data → 产品包；Organisation directory → Control Panel Organization | 4-interaction/workbenches |
| P2.11 | 服务包迁移 | 宿主按 7-system §7.2 的目标包逐个迁移，每次迁移删除旧路径 | 7-system §7.2 |
| P2.14 | 验收补齐 | 内核不变量 K4–K6；F2–F10 随相关批次逐条加入 | 7-system/verification |

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
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。
