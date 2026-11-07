---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

### 包生命周期收敛（2026-11-05）

目标规范见 [6-domain/packages](6-domain/packages.md)。一批只交付一个可运行的完整路径；完成的条目直接删除。

- [ ] C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。

### 租户并发（2026-11-06）

目标规范见 [7-system/concurrency](7-system/concurrency.md)。车道已可取消、可观测，后台按任务持锁，HTTP 响应与文件字节在车道外写出；剩余三批按顺序做，每批 `make check`、K13 加 `-race`。

- [ ] T2 读取范围按次持有（P0）：把 `t.recordReadError`、`records.Current()` 的已打开视图、系统定义投影与 `t.ctx` 改为单次读取的值，经 `runtime{}` 调用者传递；`Tenant` 读取方法接收调用者 `ctx`。完成标准：两次读取可以在同一租户并发执行且 K13 `-race` 通过；仍取车道，不改变行为。
- [ ] T3 读取映像（P0）：盘点读取闭包触达的可变状态并纳入映像（C3），车道内应用后发布；`coordinatedRecordRead` 改为取映像 + C4 位置校验与回退，不取车道。完成标准：后台任务持有车道时记录读取不等待；`platform.read_image.retries` 上线；K13 增加“读取不混用两个位置”。
- [ ] T4 决定与提交分离（P1）：`submit` 与后台任务在映像分叉上决定，带基准位置；车道内只校验、提交、应用、发布；冲突重算 2 次后回退到车道内决定（C5）。完成标准：`platform.decision.conflicts` 上线；K1 重放逐字节不变；K13 增加“并发提交等价于某个串行顺序”。
- [ ] T5 车道内工作清点（P1）：逐一核对 56 个文件 129 处 `t.mu.Lock` 是否符合 C-R2，外部 I/O 改为预留/结算；`SweepUploads` 先引入待删除标记再移出车道；`BeginAgentRead` 改用映像。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- 治理资源与容器尚未采用同一路由入口（`7-system/web`）。
- Workspace 仍有大于 500 kB 的分块与三处无效动态导入告警；真实宿主冷缓存复测显示分块主要属于当前身份需要的应用入口，暂不做机械拆包。
