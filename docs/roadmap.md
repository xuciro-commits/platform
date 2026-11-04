---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成。P0/P1/P2 全批均已完成，代码与文档同步，无未完成条目。

已完成的 P0/P1/P2 条目按路线图规则“条目完成即删除”已从本文移除；详细完成情况见 `PLATFORM-NEXT-PHASE.md`（各任务均标记 ✅已完成）及 `git log`。

发布四族与旧页面格式的保留理由写在 `docs/7-system/README.md`。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。以下已由 `PLATFORM-NEXT-PHASE.md` 纳入当前批次并删除：门户业务组分组（P0-11）、治理资源与容器同一路由入口（P0-2/P0-5 路由部分）、授权上下文下沉 `services/security`（P0-6 相关授权统一）、搜索前缀 `@#:` 不生效（P0-2）。

其余尚未排期：

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`（ADR-07 决定不改名 `web/packages/kernel`，本条保留为代码歧义说明，不作为改动项）。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包（ADR-08）。
- 少数位置直接编译 Expr，绕过可注入的规则引擎（`6-domain`）。
- `SetBuilder` 的 `domain` 条件行仍由命名查询依赖（`4-interaction/patterns` M10）。
