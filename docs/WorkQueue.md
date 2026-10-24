# 执行队列

只放当前要交付的工作。完成即移除；被替代的条目删除，不留记录。为什么做归 [Intent](Intent.md)，系统结构归 [Architecture](Architecture.md)，行为约定归 [OpenSpec 模块规范](../openspec/README.md)，工作方式归 [AGENTS](../AGENTS.md)。

## 1. 自举：系统修改自己（唯一当前目标）

平台要能让 AI 与人在运行时修改平台自身：AI 用一句提示词提出变更，人在工作区里审阅、批准并激活，宿主按新版本重启后继续服务同一份数据。这一块交付完之前，不加业务场景、不补行业样例。

已跑通的链路（2026-10-24，本机 `lightweight` 宿主，数据目录 `.build/drill/data`）：

| 步骤 | 实际命令 | 观察 |
|---|---|---|
| 提议 + 批准 | `system.change.propose` → `system.change.approve`（两个席位） | 变更冻结文件清单，智能体不能自批 |
| 构建 | `platform-updater watch -once` 或 `build -change` | 独立工作树，`go build` + `vite build`，产出 release 0005 |
| 激活 | `system.release.activate` | 可取消窗口；同时只有一个 `requested` |
| 换版 | `platform-updater swap` / 监督进程 | `/healthz` 报 `release:5` 与二进制 sha256；旧激活标 `superseded` |
| 拒绝坏版本 | `chg-drill-12` 构建出启动即退出的 0007 | `swap` 探活失败、拒绝安装；运行版本仍是 0006，激活可取消 |
| 拒绝坏数据版本 | 0010（删掉 files/relations 的树）在真实数据上隔离租户 | 监督进程判 `degraded`、回滚 0009 并把 `current.json` 改回 0009 |
| 删除后重建 | `chg-drill-11`（删掉 gateway/driver/webhook） | release 0006 从精简树构建成功并上线 |
| 旧租户名不再拒绝身份 | `chg-drill-23` → release 0016 | 浏览器带着旧租户名 `workspace` 请求时，宿主忽略它并返回实际服务的 `system`；`/v1/me` 对 `workspace`/`nope` 都回 200 |
| 一条命令走完 | `platform-updater watch -once`（批准 `chg-drill-26` 后） | 自己构建出 0017、把 `requested` 的激活换成 0017；监督进程事后把激活记为 `active`、上一版记为 `superseded` |
| 导出发布 | `make system-export RELEASE=15` | 按发布记录里每个冻结文件的 sha256 校验后，把整棵工作树复制到 `.build/system/exports/0015` |

已知缺口，按顺序补：

- **源码编辑入口缺前置条件**：`web/apps/workspace` 的 `Platform` 贡献需要有 `system` 角色的成员才出现，且现有 principal 只能在 system 租户内提交；登录到业务租户的操作者还看不到它。
- **定义类与代码类发布未分开**：目前所有发布都走构建；只改定义的发布仍会重新编译。
- **默认值未进变更**：`system.file` 只冻结变更显式列出的文件，仓库里其余文件用运行中的代码树播种，二者偏离时以种子为准。
- **删应用需要迁移**：删掉 `files`/`relations` 会让已有租户无法重放，监督进程会拒绝并回滚该发布；要删先做退役与迁移。
- **剩下两类删除不是清理**：`apps/build` 的 confirmation/dispatch 已经在事务引擎里（`transaction_steps.go`、`transaction_check.go`），页面模型的 `map`/`scene` 段由契约生成；两者都要先改规格与契约，属于独立的结构性 change（见 `platform-self-hosting` 的 1.3b/1.4 卡点）。能机械删的（gateway、drivers、catalog、模板、测试与 e2e）已经删完。
- **导出只读工作树**：`make system-export` 从 `releases/NNNN.json` 记录的构建工作树导出；工作树被清理后只能重编，不能导出（清单记录了摘要，不是文件仓库）。

## 2. 队列规则

- 一次推进一块；同一处失败两次即停，先说明原因再换方案。
- 只有当前交付阻断、已确认回归或负责人明确的新目标才能入队。
- 自举上线后不以测试数量或代码行数作为交付证据；以四次演练与实际操作结果判断。
