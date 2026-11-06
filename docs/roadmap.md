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

- [ ] L1 安全与完整性（P0）
  - 新建 `capabilities/server/services/data/lineage`：纯函数 `Build(inputs) Graph`、`Propagate(graph)`、`Project(graph, visible)`、`Scope(graph, params)`；输入是六类记录的普通切片，不依赖 `host`。`host/lineage.go`、`host/markings.go` 只收集记录、调用它、做授权投影，旧的 `lineageGraph` 删除。
  - 收集改为在一次存储锁内读取全部行（`storeRows` 同一路径），不经 `platform.Find`，去掉每类 1000 条与拒绝即空。
  - Guard 基线：每条记录经拥有者的 `acceptedIntegration` / `Execution*` 得到执行定义（不另写 `Published` 解析）；对象类型用已发布快照。`markingsIn` 只用 Guard；Preview 只给视图。
  - 写出证据：`SourcePull` 记录写出目标与标记；数据源与管道记录累计写出目标（只增不减），由运行接受结果写入，重放一致。Guard 取图传播与写出证据的较高者。新字段改宿主类型后 `go run ./cmd/api-types`。
  - 投影：每个节点经资源目录映射 RID 与项目，可见性用 `resourcePermission`；不可见节点匿名为边界计数。
  - 边：稳定 `id`、`role`、`occurrence`、`declaredBy`、`unresolved`；悬空边保留并计数；焦点根保留。
  - 状态：`status` 与 `staleReasons`（L1 表），`errorCode` 代替原始错误。
  - 响应加 `scope`、`basis`、`total`、`returned`、`truncated`、`truncationReasons`、`boundaries`、`unreadable`、`coverage`，参数先支持 `mode`、`focus`、`ref`、`project`、`direction`、`depth`、`nodeLimit`、`edgeLimit`；旧字段暂留给现有前端。
  - 顺手修：`markingIndex.highest` 对数据集版本只取该数据集的标记。
  - 完成标准：K12 全部断言通过（含“保存删去受限绑定的对象类型定义后、激活前，未获准成员仍读不到该类型”）；K5 照常通过；`make contract`、`make verify`、`make verify-sql` 通过。
- [ ] L2 入口与工作台（P0）
  - 应用迁到 `web/packages/govern/src/lineage/`（按职责拆文件，每个 ≤600 行），从 `apps/workspace/src/chrome.tsx` 移除 `lineageApplication`，`Lineage.tsx` 删除；应用说明改为数据流与影响；命令面板改归 “Governance”。
  - 按 workbenches 实现 Overview 与 Graph（`Workbench`、只读 `RelationCanvas`、Inspector、dock 的 Impact 与 Coverage 页签先给计数），URL 回放全部参数，Focus / Isolate / Expand 三种操作与键盘。
  - ui：类别色令牌与 RelationNode 类别色字段、“+n” 展开把手、选中不移动视口（patterns M19）；`relationNodeClasses` 的全局 tone 不改，Data Lineage 只用类别色。
  - 去掉 `/v1/resources` 二次拼装与 15 秒轮询；打开资源用节点的 `rid` 与 `canOpen` 经 `opens` 解析。
  - 入口：六类数据资源页 Usage 加 “Open in Data Lineage”（一个共享组件，在 `@platform/app`，传 RID）；`OpenIn` 传 RID；Monitoring Runs 资源链接改为打开资源；观测源按钮改去定义 Usage；`PublishedDependencies` 与 `CapabilityDiscovery` 移到浏览定义（`#/definitions`）。
  - 前端改用新字段后删除 L7 列出的旧字段，`make contract` 重新生成。
  - 中英文字典齐全，`node scripts/check-i18n.mjs` 通过；原因码全部可翻译。
  - 完成标准：`make e2e SPEC=F19`（步骤 1、2、4）与 `make e2e SPEC=F20` 通过；`pnpm exec tsc -p .`（govern、app、ui、workspace）、`cd web && pnpm build`、`make verify` 通过；负责人浏览器走查观感。
- [ ] L3 影响分析（P1）：`impact` 在完整可见图上计算（按种类、项目、状态，前 200 项与 `atLeast`）；dock Impact 表与 Focus 联动；跨项目匿名边界；Inspector 的 Related artifacts 只做到原资源的深链。完成标准：K12 增加“裁剪不改变影响计数”；F19 步骤 4 的影响数与服务端一致。
- [ ] L4 列级血缘（P1）：服务端按 lineage L6 推导字段图（数据源映射、管道步骤、对象类型绑定、回写请求与结果），`fields`、`field` 参数；字段追踪与 dock Fields；`result` 边接入并按保守语义传播；删除 `build/src/ontology/lineage.tsx` 的客户端推断，Ontology Manager 字段来源表改读服务端，`integration-health.tsx` 改读运行证据而不是推断。完成标准：F19 步骤 3；未声明字段显示未验证；K12 增加回写结果不降低对象类型保护。
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
