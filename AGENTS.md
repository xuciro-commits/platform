# AGENTS.md

一个业务应用平台：声明业务对象，就得到能用的应用；行业知识做成能安装、能升级的产品包。
内核的模型向 Palantir Foundry 看齐，应用框架的体验要胜出 Frappe（[intent](docs/intent.md)）。

本文件每次会话都加载；其余文档按下表在需要时读，只读相关的一节。文档写的是目标：代码与文档冲突时改代码。

## 开工前

1. 认自己：主目录里是 lead，`.build/<名>` 里是那个执行者：ship、studio、kit 或 fire（[tasks](tasks/README.md)）。先跑 `node scripts/board.mjs inbox`。
2. 看 `git status`。只动自己留下的改动；有别人的未提交改动，在看板上告诉 lead：不接着写，不替别人提交，不整理。
3. lead 读 [plan](docs/plan.md) 与 [roadmap](docs/roadmap.md) 定下一步并派活；执行者做派给自己的 `tasks/<名>-<号>.md`，一次一项。
4. 读这一项涉及的文档小节、实现与调用方，再动手。
5. 读任何未知前，先猜测，think中以形式化的：想必是XXXXX体现出来；读完后核对猜测，自信+分-分；大幅提升自己的猜测能力。

## 何时读什么

| 任务涉及 | 读 |
|---|---|
| 做什么、先后、什么先不碰 | [plan](docs/plan.md) |
| 当前阶段每一行的细目与验收 | [roadmap](docs/roadmap.md) |
| 谁管哪块、任务文件、看板、卡住怎样求助、怎样合进 main | [tasks](tasks/README.md) |
| 该不该做、属不属于平台、八个承重决定 | [intent](docs/intent.md)（产品真值见 [PRODUCT.md](PRODUCT.md)） |
| 写入、读取、发布各走哪条路；并发；版本与纪元；怎样算对 | [architecture](docs/architecture.md) |
| 资源、类型、对象、动作、权限、产品包 | [domain](docs/domain.md) |
| Go 包、代码规范、内核不变量、后端易错点 | [backend](docs/backend.md) |
| 界面代码：三层、表现、Astryx 的用法、核对、前端易错点 | [frontend](docs/frontend.md) |
| 导航、四种页面、保存模型、文字 | [ux](docs/ux.md) |
| 观感、数据怎样显示、什么不许出现 | [design](docs/design.md)（令牌与规约见 [DESIGN.md](DESIGN.md)） |
| 命名与翻译 | [glossary](docs/glossary.md) |
| 文档的规矩、谁决定什么、一行怎样算做完 | [docs/README](docs/README.md) |

改界面之前用技能 `platform-design`（设计打磨可配合 `impeccable`，读 [PRODUCT.md](PRODUCT.md) 与 [DESIGN.md](DESIGN.md)）。

## 硬规则

方向：

- 八个承重决定不动：一条写入路径、一套模型、一个进程、一种交付物、一个授权决定点、AI 是调用者、一个格式纪元、一个界面运行时。
- 只做 plan 当前阶段的行，按 roadmap 的细目做；其余没有功能在等的搬迁与改名保持冻结。
- 只有一个前端：`frontend/`。
- 界面由类型生成：不为单个类型写界面，不从空白写页面；先找 Astryx 官方模板与块，改造一次，所有类型共用。
- 平台不含行业代码；行业概念只在产品包里用对象与动作表达。
- 一项能力只有一个实现；规则求值、约束、授权、依赖解析各只有一个。

代码：

- 先读实现与调用方，从根因解决；删除被替换的路径；不扩展无关功能。
- 最小完整改动：能不写就不写，能复用就复用，删除优先于新增；不为只有一个实现的东西写接口，不留兼容分支。
- 校验只在信任边界；边界之内信任类型与不变量，不为不会出现的状态写分支，不吞错误换默认值。
- 权威状态的新写入只经接受结果；跨应用走版本化协议，不做跨权威原子性。
- 租户扩展只经受控编译、隔离执行和版本化制品。
- 不写代码注释；编译指令、生成文件头、shebang 保留。
- 测试只写两类：内核不变量与主干验收；不为单个函数写单元测试。
- 界面保留英文与简体中文；业务数据不翻译。

安全与数据：

- 不提交凭据；应用只保存凭据引用。不读取、不打印 `.build/local/` 中的账号与密码。
- 保留现有数据卷、密钥、租户配置与当前格式纪元内的历史字节（architecture“版本与纪元”）；恢复只应用已接受映像。
- 保留工作区里他人的改动；没运行的检查不能报为通过。

