---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

### 对象探索与规则收口（2026-11-04）

本轮只保留未完成、需要继续验收的能力；完成项由 git 历史追溯，不建立第二份完成清单。发布四族与旧页面格式的保留理由仍写在 `docs/7-system/README.md`。

- [ ] D1 为 `work.view` 定义版本化状态，并决定/实现 `build.query` 承载完整 ObjectSet 的版本化发布合同；在 Workshop 或对象页绑定同一已发布版本，禁止语义缩水。
- [ ] D2 把授权上下文来源与构造收进 `services/security` 的窄端口，host 只保留协调读和租户适配；先保持 K4 与读作用域不变。
- [ ] D4 用真实宿主、冷缓存和代表性身份复测首屏；只消除仍在关键路径上的静态依赖，不为 500 kB 告警机械拆包。
- [ ] D5 补资源 route、release kind、TestPlan/EvaluationSet 入口的专门验收，避免只靠生成契约推断。

### 包生命周期收敛（2026-11-05）

目标规范见 [6-domain/packages](6-domain/packages.md)。一批只交付一个可运行的完整路径；完成的条目直接删除。

- [ ] C6 定义生产完成与会计过账的一致性边界；在边界决定前不得发布 ERP/MES 拆分包。
- [ ] C7 增加 ownership rehome 接受结果：只改变 `Project / ManagedBy`，保留 RID、类型名、应用身份、记录、媒体和历史。
- [ ] C8 拆分 manufacturing 为 core/ERP/MES（按 C6、C7 的结论），移除 MES 对 ERP 内部记录地址的轮询，改用对象接口、动作或版本化查询。
- [ ] C9 增加安装闭包回执和依赖锁：显示每步 `installed / pending / failed`，支持幂等重试，不建立第二套安装事实。
- [ ] C10 把升级、降级、回滚拆成三个明确动作；只有旧定义能读取当前数据时才提供降级，失败重试不与回滚混称。
- [ ] C11 增加统一 lifecycle impact graph：依赖、客户扩展、客户引用、在飞工作、连接前提和可保留输出供 drain、uninstall、upgrade 共用一次计算。
- [ ] C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- 治理资源与容器尚未采用同一路由入口（`7-system/web`）。
- Workspace 仍有大于 500 kB 的分块与三处无效动态导入告警；`catalog` 已改为按需加载，拆包等待 D4 的真实测量。
