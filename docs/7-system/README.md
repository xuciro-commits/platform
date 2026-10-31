---
level: 7 系统架构
covers: [capabilities/server/*.go, capabilities/server/kernel/**, capabilities/server/journal/**, capabilities/server/composition/**, capabilities/server/cmd/**, contract/**]
read-when: 新建或移动 Go 包、改动写入或读取路径、接受结果格式、重放与快照、租户装配、后台任务、协议与 API 类型
---

# 7 系统架构

## 目标

一个 Go 宿主进程，内部按服务边界划包；所有权威写入经同一条接受路径，所有读取经同一个协调读取；任何状态都能从接受结果逐字节重建。本目录三份文件：

- 本文：进程与包、写入路径、读取路径、运行机制、协议与版本、代码规范。
- [web.md](web.md)：前端架构。
- [verification.md](verification.md)：检查、钩子与验收。

## 7.1 规则

- R1 一个宿主进程服务多个租户；不拆微服务。隔离计算、代码构建是受控的辅助进程。
- R2 包按服务边界划分；包之间只经接口调用，不共享内部状态，不形成环。
- R3 应用（包括内置应用与产品包的编译部分）只导入 `platform`。
- R4 权威写入只有一条接受路径（7.3）；任何旁路写存储都是缺陷。
- R5 记录读取必须经协调读取（7.4）；HTTP 读取记录的路由只用 `recordRead`。
- R6 重放只应用已接受映像，不重跑规则、授权或外部调用。
- R7 协议、存储格式、API 类型都带版本，从单一来源生成。

## 7.2 包结构

目标结构（`capabilities/server/`）：

| 包 | 职责 | 依赖 |
|---|---|---|
| `expr` | Expr 的解析、类型检查、求值与 SQL 下推编译；前端经 WASM 复用 | 无 |
| `kernel` | 提交接收、幂等、修订、接受结果编码、日志、记录存储、车道、重放、快照 | `expr` |
| `platform` | 应用 SDK：Manifest、Caller、动作目录、Expr 与 ObjectSet 接口、授权接口 | `kernel` |
| `services/resources` | RID、项目、文件夹、资源生命周期、工作状态、分支、提案 | `platform` |
| `services/ontology` | 本体元数据与类型检查 | `platform`、resources |
| `services/objects` | 对象投影、编辑事实、索引、ObjectSet 执行 | `platform`、ontology |
| `services/actions` | 动作执行、提交条件、副作用调度 | `platform`、objects、functions |
| `services/functions` | 函数制品、受控编译、隔离执行 | `platform` |
| `services/data` | 源、同步、流、数据集、管道、调度、导出、血缘与标记推导 | `platform`、resources |
| `services/automate` | 自动化与其运行 | `platform`、actions |
| `services/apps` | Workshop 模块与页面运行 | `platform`、objects、actions |
| `services/work` | 工作项、通知与投递 | `platform`、actions |
| `services/devops` | 产品、版本、安装、升级 | `platform`、resources |
| `services/security` | 成员、团队、组织单元、业务角色、标记；授权上下文构造与唯一决定函数 | `platform` |
| `services/ai` | 模型网关、AIP Logic、智能体运行、AIP Assist | `platform`、functions、actions |
| `host` | HTTP 路由、身份接入、租户装配、部署、后台调度 | 全部 services |
| `cmd/*` | 二进制入口 | `host` |

- 服务之间的调用面在被调服务的 `api.go` 中声明为接口；装配在 `host` 完成。
- 产品包中的行业逻辑不进入任何 Go 包。

## 7.3 写入路径

```
POST /v1/submissions
 → 准入（租户、主体、隔离状态、项目角色边界）
 → 定位拥有者（动作 → 服务）
 → 暂存决定：在分叉的记录存储上调用拥有者 Decide，收集编辑与意图
 → 内核接收：字段校验、模式版本、幂等键、期望修订、授权（security 决定路径）、领域规则
 → 封存接受结果（单行或批量；拒绝也封存）
 → 日志追加（一个 SQL 事务：日志行 + 状态增量 + 位置推进）
 → 从已提交字节应用到记录存储
 → 发布事件 → 入队副作用与自动化
```

- 拥有者只返回编辑与意图，不直接写存储；应用发生在提交之后、且只从已提交字节解码。
- 应用结果与提交字节不一致时隔离租户，停止写入，等待运维处理。
- 副作用至少一次投递，以请求键幂等。

## 7.4 读取路径

- 协调读取：每次记录读取持有租户车道，并在 SQL 状态上开启固定到“已应用位置”的可重复读只读事务；位置不一致即隔离。
- 命名读取：`GET /v1/{read}` → 入口授权 → 拥有者 Read → 行与字段收窄。
- ObjectSet：编译为计划 → 能下推的条件下推到索引列 → 可见性（行策略 + 标记）→ 字段视图。
- 授权：一条决定路径，构造上下文（租户、主体、动词、项目、标记、行、字段、派生）后交给唯一的决定函数；集合读取与单条读取同路径。

## 7.5 运行机制

| 机制 | 规则 |
|---|---|
| 租户隔离 | 权威状态在共享表中以租户列隔离；按租户的只读投影模式供分析与外部查询，可选 |
| 车道 | 每个租户一个车道锁，写入与协调读取共用 |
| 快照 | 每 N 条接受结果与停机时保存；启动时从最新兼容快照恢复并重放其后条目 |
| 隔离 | 应用不一致、位置不一致时租户进入隔离，只读 |
| 后台工作 | 同步、构建、自动化、投递经工作队列；每项有运行记录与重试 |
| 隔离计算 | 函数在独立进程的 WASM 中执行：内存页上限、并发槽、模块大小上限、摘要校验；构建在无网络容器中 |
| AI 网关 | 预留额度 → 调用供应商 → 结算；推断许可由部署环境指定 |
| 可观测 | OpenTelemetry 追踪与指标；`/healthz`、`/metrics` |

## 7.6 协议与版本

- `contract/proto` 是协议的唯一来源；Go 与 TypeScript 类型由它与宿主类型生成（`go run ./cmd/api-types` 生成 `web/packages/kernel/src/gen/host.ts`），生成文件不手改。
- 每个提交带模式版本头；接受结果格式带版本；存储格式演进只经版本化迁移，迁移本身是接受结果。
- 错误带错误码与可翻译的英文原因；原因字符串是翻译键。

## 7.7 代码规范

- 手写文件 ≤ 800 行（Go）/ 600 行（TS）；超出按职责拆分，不按“工具函数”拆。
- 包名表达职责；同一名字不指两样东西（如 `kernel`、`agents`）。
- 不留兼容分支，除非存储格式需要（且写明版本）。
- 注释、删除旧路径、新依赖的规则见 AGENTS.md 硬规则。

## 现存差距

- 宿主根包 `platformserver` 平铺约 180 个文件，`engine/`、`studio/`、`apps/`、`internal/` 与目标包不对应（路线图 P2.11）。
- 授权上下文构造在 `Tenant.authorizeDepth`（根包），决定函数在 `platform/authz`，应一并移入 `services/security`，`platform` 只留接口（P2.6）。
- Expr 求值器在 `internal/rules`，应成为 `expr` 包；`platform/field_policy.go`、`platform/entity.go`、`platform/definition.go`、`kernel/records/record_domain.go` 绕过可注入的规则引擎（P2.5）。
- AI 分散在 `aigateway`、`apps/ai`、`internal/aistate`、`internal/agents`、`engine/agents`、`platform/ai.go`；两个包都叫 `agents`，`aigateway` 反向依赖 `engine/agents`（P2.7）。
- 重放仍重新执行旧条目类型（submission、input、effect、usage、agent、delivery、job），违反 R6（P2.13）。
- 超过 §7.7 限额的手写文件（P2.12）。

## 相关

- 领域概念：[6-domain](../6-domain/README.md)
- 质量目标 Q1、Q4：[1-enterprise §1.7](../1-enterprise.md)
