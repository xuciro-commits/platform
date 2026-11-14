---
name: platform-loop
description: Work-order loop for an executor model - claim the next roadmap row of its tier, do it, verify, commit, push and report in the fixed format. Use when the owner starts a model on the roadmap queue, e.g. "/loop /platform-loop 易" or "/platform-loop 难".
---

# Platform loop

参数：你承担的档位，`难` 或 `易`；没有参数时问负责人一次。每次调用是一轮；在 `/loop` 下一轮结束即排下一轮。先读 `AGENTS.md`。工单的“方向”由主会话定，不重议（docs/method.md 工单循环）。

## 一轮


1. 同步：`git pull --rebase origin main`，`make check`。不是自己造成的失败，写入汇报后停止。
2. 领取：自上而下取第一条本档、前置已完成、没有“进行中”或“阻塞”标记的行；有 `[独占]` 行在进行中时不领任何行，领 `[独占]` 行前确认没有别的行在进行中。在行尾写 `（进行中：模型名，日期）`，单独提交并推送（标题 `docs: 路线图——领取 U1.4`）；推送被拒说明别人先领了，rebase 后重选。
3. 读：AGENTS.md、行内列出的规范小节与文件。别的不读。
4. 做：只做“方向”允许的事，遵守 AGENTS.md 的硬规则。行内没定、又必须定的设计选择，按阻塞处理，不自行发明。
5. 验：跑完行内“验证”列出的全部档位。同一失败修两轮仍不过，按阻塞处理。
6. 交：更新被改变行为的规范与差距条目，下调 `scripts/shape-baseline.json` 中改善的值；路线图删除该行，父行的“已完成”补一句；按 AGENTS.md 的提交格式提交并推送。
7. 报：提交正文末尾加“汇报”段，并作为本轮最后一条消息原样输出。
8. 回到 1。没有可领的行时停止并说明原因。

## 验证档位

（命令都加超时；没运行的不得写成通过）：

| 档 | 命令 | 何时 |
|---|---|---|
| V1 基本 | `gofmt -l .`、`go build ./...`、`go vet ./...`、`make check`、`make test`、`go test ./host ./kernel/... ./services/...` | 每行 |
| V2 路径 | `make e2e SPEC=…` | 行内点名的验收 |
| V3 全量 | `make e2e`（全部用例）与 `make verify-sql` | 跨服务搬移、全树改名、接受路径或发布路径变化 |
| V4 并发 | `go test -race ./host -run 'TestInvariantK(1|2|3|13)' -count=1` | 动到 `kernel`、车道、决定、读取映像 |
| W 前端 | `make check-web`，改动包内 `pnpm exec tsc -p .` | 动到 `web/` |

`make e2e` 需要 `web/apps/workspace/dist`（没有就先 `cd web && pnpm build`）和空闲的 18496 端口；端口被占就等，不杀别人的进程。验收与不变量的断言不得为了通过而修改；必须改断言时按阻塞处理。

## 汇报

（固定格式，每项一行；“现在为真”写能被命令验证的陈述）：

```
汇报
行：U1.4 Error 进 platform
结果：完成｜部分完成｜阻塞
现在为真：……
数字：platform N 行；host N 行；租户字段 N；探测 N；不一致包 N
检查：V1 通过；V3 24/24、verify-sql 通过；V4 通过（199s）
未运行：无｜……
偏离工单：无｜……
新增行或差距：无｜……
待主会话决定：无｜……
```

## 阻塞、切分、并行

阻塞：只还原自己动过的文件，在该行下写 `阻塞（日期，模型名）：问题；证据 file:line；两个可选做法与建议`，提交推送，领下一条。主会话答复后删除阻塞标记。

`[难]` 的“诊断与切分”行不写代码，产出是路线图里的新行：同一格式，带档位、前置、方向、完成判据与验证档位。拆出的 `[易]` 行要写到不需要再做设计选择：文件清单、改法、完成判据都是确定的。一行的改动超过一次提交能审阅的范围（约 3000 行差异或 100 个文件，纯改名除外）时，先拆行再做。

并行：两个模型不共用一个工作目录。默认串行；要并行时各用 `git worktree add ../platform-档位 -b loop-档位 main`，推送用 `git push origin HEAD:main`，e2e 与 verify-sql 仍按上面的端口规则错开。

主会话审阅：`git log --format='%h %s%n%b' -n 20 | grep -A10 '^汇报'` 读汇报，`grep -n '阻塞' docs/roadmap.md` 读阻塞；只对 `[难]` 行与“偏离工单”不为“无”的提交读差异。
