# Platform · 业务应用构建平台

面向 FDE 与客户构建者，提供数据接入、语义建模、动作/流程/AI、操作界面、受控发布与后续修改。语言中立的内核契约在 contract/，Go 宿主在 capabilities/server/，Web 工作区在 web/；apps/ 是原生参考应用，solutions/ 负责组合宿主。

产品宗旨见 [Intent](docs/Intent.md)，架构与支持范围见 [Platform](docs/Platform.md)，当前工作见 [WorkQueue](docs/WorkQueue.md)。开发从 [AGENTS](AGENTS.md) 与 [ADR-0124](docs/ADR/0124-platform-consolidation.md) 开始。

## 快速启动

需要 Go 1.27.1、Node.js ≥ 22.18.0；Web 固定使用 pnpm 12.5.1。先安装 Web 依赖：

~~~bash
cd web
corepack pnpm install --frozen-lockfile
cd ..
~~~

无 Docker 的持久化运行：

~~~bash
make lite
make lite SOLUTION=manufacturing
# 只有 Corepack 时加 PNPM="corepack pnpm"
~~~

酒店入口为 http://localhost:8495，制造入口为 http://localhost:8490。工作台提供本机签名的演示角色卡，无需密码。每次启动构建对应工作台与宿主，数据保存在 .build/lite/<solution>/；备份前停止宿主，并一起保存日志、文件和签名密钥。租户 Go/TinyGo 编译和 Wasm 执行仍需独立计算服务，未配置时明确拒绝。

全栈开发需要 Docker/OrbStack、Python 3；首次准备 Air、Web 与浏览器依赖后启动：

~~~bash
make setup
make local
~~~

make local 在后台运行 Air/Vite，复用原数据卷；日志在 .build/dev/。make docker 切回容器宿主。账号、服务地址、外部密钥、存储和两种运行配置的完整说明只在 [本地环境指南](deploy/local/README.md) 维护。

## 开发与检查

~~~bash
make help
make check
make test-go PKG=./platform RUN=TestName
make e2e SPEC=page-notice
~~~

命令定义归 [Makefile](Makefile)，检查范围和结果含义归 [Testing](docs/Testing.md)。应用使用与构建步骤见 [Apps](docs/Apps.md)；发布、演练和备份见 [本地环境指南](deploy/local/README.md)。架构决定按 [ADR-0124 的主题入口](docs/ADR/0124-platform-consolidation.md#12-现行主题入口) 查阅。
