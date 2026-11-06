---
level: 7 系统架构
covers: [capabilities/server/kernel/tenant_lane.go, capabilities/server/host/record_read_scope.go, capabilities/server/host/submit.go, capabilities/server/host/staged_decision.go, capabilities/server/host/operations.go, capabilities/server/kernel/records/**, capabilities/server/kernel/store/postgres.go, capabilities/server/services/automate/runtime/automation_work.go]
read-when: 改动租户车道、协调读取、暂存决定、接受结果提交与应用、后台工作轮次，或在车道内新增任何工作
---

# 7 系统架构 · 租户并发

## 目标

一致性由“事实如何成为事实”保证，而不是由“谁独占租户”保证。一个租户内：

- 读取不等待写入，也不等待后台工作；每次读取看到一个完整的已应用位置。
- 决定（授权、规则、拥有者 Decide）在不可变映像上计算，不持有车道。
- 车道只包住“验证基准位置 → 提交 → 应用 → 发布映像”，持有时间以毫秒计。
- 接受结果的格式、重放语义、幂等与隔离规则不变；历史字节不变。

本文是 [README §7.3](README.md#73-写入路径)、[§7.4](README.md#74-读取路径) 的并发规格；两处的步骤顺序不变，本文规定每一步在哪把锁里、看哪个位置。

## 规则

- C-R1 租户仍是唯一的提交序列：一个租户的接受结果按位置严格递增，同一时刻只有一个提交者。不引入对象锁、行锁、Actor、消息队列、分布式事务或工作流引擎。
- C-R2 车道只做提交：车道内只允许基准校验、`AppendAccepted`、从已提交字节应用、发布映像、隔离判定。网络、外部进程、构建、模型调用、全量扫描、`sleep`、等待其他锁都不得在车道内。
- C-R3 读取经映像：协调读取取最新已发布映像，不取车道。映像发布后不可变；任何读取不得触达映像之外的可变租户状态。
- C-R4 决定是候选：决定在映像的分叉上计算，带基准位置；只有在车道内基准等于已应用位置时才提交。基准落后即作废重算，不做部分合并。
- C-R5 有界进展：同一决定的乐观重算至多 2 次；仍冲突则在车道内决定一次（即今日行为）。任何路径都不会饿死，最坏情况不差于今日。
- C-R6 外部 I/O 两段化：先以接受结果预留（reserve / prepare），在车道外调用，再以接受结果结算（settle / consume）。决定本身只读预留与已准备的输入，不发起 I/O。
- C-R7 不以超时、重启、换进程或给锁加超时作为修复；慢操作按 C-R2 移出车道。
- C-R8 一个实现：读取映像就是现有记录投影的不可变发布，不新建“从 SQL 直接读”的第二条读取或授权路径；乐观校验在内核提交处实现一次，拥有者不写并发代码。
- C-R9 进程安全优先：Go 的映射并发读写会使整个宿主崩溃（影响所有租户）。任何移出车道的读取或决定，先证明它触达的状态在映像内或不可变，并在 `-race` 下通过 K13。

## 规格

### C1 三段

```
读取：  映像 N ──────────────► 结果（同一位置 N；SQL 视图校验 = N）
决定：  映像 N ─► 分叉 ─► Decide/授权/规则 ─► 候选结果(basis=N)
提交：  [车道] applied==N ? 追加→应用→发布映像 N+1 : 冲突
后台：  预留(接受) ─► 车道外 I/O ─► 结算(接受)
```

### C2 车道

| 项 | 规格 |
|---|---|
| 实现 | `kernel.Lane` 是容量 1 的通道信号量，等待按到达顺序；`Hold(ctx, holder)` 在 `ctx` 结束时放弃排队并返回错误；`Lock()` 等价于不可取消的 `Hold`；释放未持有的车道直接 panic |
| 持有者标签 | 未指定时取调用函数名（如 `host.Tenant.submit`）；HTTP 协调读取用路由模式；后台工作用 `work`。标签只有代码位置与路由模式两类取值，不含 ID |
| 预算 | 单次持有超过 200 ms 记一条日志（tenant、holder、时长），并作为“最近一次慢持有”出现在健康读取中；不打断执行 |
| 调用方上下文 | HTTP 协调读取把请求 `ctx` 传给车道；客户端断开即退出排队，不再执行读取 |
| HTTP 响应 | `rt.recordRead` 的处理函数在车道内写入缓冲，释放车道后才写网络；文件与媒体字节在车道内鉴权、经 `outsideLane` 在车道外流式传输 |
| 诊断 | 车道状态（持有者、持有时长、等待数）用原子值读取，不取车道；`/v1/health` 的观测存储探测与指标采集都不在车道内 |
| 车道内禁止 | C-R2 所列；违反由持有预算日志暴露，修正方式是移出车道，不是放宽预算 |

### C3 读取映像

映像是某个已应用位置 N 上、读取路径所需全部租户状态的不可变快照：

| 内容 | 来源 | 不可变方式 |
|---|---|---|
| 记录投影各类型的行与派生索引 | `kernel/records`（`Index[*Row]` 已是路径复制持久树） | 发布时取根；应用只生成新根 |
| 当前范围与 SQL 来源行的位置 | `records.Current()` | 映像记录 N；SQL 读取经 C4 校验 |
| 授权上下文：成员、团队、许可、标记索引、项目授权 | `services/security`、`host/markings.go` | 发布时冻结或改为持久结构 |
| 资源目录与工作状态 | `host/resource_directory.go` | 同上 |
| 拥有者命名读取所需的账本状态 | 各 `platform.Ledger` | 逐个纳入；未纳入的命名读取暂留车道（现存差距） |
| 已应用位置、隔离标记、业务停止标记 | `Tenant` | 原子值 |

- 发布：车道内应用完成后，以原子指针替换“当前映像”。读取者 `Load` 一次，整次读取只用这一份。
- 映像内容清单写在一处（`host` 的映像构造函数），读取闭包只经它取状态；新增读取依赖必须先加进清单。
- 前提是读取范围按次持有：一次读取的 SQL 视图、读取错误、系统定义投影和追踪上下文属于这次读取的值，经调用者传递，不再是租户级字段；在此之前任何两次读取都不能并发。
- 内存：持久结构共享未改动部分；旧映像随最后一个读取者释放。不得为发布深拷贝全量行。

### C4 读取一致性

SQL 来源行仍从 PostgreSQL 可重复读事务读取：

1. 取映像 N。
2. 开启只读可重复读事务，第一条语句读 `runtime_tenants.seq` 得到 S（固定该事务快照）。
3. S = N：一致，执行读取。
4. S > N：已有更新的提交尚未发布映像；关闭事务，等待映像 ≥ S 发布（上限 50 ms）后重试，至多 2 次；仍不一致则取车道读一次（今日行为）。
5. S < N：不可能出现在健康租户中；隔离租户（与今日 `StatePositionError` 同义）。

读取结束不再检查“内存应用位置 = SQL 位置”，因为读取不再持有车道；该检查移到车道内应用完成处（C5）。

### C5 决定与提交

| 步骤 | 位置 | 规格 |
|---|---|---|
| 准入 | 车道外 | 租户、主体、隔离、业务停止、模式、项目角色边界（`submit` 前半段） |
| 幂等预查 | 车道外，映像 N | 命中已提交答案直接返回；未命中继续（提交时再查一次） |
| 决定 | 车道外，映像 N 的分叉 | `newStagedDecisionOn(image.records.Fork())`；序列、通知、连接器状态取自映像；产出候选结果与 `basis=N`；拒绝同样是候选 |
| 校验 | 车道内 | `applied == basis` 才继续；否则释放车道、按 C-R5 重算 |
| 提交 | 车道内 | `AppendAccepted` 不变（SQL 事务内的 `statePosition == seq-1` 检查保留为最后防线）；幂等在 SQL 中二次确认 |
| 应用与发布 | 车道内 | 从已提交字节应用；应用后断言 SQL 位置 = 内存位置，不一致即隔离；发布映像 N+1 |
| 事件与副作用 | 车道外 | 入队投递、自动化、外部副作用 |

- 校验粒度是整个租户位置：任何插入的提交都使候选作废。这自然覆盖跨对象唯一性、库存汇总、权限撤销、包退役等约束，不需要拥有者声明读集合。
- 细粒度校验（对象版本、读集合）不在本规格内；只有在 C8 指标显示冲突重算成为瓶颈时，才另写规格，由内核统一记录读集合，拥有者不手写。
- 决定必须确定且无副作用：同一映像、同一提交得到同一候选字节。依赖外部结果的决定只读 C-R6 的预留或准备输入（现有 `preparedOperationInput`、模型预留即此模式）。

### C6 后台工作

- `Tenant.Round` 每个任务、每个作业各取一次车道（`WorkLoop.Round(now, 1)`），任务之间读取与前台提交可以进入；车道内仍包含该任务的决定，直到 C5 落地。
- `WorkLoop` 的配额、轮转与 `opsMu` 不变；轮转按任务推进。
- 投递、同步、构建、管道运行、智能体步骤：外部部分按 C-R6 在车道外执行；结果以接受结果回到 C5。
- 调度器按租户轮转不变；一个租户的长任务只延迟该租户的下一任务，不占用其他租户的车道。

### C7 启动、快照、隔离与重放

- 启动恢复、快照保存、维护操作仍可整段持有车道（holder 为 `startup`、`snapshot`、`admin:*`），期间读取按 C4 第 4 步回退并等待；这些路径不在前台请求热路径上。
- 重放只应用已接受映像（README R6），与本规格无关；K1 不变。
- 隔离判定集中在两处：车道内应用后位置断言（C5）与读取 S < N（C4）。

### C8 指标与诊断

| 指标 | 类型 | 标签 |
|---|---|---|
| `platform.lane.wait` | 直方图（秒） | tenant、holder |
| `platform.lane.hold` | 直方图（秒） | tenant、holder |
| `platform.lane.waiters` | 当前值 | tenant |
| `platform.lane.held` | 当前值（秒），当前持有者已持有时长 | tenant |
| `platform.decision.conflicts` | 计数：`retried` / `fallback` | tenant |
| `platform.read_image.retries` | 计数：`retried` / `fallback` / `quarantined` | tenant |

- 标签遵守 [observability §7.20](observability.md)：不含 member、对象 ID、URL。
- Monitoring 的 Health 页显示车道等待数与最近一次慢持有（持有者、秒数、时间）。

## 现存差距

- 读取：`host/record_read_scope.go` 的 `coordinatedRecordRead` 每次读取仍取车道。读取范围是租户级的单一槽位（`t.recordReadError`、`records.Current()` 的已打开视图、`beginRecordRead` 中的 `applySystemDefinitionProjection`、`telemetry.go` 的 `t.ctx`），C3 的前提尚未满足。
- 读取：`Records`、`RecordOf`、`Search`、`Readable` 等 `Tenant` 方法与非 HTTP 调用仍传 `context.Background()`。
- 提交：`host/submit.go` 的 `submit` 从准入后整段持有车道，决定在车道内计算；`newStagedDecision` 分叉的是可变的当前存储，序列、通知、连接器与拥有者账本取自活动状态。
- 映像：读取闭包触达的可变状态（成员与授权、标记索引、资源目录、各拥有者账本、序列、通知、连接器）尚未盘点，也未冻结。
- 车道内：56 个文件共 129 处直接调用 `t.mu.Lock`（`continuous_frames.go`、`agent_host.go`、`release_candidate.go`、`branches.go`、`aicall.go` 等），持有范围未逐一核对是否符合 C-R2；`agent_host.go` 的 `BeginAgentRead` 以车道代替读取映像。
- 车道内：`host/files.go` 的 `SweepUploads` 在车道内删除对象存储字节；移出需要先有“待删除”标记协议，否则与附件引用竞争会丢字节。
- 指标：`platform.decision.conflicts`、`platform.read_image.retries` 随 C4、C5 落地。
- 验证：K13 只覆盖 C2 与 C6；C4、C5 的并发部分随实现补齐。

## 相关

- 写入与读取路径：[README §7.3、§7.4](README.md)
- 指标规则：[observability](observability.md)
- 不变量：[verification](verification.md) K1、K13
