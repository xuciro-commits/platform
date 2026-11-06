---
level: 7 系统架构
covers: [.claude/hooks/**, .claude/settings.json, Makefile, web/e2e/**, capabilities/server/**/*_test.go]
read-when: 运行或新增检查、验收用例、钩子；判断一项改动需要哪些验证
---

# 7 系统架构 · 验证

## 目标

让“完成”可被机器判定。验证分四层：静态检查、钩子、内核不变量、黄金路径验收。只写这四层；不为单个函数写单元测试，不做界面快照测试。

## 规则

- R1 没运行的检查不能报为通过；报告写出实际运行的命令与结果。
- R2 必须恒成立的约束由钩子或用例保证，不靠文字提醒。
- R3 测试只有两类：`capabilities/server` 中的内核不变量测试，`web/e2e/` 中按 5-experience F1–F22 编号的验收用例。
- R4 验收在隔离的宿主上运行：内存日志、开发令牌席位（`web/e2e/tenants.json`），不依赖 `make local`、数据库或真实账号。

## 7.8 静态检查

| 改动 | 命令 |
|---|---|
| Go | `cd capabilities/server && go build ./... && go vet ./...`；`gofmt -l` 为空 |
| Go 包边界 | `node scripts/check-boundaries.mjs`：`go list` 的实际导入与 [README §7.2](README.md#72-包结构) 依赖列一致；不符的导入只能是该文件“现存差距”中登记的条目，条目消除后必须删除 |
| 宿主类型与页面契约 | `make contract` 前后 `kernel/src/gen/*.ts`、`pageui/safety.ts`、`platform/page_compat.json` 逐字节一致 |
| Web | 改动包内 `pnpm exec tsc -p .`；`cd web && pnpm build` |
| 文案 | `node scripts/check-i18n.mjs` 检查 `t("...")` 字面量与 `defineApp` 说明都在字典中；动态键取值齐全 |
| 全部 | `git diff --check` |

Go 命令设置 `GOPROXY=https://goproxy.cn,direct`，所有长命令加超时。

## 7.9 钩子

| 钩子 | 时机 | 作用 |
|---|---|---|
| `format.py` | 编辑或写入 `.go` 文件后 | `gofmt -w` 该文件 |
| `commit-gate.py` | 执行 `git commit` 前 | 对相对 HEAD 的全部改动（含未跟踪文件）：代码或 7-system §7.2 有改动时运行 `make check`，工作区内容与上次通过时相同则不重跑。暂存的代码命中某规范 `covers` 而该规范未暂存时，首次拦下并列出规范；暂存内容不变再次提交则放行（带 `-a` 提交时按全部改动计） |


钩子失败时以退出码 2 把原因交回代理；代理修正后重试，不绕过钩子。

## 7.10 内核不变量

位于 `capabilities/server/host/invariants_test.go` 及同目录的 `*_invariants_test.go`（夹具在 `invariants_harness_test.go`），只测这些性质：

| 编号 | 不变量 |
|---|---|
| K1 | 重放：同一日志重放两次得到逐字节相同的记录存储；源与管道的删除、属性回退和恢复保留接受结果；旧 core 种子只在原 SQL 状态已有对应记录时恢复，新租户不生成 |
| K2 | 幂等：同一幂等键的重复提交返回同一结果，不产生第二条接受结果 |
| K3 | 修订：期望修订不符的提交被拒绝且封存拒绝 |
| K4 | 授权：被拒绝的读取与写入带原因；标记未获准时集合读取过滤、单条读取拒绝；资源发现不授予定义读取；专项许可、团队授权、解除失权与其重放保持一致 |
| K5 | 血缘：标记沿管道与数据源传播到对象类型 |
| K6 | 分支：分支保存不改变 main；合并是单个接受结果 |
| K7 | 产品版本：只从已保存候选冻结 Contents 闭包；版本不可替换，产品后续编辑不改写版本字节，Marketplace 目录消费同一版本 |
| K8 | 系统自举：`system` 租户的发布记录只读；保存新候选与激活都被拒绝，已有候选仍可审查 |
| K9 | 产品包生命周期：退役归档本包资源；同版本重装恢复本包资源、应用身份和客户记录；升级、降级、回滚分别执行，存储兼容检查阻止旧定义无法读取的数据形状；安装闭包按精确版本显示 `installed / pending / failed` 且不重复提交已安装步骤；重放保持结果 |
| K10 | 资源所有权 rehome：管理员接受结果只改变 `Project / ManagedBy` 和修订号，RID、类型、名称、记录和重放结果不变；非管理员拒绝，陈旧修订拒绝 |
| K11 | 运营观测：租户与成员范围不可越过；日志读写不改变接受结果或记录；保留与覆盖边界准确；日志查看产生审计；请求关联来自显式上下文，不来自可变租户状态；运行索引按成员可见范围隔离且不改变 owner 状态 |
| K12 | 血缘：Guard 不因保存但未激活的定义改动降低，且不低于写出证据；项目委派构建者的血缘响应不含其他项目资源的身份；裁剪不改变标记与影响计数；截断、不可读、悬空引用都被计数；同一对资源的多条边保持独立身份；FormulaExpr、join/lookup 的字段来源与回写请求/结果可追踪；不可读接受定义 fail-closed |

资源入口有独立宿主验收：直接检查 page/default/release-plan 三种 route，`build.testplan` 的 `test-plan` / `evaluation-set` 目录引用与 `variant`，以及两种 release kind 的发布预览结果；同时检查 `work.view` 的 v0/v1 状态和 `build.query` 的 v1 ObjectSet 发布合同。这些断言读取实际映射与发布结果，不以生成的前端契约反推。

## 7.11 黄金路径验收

- 位置：`web/e2e/F<n>-<名称>.spec.ts`，Playwright；配置每次默认在 18496 启动一个全新宿主并加载工作区构建，`PLATFORM_E2E_PORT` 可指定独立端口；不复用已有宿主（复用会带入别的运行留下的草稿）。
- 登录：`kit.ts` 的 `signIn(page, 主体)` 以开发令牌进入；席位为 admin、builder、user、publisher，以及 F20 使用的 cleared。
- 每个用例对应 5-experience 的一条任务路径，步骤与成功标准逐条断言。
- 用例在空项目中创建自己的资源，结束时归档；不依赖其他用例的数据。
- 选择器使用可访问名称（角色 + 名称），不用 CSS 类。

## 7.12 命令分层

| 命令 | 内容 | 预算 | 何时 |
|---|---|---|---|
| `make check` | 7.8 全部静态检查、包边界、内核不变量（`scripts/check-invariants.mjs` 每个用例一个进程并行，最慢的先跑）、契约逐字节一致 | 60 秒 | 提交钩子在代码改动时自动运行 |
| `make e2e SPEC=F17` | 指定的黄金路径用例 | 按用例 | 批次结束前，跑改动路径上的用例 |
| `make verify` | `check` 加全部黄金路径用例 | 数分钟 | 改动共享外壳、宿主装配、契约或多条任务路径时 |

检查只在本机运行，没有远端 CI；钩子是唯一的强制关口。`make check` 超出预算时，先找出最慢的一项并让它变快，不把检查移出 `check`。

## 7.13 SQL 模式

`make verify-sql` 在本机 PostgreSQL 容器中创建隔离临时库，启动独立宿主，验证项目委派构建者创建对象、viewer/discoverer 的资源目录权限、运营日志、不可变审计、运行索引、停止并重启宿主后的读取，最后删除临时库。它验证 SQL 接受结果、项目权限和启动恢复，不替代 `make verify` 的完整 Playwright 路径，也不把内存测试当成 SQL 覆盖。

## 现存差距

- `make verify` 的内核不变量与 F1–F22 仍运行在内存宿主；SQL 模式覆盖创建、项目委派、viewer/discoverer 目录权限、日志、审计、运行索引和重启读取，尚未把全部 Playwright 用例跑在 PostgreSQL 上。
