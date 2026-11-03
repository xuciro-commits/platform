---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

无排期项。发布四族与旧页面格式的保留理由写在 `docs/7-system/README.md`。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。
- 门户的业务组未按产品或项目分组（`3-information` §3.3）。
- 治理资源与容器尚未采用同一路由入口（`7-system/web`）。
- 授权上下文在宿主根包构造，未进入 `services/security`（`6-domain/security`）。
- 少数位置直接编译 Expr，绕过可注入的规则引擎（`6-domain`）。
- `SetBuilder` 的 `domain` 条件行仍由命名查询依赖（`4-interaction/patterns` M10）。