不留垃圾：

- 未经负责人决定不新建文档；`docs/` 只保留文档地图里的文件，产品进度写在 plan，当前阶段的细目写在 roadmap，做完的删掉。
- 不写历史、总结与方案；任务分解只写在 roadmap 与 `tasks/`，做完即删。
- 代码改变了文档写着的行为，同一提交改文档。
- 仓库里不留临时文件、调试输出、备份副本、空壳和“以后再用”的代码；草稿放系统临时目录。
- 不改检查脚本来让检查通过；被提交关口拦下时修正后重试，不绕过。

## 命令

| 用途 | 命令 |
|---|---|
| 前端 | `pnpm --dir frontend dev`（http://localhost:8496，代理到 `PLATFORM_HOST`） |
| 夹具宿主 | `make frontend-host`（内存宿主 http://127.0.0.1:18511，自带示例销售项目） |
| 整套本地运行 | `make local`（入口 http://localhost:8495 是 `frontend/`；宿主日志 `.build/dev/platform-api.log`） |
| 快速检查 | `make check`：Go 构建、vet 与格式，包边界与形状，产品包，契约，文案，文档，空白 |
| 前端检查 | `make check-frontend`（改了 `frontend/` 时） |
| 主干验收 | `make trunk`：自己起宿主、灌数据、起前端，浏览器走一遍演示路径；`HEADED=1` 看着它走 |
| 宿主类型与契约 | `make contract`（改了宿主类型之后） |
| 内核不变量 | `make test RUN=K13` 定点跑相关编号；不给 `RUN` 跑全部 |
| SQL 模式 | `make verify-sql`（隔离的临时 PostgreSQL：写入、重启、读取） |
| 产品包 | `make packages`（刷新作者态源的锁文件，并把夹具一起装进内存租户检查；顶层制品是发布快照，不重建）；试跑动作用 `pkg try` |

提交钩子（`.githooks/`，通过 `make hooks` 启用）自动跑 `make check`，暂存里有 `frontend/` 时加跑 `make check-frontend`，并用 `scripts/check-commit-msg.mjs` 拦截不合规范的提交信息。

## 工作方式

- 负责人掌舵；lead 管大局并派活，ship、studio、kit 各管一条车道，fire 救火（[tasks](tasks/README.md)）。负责人看两样东西：plan 里的那一行，和浏览器里的界面。
- 一项任务由领到它的 Agent 端到端做完：设计、实现、核对、提交。车道内可派子代理查资料、跑检查；不把半截活交给另一个 Agent 接力。
- 每轮开始、每次提交后、结束本轮前跑 `board inbox`；手上有活就不停，卡住换一项做。
- 每项做完：检查，提交，`node scripts/board.mjs land` 合进 main 并推送，汇报。提交只暂存自己改的路径，不用 `git add -A` 与 `git commit -a`。
- 提交信息用详细中文（由 `.githooks/commit-msg` 强制检查）：标题写 `类型: 范围——行为变化`（类型限 `功能/修复/优化/性能/重构/文档/验收/清理/合并`）；正文空一行，必填 `范围与行为变化` 与 `检查` 两节（每节用 `- ` 列具体条目，按需补充 `替代与删除`/`文档`/`性能对比`）；末尾空一行写署名尾注 `Co-Authored-By: 名字 <邮箱>`（例如 `Co-Authored-By: Gemini <noreply@google.com>`），写这次提交的 AI 署自己，几个参与者各写一行。未运行的检查不得写成通过，不写空泛总结。
- 汇报只说三件事：现在能做什么（附能点开的地址）、什么没做或没核对、要负责人决定什么。

## 易错点

- zsh 里 `ls` 被别名为 eza，用 `command ls`；通配符加引号，没有匹配的通配符会让整条命令不执行。
- 长命令一律加超时，例如 `perl -e 'alarm 400; exec @ARGV' go build ./...`。Go 模块经 `GOPROXY=https://goproxy.cn,direct`。
- 在 `frontend/` 里不带参数跑 `astryx init` 会改写本文件；只用 `pnpm astryx init --agent-docs-path AGENTS.md`。
- 本地与夹具数据可随时清空重建；不要为修启动问题删除配置或数据卷。
- 后端的易错点在 [backend](docs/backend.md)，前端的在 [frontend](docs/frontend.md)；动那一侧之前先读。
