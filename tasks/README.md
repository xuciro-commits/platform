# 多 Agent 协作

这份文档回答：五个 Agent 各管什么、任务怎样派、卡住时怎样互相给信号、怎样合进 main。
方向与阶段在 [plan](../docs/plan.md)，主线细目在 [roadmap](../docs/roadmap.md)，这里只管分工与信号。

## 五个 Agent

| 名 | 工作树与分支 | 管什么 |
|---|---|---|
| lead | 主目录，`main` | 灵魂与大局：plan、roadmap、功能矩阵、本目录的任务；派任务；B 级内核决定；事后审 main |
| ship | `.build/ship`，`ship` | 投产与客户：roadmap 阶段 3 的各行（PostgreSQL、重启与恢复、扩展、升级、开通） |
| studio | `.build/studio`，`studio` | 工作台：重型交互（画布、Flow、本体关系图、表达式编辑器）与它们背后的服务 |
| kit | `.build/kit`，`kit` | 能力与包：受管能力服务（文档渲染、连接器、通话）与产品包 |
| fire | `.build/fire`，`fire` | 救火：lead 派的急活、没人接的内核请求、main 变红时先修 |

- 名字就是工作树目录名与分支名；看板脚本按所在目录自动认人，主目录里是 lead。
- 负责人在看板上叫 owner，可以直接发消息。
- 车道按模块端到端划分：一个人从数据模型做到界面再到 trunk 用例，不按前端、后端分层接力。
- lead 做完功能矩阵后可以重划 ship、studio、kit 的范围；改了就改上表。

## 开一个车道

```bash
git worktree add .build/studio -b studio main
```

- 每个 Agent 一个工作树、一条同名分支。分支长期用：每天 `git rebase main`，做完一项就 land，land 之后它和 main 一样。
- `make` 按工作树目录名自动错开端口：ship 偏移 10，studio 20，kit 30，fire 40，lead 0（`TRUNK_PORT_OFFSET`）。
  `make trunk`、`make trunk-pg`、`make test-pg` 的宿主、页面与临时库因此互不干扰。

## 任务

- 一个任务一个文件：`tasks/<名>-<号>.md`，例如 `tasks/studio-3.md`。lead 新建并派给车道，执行者只改自己名下的文件。
- 做完的任务在合进 main 的那个提交里删掉；经验写进提交信息。不留历史。
- 车道内的小想法可以由执行者自己开任务（`<自己的名>-<号>`），并在看板上告诉 lead。
- 不属于任何车道的公共活（例如通用画布、拆分 i18n 词典），lead 写成 `tasks/open-<号>.md`，谁先 `board lock open-<号>` 抢到算谁的。

任务文件只写这几节，60 行以内：

```markdown
# studio-3 Flow 画布能连线、发布、真跑

车道：studio    依赖：R4（看板请求）或 ship-2（任务）；没有写“无”
能改：services/automate/**、frontend/src/pages/flow/**、frontend/e2e/flow.spec.ts
不碰：kernel/**、contract/**

## 要做到
- 一句话一个可验的结果，最后一条是 trunk 用例：`make trunk SPEC=flow` 里加的那一步

## 交接
- 卡在哪、改了什么约定、下一步（交给别人或换人接手时写）
```

- “要做到”写产品级的结果：界面上能走通、数据落盘、trunk 能验。“接口有了”“页面有了”不算。
- 一个 roadmap 项派给执行者时，任务文件写“做 roadmap 3.1c”，不抄 roadmap 的内容。

## 看板

看板放在所有工作树共用的 git 目录里（`.git/board/`），不入库、不提交、不会冲突，写进去别的 Agent 立刻读得到。
命令都是 `node scripts/board.mjs <命令>`，不带参数看用法。`events.log` 是全部留言的存档，只追加。

| 命令 | 做什么 |
|---|---|
| `inbox` | 打印上次之后发给你的新事件，再打印超时告警 |
| `say --to <名\|all> [--kind 类] [--ref 号] 文字` | 留言；类：say、assign、idea、block、review、merged、contract |
| `ask --to <名> --ref <任务号> 文字` | 开一个请求，打印请求号 R# |
| `take R# 接口草图` | 接下请求；同时接的只有一个成功 |
| `done R# --sha <main 上的提交> 怎么用` | 请求做完 |
| `lock <名>` / `unlock <名>` | 抢锁、放锁：`kernel`、`main`、`open-<号>`；lead 与 owner 能 `--force yes` 收回 |
| `land --ref <任务号>` | 在自己的分支上：过 `make check`，快进合进 main，推送，通知所有人 |
| `status` | 没完的请求、锁、最近十条事件、告警 |

