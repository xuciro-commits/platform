# AGENTS.md

业务应用构建平台，以 Palantir Foundry 为基准。终局：先把平台做成，再用平台自己的工具构建行业应用，经 Marketplace 交付到客户租户（[1-enterprise](docs/1-enterprise.md)）。

规范领导代码：`docs/` 写目标设计，代码与规范冲突时改代码。本文件每次加载；其余文档按下表在需要时读，只读相关小节。文档体系本身见 [docs/README](docs/README.md)。

## 何时读什么

| 任务涉及 | 先读 |
|---|---|
| 选下一批、记录新差距 | [roadmap](docs/roadmap.md) |
| 命名、翻译 | [glossary](docs/glossary.md) |
| 判断是否属于平台、终局、质量目标、外部依赖 | [1-enterprise](docs/1-enterprise.md) |
| 新能力或资源类型放哪个应用、平台还是产品包 | [2-product](docs/2-product.md) |
| 导航、门户、资源分类、URL、Search、Files | [3-information](docs/3-information.md) |
| 页面原型、保存模型、选择、窗口、快捷键 | [4-interaction](docs/4-interaction/README.md) |
| 某个界面组件或交互模式 | [4-interaction/patterns](docs/4-interaction/patterns.md) |
| 某个工具应用的布局 | [4-interaction/workbenches](docs/4-interaction/workbenches.md) |
| 任务路径、文案、启发式检查 | [5-experience](docs/5-experience.md) |
| 资源内核、本体、对象、数据、动作、函数、自动化 | [6-domain](docs/6-domain/README.md) |
| 授权、项目角色、标记、行列策略 | [6-domain/security](docs/6-domain/security.md) |
| 分支、提案、产品、版本、安装、升级 | [6-domain/change](docs/6-domain/change.md) |
| Go 包、写入与读取路径、运行机制、协议 | [7-system](docs/7-system/README.md) |
| 前端包、路由、数据获取、i18n | [7-system/web](docs/7-system/web.md) |
| 检查、钩子、验收用例 | [7-system/verification](docs/7-system/verification.md) |

前端改动另用技能 `platform-design`；一批完整交付用技能 `platform-batch`。

## 硬规则

- 先读实现与调用方，从根因解决；删除被替换的路径；不扩展无关功能。
- 新能力先在 2-product 找到唯一归属；找不到就先改规范。一项能力只有一个实现，一类资源只由一个工具应用编辑。
- 代码改变了某份规范描述的行为，同一提交更新该规范（规则、规格或现存差距）。
- 不写代码注释；编译指令、生成文件头、shebang 保留。
- 测试只写两类：内核不变量与黄金路径验收（7-system/verification）；不为单个函数写单元测试。
- 权威状态的新写入只经接受结果；跨应用走版本化协议，不做跨权威原子性。
- 应用只导入 `platformserver/platform`；客户端复用现有共享包；规则求值、约束、授权、依赖解析各一个实现。
- 平台不含行业代码；行业概念只在产品包中以本体与应用表达。
- 租户扩展只经受控编译、隔离执行和版本化制品。
- 界面保留英文与简体中文；业务数据不翻译；工具应用名用 Palantir 产品名，不翻译。
- 保留现有数据卷、密钥、租户配置与历史字节；恢复只应用已接受映像。
- 不提交凭据；应用只保存凭据引用。不读取、不打印 `.build/local/` 中的账号与密码。
- 保留工作区里他人的改动；没运行的检查不能报为通过。
- 新依赖须负责人批准。

## 命令

| 用途 | 命令 |
|---|---|
| 本地运行 | `make local`（入口 http://localhost:8495，热更新；宿主日志 `.build/dev/platform-api.log`） |
| Go 检查 | `cd capabilities/server && GOPROXY=https://goproxy.cn,direct go build ./... && go vet ./...`；`gofmt -l .` |
| 宿主类型 | `cd capabilities/server && go run ./cmd/api-types`（生成 `web/packages/kernel/src/gen/host.ts`） |
| Web 检查 | 改动的包内 `pnpm exec tsc -p .`；`cd web && pnpm build` |
| 空白 | `git diff --check` |

长命令一律加超时（如 `perl -e 'alarm 400; exec @ARGV' go build ./...`）。

## 工作方式

- 每批按技能 `platform-batch` 交付，结束时提交并推送 main；角色与模型分工见 docs/README §7。
- 负责人决定方向与持久代价的取舍，并在浏览器走查观感；其余顺序由代理决定。
- 钩子在 Go 编辑后格式化，并在 `git commit` 前检查构建、类型、空白与规范同步；被拦下时修正后重试，不绕过。
- 项目知识只写进仓库；外部评审不入库（docs/README §6）。

## 易错点

- HTTP 读取记录的路由必须用 `rt.recordRead`；`rt.handle` 读记录会因“no consistent view”崩溃。
- 授权的集合读取与单条读取都经 `authorizeDepth`；`held=true` 时调用者已持有存储锁，不得再加锁。
- `t()` 在模块加载时求值的文案切换语言后才生效；动态键要在字典里列全取值。
- zsh 中 `ls` 被别名为 eza，用 `command ls`；通配符加引号。
- 本地数据可随时清空重建；不要为修启动问题删除配置或数据卷。
