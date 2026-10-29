# 本地运行与静态系统维护

平台提供单一工作区与独立的静态监督工具。原 `#/self-hosting` 路由保留为 **System history / 系统历史**：只读查看源码、变更、发布和原激活结果，并按原请求键核对未知回执。全量动态自举的新源码保存、提案、批准、构建和激活已冻结；平台代码通过普通静态部署更新。
架构与数据归属见 [Architecture](../../docs/Architecture.md#9-自举system-作用域)，当前待验收项见 [WorkQueue](../../docs/WorkQueue.md)。本文说明运行方式；本轮实际观察与未验证项见 WorkQueue，不把历史演练当作当前交付。

## 启动工作区

需要 Go 1.27.1、Node ≥ 22.18.0、固定 pnpm 12.5.1，以及运行中的 Docker。

```bash
make setup PNPM="corepack pnpm"
make bootstrap
make docker
```

唯一产品入口为 **http://localhost:8495**，平台页面、应用、API 与登录共用此地址。Docker 模式只发布这一端口；PostgreSQL、Rauthy、RustFS 和计算服务在内部网络运行。无需访问单独的登录端口，也不为业务应用建立端口或专用容器。

首次 `make bootstrap` 生成新的空白租户、账号密码和各服务独立的随机密钥。账号密码见本地私有文件 [credentials.md](../../.build/local/credentials.md)，机器可读对应文件为 `.build/local/accounts.json`；它们权限为0600，不进入版本库。

| 账号 | 用途与初始权限 |
|---|---|
| `admin@platform.test` | 平台、AI、企业与工作治理，发布资格；独立配置的宿主管理员 |
| `builder@platform.test` | 构建与集成；没有宿主管理资格 |
| `user@platform.test` | 普通测试成员；没有默认业务角色 |

三个账号密码各不相同，均只供本机开发测试。Rauthy管理员可能在首次登录时要求完成个人资料；其管理后台也通过同源 `/auth` 提供。账号没有预装业务包、业务授权、AI提供方或示例数据；按实际测试需要显式配置。

实际配置在忽略的 `deploy/local/.env` 与 `.build/local/`：包含原租户文件、Rauthy bootstrap、账号说明和主密钥。重复 `make bootstrap` 只验证并复用，不覆盖密钥；文件缺失或不一致会拒绝继续，先恢复原文件或明确执行新的整体重建，不能混合旧数据和新密钥。

日常命令：

```bash
make local         # 切到一个 Air API + 一个 Vite 的热更新模式，入口仍8495
make docker        # 切回Docker单入口，保留数据和密钥
make local-update  # 交付当前已提交版本，检查版本、健康、同源登录和页面资源
make infra-down    # 停止容器，不删除卷
```

热更新使用回环18495作为内部API，原生开发 overlay 才向本机回环映射数据库5433、身份服务8480和对象存储9000，浏览器继续只访问8495。`make local` 由同一监督器启动和停止原生Wasm worker、代码构建器、Air和Vite；计算通信仅使用 `.build/dev/compute/` 中的本机Unix socket，不从Docker虚拟机映射socket。代码构建器仍调用固定摘要的隔离编译容器，镜像配置复用原Compose声明；Docker部署中的独立计算容器保持原隔离。

`make dev` 与 `make web` 可分别运行API和Vite；完整计算环境使用 `make local`，单独运行API时仅使用明确注入的计算socket，不根据遗留socket文件自动启用。`make build-compute` 只编译原生计算程序，不留下后台进程。服务日志在 `.build/dev/`，Compose 项目为 `platform`，卷以本地生成的 `PLATFORM_DATA_NAMESPACE` 命名。

`make lite` 是独立的无Docker轻量运行形态，默认8495，数据在 `.build/lite/platform/`，沿原签名角色卡登录；它不是上述密码登录环境，不自动启动updater或监督换版。不要与另一个平台同时占用同一端口。

## 推断部署许可

云端生成只允许获权设计任务；生产Chat、Agent、AI函数和在线知识嵌入需要部署认可的本地或边缘服务。提供方的 `kind=local`、模型名称和客户端阶段标记均不授予该许可。部署启动时从 `PLATFORM_AI_INFERENCE_POLICY` 读取明确的提供方ID与HTTP源站，默认没有运行态许可：

```json
{"runtime":[{"provider":"factory-inference","origin":"http://inference.internal:8000","location":"edge"}]}
```

以上是配置格式示例，不是本机已有服务。`provider` 必须与AI配置中的实际ID一致，`origin` 只有协议、主机和可选端口，不含路径、凭据或查询参数；`location` 仅接受 `local` 或 `edge`。在原维护会话的启动环境中导出实际审阅后的JSON，例如 `export PLATFORM_AI_INFERENCE_POLICY='<实际JSON>'`，再沿原更新方式重启生效；需要持久化时使用部署管理器的环境配置。不要编辑bootstrap生成并校验摘要的`deploy/local/.env`，也不要把许可保存到应用包或动态设置。Docker与原生模式都传递该变量，不会自动认可本地提供方。

请求会固定模型、阶段、源站及策略摘要；许可或提供方地址变化后，原在途调用明确停止，不能静默更换默认模型。已接受结果仍恢复原映像。此许可核对的是部署认可的调用边界，不宣称能从服务URL证明其物理硬件、模型参数量或成本。

## 静态控制与历史维护

租户、身份、凭据、AI模型配置与宿主恢复使用原静态控制面板及原授权，不依赖动态Page/App。业务应用继续在业务工作区安装、生成草稿和发布；安装后仍需明确授予使用者业务角色。系统工作区的角色不会带入业务工作区。

既有 `system` 租户、原席位、已接受结果和不可变定义安装映像保留。原 `builder`/`publisher` 角色不再开放新的动态源码或定义自举。恢复只应用原接受映像，不重跑构建或向模型请求代码。`-source-root` 仍可读取种子或封存源码，返回的所有文件均只读，凭据、Git状态及缓存不进入源码索引。

工作区保留旧文件内容、冻结差异、发布和激活证据。请求结果未知时只核对原请求键，不复制成新提案或激活。`/healthz` 的实际代码发布号与摘要独立于记录中的已请求状态；收到请求回执不等于版本已切换。

### 保留结果与监督器

`platform-updater build/activate/watch` 和 `serve -watch` 已停用。使用原发布者令牌处理既有维护责任，令牌沿 `PLATFORM_TOKEN` 注入，不进入包、源码或文档：

```bash
cd capabilities/server
go run ./cmd/platform-updater reconcile -root /absolute/path/to/platform -host http://127.0.0.1:8495
go run ./cmd/platform-updater serve -root /absolute/path/to/platform -profile lightweight -data /absolute/path/to/data -tenants /absolute/path/to/tenants.json -addr 127.0.0.1:8495 -host http://127.0.0.1:8495
```

`reconcile` 是显式的一次性核对，不订阅新工作。它按原请求键续报已留存构建结果；原批准工作没有构建制品时报告未开始并终结失败，不运行编译器。已有 requested 激活不能仅凭缺少选择文件推断未执行：先核对实际运行版本与证据，再显式选择或回报；不自动改为取消或失败。

`serve` 继续使用原 `.build/system/current.json`、数据目录、租户配置和部署参数监督已选择版本，保留健康检查、排空、原未知回执与回滚前驱。额外宿主参数放在 `--` 之后；不能覆盖原发布、摘要、源码或数据身份。已有版本重启失败时仍保留前驱和维护状态，不自动创建新的动态回滚请求。

只有核实原激活身份、实际版本和保留制品后，才使用 `swap -activation ACTUAL_ACTIVATION_ID` 处理原请求，或用 `finish -activation ACTUAL_ACTIVATION_ID -result active|rolled-back|aborted -evidence TEXT` 如实回报其结果。这些命令不创建新激活。缺少或冲突的证据必须保留并交给维护者，不删除 `current.json`、源文件、制品、密钥或数据来解除阻塞。

## 源码导出与备份

```bash
make system-export RELEASE=17 OUT=/absolute/path/to/export
```

发布号须来自本环境有本地制品清单的实际代码发布。导出读取发布绑定的完整封存源码并校验每个文件，不依赖临时工作树；纯定义发布引用同一代码基线，不生成独立二进制或本地构建清单。没有完整源码的旧代码发布会明确失败。导出是源码，不包含数据库、密钥或可运行部署备份。

完整恢复须同时保留：

- 轻量数据目录的日志、对象字节、快照及 `idp.key`；交付模式则保留正式 PostgreSQL 和对象存储。
- `.build/system/sources/` 的完整封存源码，以及回滚需要的 `.build/system/{bin,web,releases,builds}` 与 `current.json`。
- 原租户配置、签名与凭据主密钥、部署引用及运行参数；它们不进入源码清单。

`/healthz` 可核对进程、发布号、二进制摘要和隔离租户数量；具体租户仍需获权调用 `/v1/health`。健康、源摘要、页面资源和实际操作都要核对，超时、重启或单一 200 不构成交付完成。

## 更新、重建与旧环境退出

日常更新必须保留 `.env`、私有配置目录、数据卷和租户身份。`deploy/local/update.sh` 要求源码已提交，沿选定Docker或原生模式更新，验证实际版本、租户健康、同源OIDC发现及页面字节；不能把进程重启当作完成交付。

本次 new start 经负责人明确授权，退出本项目旧容器、旧卷（包括历史review卷）、所有旧本地运行状态与恢复副本、旧账号/权限/AI配置和旧密钥。新环境从随机命名的空白租户开始；不迁移旧数据，不沿用酒店/制造账号或固定口令。平台源码、可安装声明包和其他项目资源保留。旧浏览器凭据不能登录新身份服务，未确认请求不能跨新租户身份恢复；若标签仍记着已退出的租户，退出登录再重新进入。

这不是普通启动的兜底行为。以后需要重建时，先停止当前平台并按实际Compose标签和挂载核对范围，再明确退出对应卷及 `.build`/`.env`，最后生成全新配置。禁止使用全局Docker prune；不得删除其他项目数据，不能为了修复恢复错误擅自重建。

`make rehearse` 使用独立临时PostgreSQL，验证当前组合、静态维护与检查点/恢复，不访问日常卷。轻量角色卡、自动浏览器的机制夹具与上述真实密码账号分别用于各自运行形态；自动测试账号不会进入本地IdP或授予真实业务权限。

历史候选的原始字节和摘要继续共同保留；不能把封存候选展开成可重排的SQL JSON，或为退出自举重写历史。模型配置仍从静态AI配置入口维护，新的AI阶段与预算政策按当前AI规范及实施状态生效。
