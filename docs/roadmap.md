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
| P2 内核收敛 | 资源内核、单一实现、服务包边界、薄验收 | 完成 |
| P3 构建者闭环 | 黄金路径 F1–F10 每一步达到 4-interaction 与 5-experience 的标准 | 完成 |
| P4 行业应用 | 用平台工具重建行业产品包，作为平台的探针 | 完成 |
| P5 交付运营 | 产品依赖、受管项目、客户扩展、升级审阅 | 完成 |

## 下一批

本轮 P2–P5 已全部完成；新的差距从下一批重新编号。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。

## 清理待办

- `pageui/manifest.ts` 与 `platform/page_compat.json` 仍是两份手工组件数据；共享字段已有差异（observation 输出端口、scopeRule 默认值）。先定一份权威来源，再删除生成副本。
- 发布准备仍按 definition、integration、record、page 四族分派；Confirmation 与 Dispatch 各自维护状态机。收敛前先给出资源族盘点，并证明发布字节与重放结果不变。
- `platform/page_model_legacy.go` 仍解码旧页面格式；确认已接受映像中不再存在旧格式后，按版本化迁移退役。
