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
- R3 测试只有两类：`capabilities/server` 中的内核不变量测试，`web/e2e/` 中按 5-experience F1–F10 编号的验收用例。
- R4 验收在隔离的宿主上运行：内存日志、开发令牌席位（`web/e2e/tenants.json`），不依赖 `make local`、数据库或真实账号。

## 7.8 静态检查

| 改动 | 命令 |
|---|---|
| Go | `cd capabilities/server && go build ./... && go vet ./...`；`gofmt -l` 为空 |
| 宿主类型 | `go run ./cmd/api-types` 后生成文件无意外差异 |
| Web | 改动包内 `pnpm exec tsc -p .`；`cd web && pnpm build` |
| 文案 | 新字符串在 `zh-CN` 字典中；动态键取值齐全 |
| 全部 | `git diff --check` |

Go 命令设置 `GOPROXY=https://goproxy.cn,direct`，所有长命令加超时。

## 7.9 钩子

| 钩子 | 时机 | 作用 |
|---|---|---|
| `format.py` | 编辑或写入 `.go` 文件后 | `gofmt -w` 该文件 |
| `commit-gate.py` | 执行 `git commit` 前 | 对相对 HEAD 的全部改动（含未跟踪文件）：Go 文件 gofmt 与 `go build ./...`；改动的 TS 包 `tsc`；`git diff HEAD --check`。改动的代码命中某规范 `covers` 而该规范未改时，首次拦下并列出规范；内容不变再次提交则放行 |

钩子失败时以退出码 2 把原因交回代理；代理修正后重试，不绕过钩子。

## 7.10 内核不变量

位于 `capabilities/server/invariants_test.go`（夹具在 `invariants_harness_test.go`），只测这些性质：

| 编号 | 不变量 |
|---|---|
| K1 | 重放：同一日志重放两次得到逐字节相同的记录存储 |
| K2 | 幂等：同一幂等键的重复提交返回同一结果，不产生第二条接受结果 |
| K3 | 修订：期望修订不符的提交被拒绝且封存拒绝 |
| K4 | 授权：被拒绝的读取与写入带原因；标记未获准时集合读取过滤、单条读取拒绝 |
| K5 | 血缘：标记沿管道与数据源传播到对象类型 |
| K6 | 分支：分支保存不改变 main；合并是单个接受结果 |

## 7.11 黄金路径验收

- 位置：`web/e2e/F<n>-<名称>.spec.ts`，Playwright；配置每次在 18496 启动一个全新宿主并加载工作区构建，不复用已有宿主（复用会带入别的运行留下的草稿）。
- 登录：`kit.ts` 的 `signIn(page, 主体)` 以开发令牌进入；席位为 admin、builder、user。
- 每个用例对应 5-experience 的一条任务路径，步骤与成功标准逐条断言。
- 用例在空项目中创建自己的资源，结束时归档；不依赖其他用例的数据。
- 选择器使用可访问名称（角色 + 名称），不用 CSS 类。

## 7.12 一条命令

`make verify`：静态检查 + 内核不变量 + 验收用例；批次结束前运行。

## 现存差距

- K4–K6 未写；验收只有 F1（路线图 P2.14）。F1 走无数据源的分支（P3.25），第 4 步用固定等待代替如实的保存状态（P3.21）。
