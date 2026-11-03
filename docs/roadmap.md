---
level: 路线图
covers: []
read-when: 每批开始（选下一批）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

- 发布准备的四族盘点已写入 `docs/7-system/README.md` §7.5.1；下一步把 integration 的发布状态迁移并入注册表，再用快照与重放证明字节不变。Confirmation 与 Dispatch 各自维护状态机，暂不动。
- `platform/page_model_legacy.go` 仍解码旧页面格式；确认已接受映像中不再存在旧格式后，按版本化迁移退役。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。
