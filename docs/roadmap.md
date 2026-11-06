---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

### Data Lineage 重建（2026-11-05）

目标规范见 [6-domain/lineage](6-domain/lineage.md)（语义、基线、合同、现存差距）与 [4-interaction/workbenches § Data Lineage](4-interaction/workbenches.md#data-lineage)（界面）。顺序固定：先安全与完整性，再入口与界面，再影响与字段；不先做列级、保存图或全租户大图。每条是一批，按技能 `platform-batch` 交付；完成即删除，并删除 lineage.md 中对应的现存差距。

L1–L4 已于 2026-11-06 完成：安全与完整性、入口与工作台、影响分析、列级血缘均通过 K12、F19、F20、`make check`、`make e2e` 与 `make verify-sql`。
- [ ] L5 分支与版本（P1/P2）：`branch` 参数复用分支暂存世界，分支图只读、不写 main 索引；节点展示保存版本、执行版本、数据集版本与已消费版本；构图按 generation 缓存并测量 1000+ 资源的响应时间。不做历史时点查询。
- [ ] L6 产品化（P2）：项目聚合图细化、成员私有的视图配方（只存查询与布局参数）、分享链接、超大图性能、可访问性检查。

### 可观测性收敛（2026-11-05）

目标规范见 [7-system/observability](7-system/observability.md)。先修事实源，再扩界面；一批仍交付一条可运行路径。

- [x] O1 建立独立运营观测存储与统一 `Observation` 契约；内存、文件、PostgreSQL 同语义；日志不写业务 journal。
- [x] O2 HTTP 入口生成 request ID 与 W3C trace 上下文；记录租户、主体、路由模式、状态、耗时；默认记录失败、慢请求与 10% 成功请求。
- [x] O3a 提交接受与拒绝写入运营日志；日志查询按租户、级别、来源、应用、时间、运行、资源、请求过滤，使用游标与保留覆盖。
- [x] O3b 后台失败与 owner 运行生命周期写入运营日志；运行索引成为 Monitoring 与 application runs 的唯一查询路径。
- [x] O4a Monitoring → Logs 完成列表、筛选、详情、空/无权限/截断/保留状态；Live 明确为 2 秒轮询并支持暂停。
- [x] O4b 保存视图扩为查询族，旧 type/status 视图迁移且不出现第二套保存资源。
- [x] O5a 完成租户级日志策略：级别、请求日志、保留；清理幂等；策略变更经接受结果；日志访问写入审计。
- [x] O5b 增加配额、临时 debug、应用级策略和不可变安全审计存储；审计读取不能依赖内存环。
- [x] O6a `/healthz` 只做存活，新增 `/readyz`；`/metrics` 改为 loopback 或 host admin 抓取，并保留状态类。
- [x] O6b Health 显示观测摄取、丢弃、保留和 readiness 条件及关联运行。
- [x] O7 运行索引替代 Monitoring 与 application runs 的 owner 全量扫描；历史缺口显示 `coverage`，原始 owner 仍是运行权威。
- [x] O8 Traces、Metrics、Alerts 只在 O1–O7 通过后接入；告警复用 Automate，不新建执行器。

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
