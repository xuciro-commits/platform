# cmd 分类清单

`cmd/` 按部署相关性分三类（`openspec/changes/consolidate-platform-boundaries` B1.1 的产出，随实现演进维护）。**运行服务**是唯一计入部署拓扑的程序；工具二进制不作为部署服务运行、不出现在默认 compose 服务清单。

## 运行服务（3 个程序）

| 程序 | 职责 | 部署归属 |
|---|---|---|
| `platform-server/` | 通用宿主：租户装配、提交管线、Web API；webhook 接收端点将作为其可选进程内能力（B2 收编） | compose 双实例 8490/8495 |
| `gateway/` | 设备网关：会话与共享驱动子进程的托管、引用计数回收 | compose 独立服务；驱动子进程是崩溃隔离边界，有意不合并 |
| `compute/` | Dockerfile 构建受控执行隔离对：`wasm-worker`（最小特权 Wasm 执行）+ `code-builder`（唯一允许创建离线编译 guest 的固定命令驱动） | compose worker + builder 两服务；合并即削弱受控执行边界，有意保留 |

## 运维工具（不进部署拓扑）

| 程序 | 职责 |
|---|---|
| `storage-files/` | 停机状态下校验整个文件存储（恢复核验） |
| `rule-browser/` | 以与宿主同一受信规则实现浏览规则求值（诊断） |
| `webhook-sink/` | 本地栈与演练的 webhook/SMTP 接收端；B2 收编为 platform-server 可选能力后，独立部署退役 |

## 代码生成与检查工具（开发机）

| 程序 | 职责 |
|---|---|
| `api-types/` | 从宿主 API 生成 `web/packages/kernel/src/gen/host.ts` |
| `check-boundaries/` | 源码导入边界检查（含 build tags 排除的代码） |
| `uaf-gen/` | 从保留的 OMG 原件生成 UAF 运行元模型 |

## 驱动程序（由 gateway 托管的子进程程序）

`drivers/{modbus,s7,simulator}` 的入口在 `driver-*/`：它们不是独立部署服务，由 gateway 按会话启动与回收；部署面计入 gateway。新增驱动时在此登记，不新增部署服务。