什么时候看板（每次都要）：

1. 每一轮开始时 `inbox`。
2. 每次提交之后 `inbox`。
3. 宣布卡住或结束本轮之前 `inbox`。

- Claude 会话可以让 Monitor 跑 `board.mjs watch`，工作中途就收到新事件。
- 手上还有可做的任务就不结束本轮；卡住了换一个做。全做完才停。
- 已经停下的 Agent 不会自己醒；负责人对它说“看板”，它就去读 `inbox`，负责人不用转述内容。

## 卡住了怎样要别人帮忙

以要改内核为例：

1. ship 卡住：`board ask --to fire --ref ship-2 "records 要按类型给出变化计数"`，然后立刻换手上的另一个任务。
2. fire 到检查点看到请求：先 `board lock kernel`，再 `board take R5 "Store.Fingerprint(types...) (*Store, string, bool)"`。
3. ship 下一个检查点读到接口草图，照它写调用方，或者先用假实现把界面和测试做起来。
4. fire 合进 main 后 `board done R5 --sha <提交> "rebase 后调用 Fingerprint"`，`board unlock kernel`。
5. ship 读到 done，rebase 到 main，删掉假实现，回到卡住的地方。

- 请求默认发给 fire；只有 lead 能答的（B 级内核、方向、新依赖）发给 lead。
- 接口草图是关键：有了它，提问方在答复完成前就能接着干大部分活。
- 超时告警：请求开了 2 小时没人接，或接了 12 小时没做完，或锁持有超过 12 小时，`inbox` 会标 `ALERT`；lead 处理，lead 不在时负责人决定。

## 内核

内核指 `capabilities/server/kernel/**`、存储格式、接受路径与授权决定路径。同一时间只有一个人改：先 `board lock kernel`。

| 级 | 例子 | 谁做 |
|---|---|---|
| A：不改已有行为 | 加方法、加索引、性能修复、新的只读查询 | 持锁的车道自己做 |
| B：改语义 | 存储格式与格式纪元、接受映像、授权决定、协议版本 | lead 做，或负责人拍板后指派 |

- A 级单独一个提交，标题范围写“内核”；过全量 `make test`、相关 `make test RUN=K…` 与 `make trunk-pg` 才能 land。
- 不许绕开内核的临时办法，例如在服务里直接写存储。

## 合进 main

1. 在自己的分支上提交（提交钩子照常跑 `make check`，有 `frontend/` 时加 `make check-frontend`）。
2. `git rebase main`；热点文件按下面重新生成，不手工合。
3. 跑自己任务的 trunk 用例与相关测试。
4. `board land --ref <任务号>`：它拿 `main` 锁、再过一次 `make check`、快进合并、推送、发 `merged` 事件、放锁。
   main 在这期间动了，它会拒绝，回到第 2 步。

热点文件：

- `frontend/src/host/gen/` 与 `page_compat.json`：rebase 后 `make contract`。
- `scripts/shape-baseline.json`：取 main 的版本，再按 `make check` 的提示改到实际值。
- `frontend/src/i18n/*.json` 与 `i18n.ts`：两边新增的词条都留下。
- `docs/plan.md`、`docs/roadmap.md`、本文件：只有 lead 改；执行者要改，留言给 lead。

- lead 在主目录的 main 上直接提交并推送；它的提交也会让别人的 land 先 rebase。
- main 变红（检查不过）时，谁先发现谁在看板 `say --to all --kind block`，fire 先修。

## 负责人的想法

- 默认告诉 lead；lead 判断归哪条车道、拆成任务、在看板 `assign`。
- 只关一条车道的想法，可以直接告诉那个执行者；它先补任务文件，再 `say --to lead --kind idea`，然后再动手。
  一旦发现会碰到别的车道或内核，停下转给 lead。
- lead 不在时，任何 Agent 都可以替负责人记一条 `say --to lead --kind idea`，然后接着干自己的活。
- 改方向、阶段门槛、intent、新依赖的想法，lead 先问负责人。
