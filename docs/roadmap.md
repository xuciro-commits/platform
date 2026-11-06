---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

### Data Lineage 产品化后续（2026-11-06）

目标规范见 [6-domain/lineage](6-domain/lineage.md) 与 [4-interaction/workbenches § Data Lineage](4-interaction/workbenches.md#data-lineage)。L1–L6 与 F19–F21 已交付，以下只保留未完成项。

- [ ] D1 超大图性能（P1）：已加载子图的前端虚拟化/按需展开，保持 1000+ 资源的筛选、聚焦与隔离可用；验收为不静默丢节点、交互不卡顿。
- [ ] D2 导出（P2）：从当前只读图导出可见子图或影响列表，复用同一查询与授权结果，不建立第二条计算路径。
- [ ] D3 可访问性（P2）：对图、表格、筛选、Inspector 与键盘路径做专项检查；验收为关键任务可只用键盘完成，并有明确的焦点和错误反馈。

### 包生命周期收敛（2026-11-05）

目标规范见 [6-domain/packages](6-domain/packages.md)。一批只交付一个可运行的完整路径；完成的条目直接删除。

- [ ] C8 按 C6、C7 的结论拆分 manufacturing 为 core/ERP/MES 包并完成存量 rehome；MES 已改用 `productionorderupstream` / `productionbatchupstream` 查询，不再轮询 ERP 内部记录地址。
- [ ] C11 增加统一 lifecycle impact graph：依赖、客户扩展、客户引用、在飞工作、连接前提和可保留输出供 drain、uninstall、upgrade 共用一次计算。
- [ ] C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- 治理资源与容器尚未采用同一路由入口（`7-system/web`）。
- Workspace 仍有大于 500 kB 的分块与三处无效动态导入告警；真实宿主冷缓存复测显示分块主要属于当前身份需要的应用入口，暂不做机械拆包。
