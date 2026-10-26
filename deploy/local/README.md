# 本地运行与系统自举

本分支提供 `platform-server`、单工作区和独立的 `platform-updater`。工作区的 **Self-hosting / 系统自举** 入口是 `#/sys`；页面中的 **Help: self-hosting / 帮助：系统自举** 可展开操作说明。源码保存、提案、批准、激活是不同步骤。

架构与数据归属见 [Architecture](../../docs/Architecture.md#9-自举system-作用域)，当前待验收项见 [WorkQueue](../../docs/WorkQueue.md)。本文说明运行方式；本轮实际观察与未验证项见 WorkQueue，不把历史演练当作当前交付。

## 启动工作区

需要 Go 1.27.1、Node ≥ 22.18.0、固定的 pnpm 12.5.1。在仓库根目录安装依赖：

```bash
make setup PNPM="corepack pnpm"
```

`make lite PNPM="corepack pnpm"` 会构建工作区和宿主，默认监听 `http://127.0.0.1:8495`，数据目录为 `.build/lite/platform/`。它保留日志、文件及签名密钥，以前台宿主直接提供服务；它没有自动启动 updater 或监督换版。`LITE_HOST=0.0.0.0` 可用于受信任网络预览。

自举环境须配置 `system` 租户和明确角色：构建者持有 `system:builder`，人工发布者持有 `system:publisher`；编辑元对象还需原 `build:builder`。人工发布者可以批准自己的人工提案；受管 AI 即使被错误授予发布角色仍不能批准，AI 提案须人工批准。初始席位只用于空日志，已有成员授权仍以接受结果为准；更改起始 JSON 不会重写既有授权。

使用独立数据目录启动源码可读的自举宿主：

```bash
make host ARGS='-profile lightweight -data .build/self-hosting/data -idp-new-key'
make host ARGS='-profile lightweight -data .build/self-hosting/data -tenants /absolute/path/to/tenants.json -source-root . -web web/apps/workspace/dist -addr 127.0.0.1:8495'
```

运行前须有 `web/apps/workspace/dist`；可在 `web/` 运行 `corepack pnpm --dir apps/workspace build` 构建。`tenants.json` 使用现有 `TenantSpec` 格式，包含上述 `system` 席位；不要把旧业务环境的租户文件当成自举角色配置。轻量身份沿该目录原签名角色卡登录，命令行调用可用同一宿主的 `-mint-token <subject>` 取得实际令牌。`-idp-new-key` 只用于首次建立目录，不能为日常启动替换已有签名密钥。

`-source-root` 可以是种子源码目录，也可以是已封存源码容器目录（包含 `manifest.json` 和 `files/`，不是 `files/` 本身）。初次从种子捕获完整源码，以后按摘要使用 `.build/system/sources/<digest>/`。它不开放任意主机文件读取；凭据、`.git`、`.build`、依赖和构建缓存被排除，冻结内核只读。

## 人工修改与 AI 修改

1. 选择获权的 `system` 租户，进入 `#/sys`，展开页内帮助。
2. 在 **System files / 系统源码** 按路径检索并打开源码；可手工输入新路径。编辑后保存，保存会检查读取时的修订，删除保存墓碑。失败或回执未明时保留输入；在 **Unconfirmed requests / 尚未收到回执的请求** 核对原请求，不复制成另一条请求。
3. 在 **Prepare a proposal / 准备变更提案** 选择已保存源码及 Build 草稿。草稿沿原记录入口创建、编辑，刷新列表后选入；纯定义提案可以没有源码文件。
4. 写明标题与理由后提案，由人工发布者审阅冻结内容并批准。后续改动不能悄悄替换已冻结提案。
5. 纯定义提案生成 `kind=definitions` 发布，点击激活即沿原 owner 热安装，不编译、不重启，也没有代码换版的可取消窗口。含源码的提案等待 updater 构建，再由发布者显式激活；混合提案的定义在代码换版成功时共同安装。
6. 代码激活在窗口内可以取消。窗口结束后由监督进程实际换版；界面的激活记录和 `/healthz` 是结果核对入口，按钮收到接受回执不等于换版已完成。

工作区初始化、每 30 秒及窗口重新聚焦时会核对实际代码发布号与二进制摘要。换版后出现手动刷新提示，当前输入保留；保存编辑后再点“Refresh the page / 刷新页面”。它不会自动重载整工作区，纯定义热激活也不会误触发代码换版提示。

**Ask for a change / 提出修改需求** 会启动真实 `system.architect`。它通过参数化源码读取工具查找和打开文件，使用原动作保存与提案；运行记录链接可核对实际工具轨迹。必须先配置真实可用的 AI 提供方和授权模型；固定替身、来源字段 `origin=prompt` 或仅创建 run 都不能证明模型完成了修改。

## 构建与监督换版

updater 使用 `PLATFORM_TOKEN` 中的实际 system 发布者令牌；不把令牌写进包、提案或源码。命令从仓库根执行，`-root` 使用本仓库的绝对路径：

```bash
cd capabilities/server
go run ./cmd/platform-updater watch -root /absolute/path/to/platform -host http://127.0.0.1:8495 -once
```

`watch` 构建已批准的代码提案并保留结果，再处理已经请求且窗口结束的激活。它不会替发布者发起激活，也不能单独代替监督进程。构建回报丢失时复用已保留制品和原提交键；不要通过新建变更来核对不明回执。

监督命令使用原数据目录、租户配置和监听地址：

```bash
cd capabilities/server
go run ./cmd/platform-updater serve -root /absolute/path/to/platform -profile lightweight -data /absolute/path/to/data -tenants /absolute/path/to/tenants.json -addr 127.0.0.1:8495 -host http://127.0.0.1:8495 -watch
```

原宿主的额外部署参数可放在 `serve` 命令的 `--` 之后传入，例如 `-host-admins`、`-database`、`-oidc-issuer`；凭据沿环境引用或原受控配置提供。发布号、摘要、源码、工作区、数据与租户路径仍由监督启动路径持有，不能用额外参数覆盖成另一发布。

首次从直接启动的宿主进入监督模式时，先完成首个源码提案的批准与构建，再在工作区请求激活并等待可取消窗口结束。使用该请求的真实 activation ID 建立首个监督选择：

```bash
cd capabilities/server
go run ./cmd/platform-updater swap -root /absolute/path/to/platform -host http://127.0.0.1:8495 -activation ACTUAL_ACTIVATION_ID
```

`swap` 沿权威激活读取并校验本地制品后写入 `.build/system/current.json`；它不证明新版本已成功启动，也不自行创建激活。随后停止直接启动的宿主，以同一数据目录、租户配置和监听地址运行上面的 `serve -watch`；监督进程启动首发、探活并回报同一激活。首发没有已保留的旧监督版本可自动回滚，失败时停止并核对原数据及原宿主。已有 `current.json` 时继续使用原选择核对来源，不覆盖成任意版本。直接宿主与监督宿主不能同时占用同一端口或数据目录。

代码换版由监督进程核对真实 activation ID、来源发布、可取消窗口和当前状态，再排空旧进程、启动候选并检查健康。失败保留证据并回滚；`current.json` 保留未完成回报，重启后继续原激活。发现租户隔离或缺失制品时不能以进程启动成功代替恢复成功。

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

## 本地更新与旧业务归档

负责人已选择采用新工作区，旧应用暂不开放。`make local` / `make docker` 和 `deploy/local/update.sh` 以同一个宿主提供 `8495`、`8490` 两入口；两个 URL 看到同一组 `workspace`、`system` 租户。不能在同一数据库中另起一个包含 `system` 的独立写入宿主。

当前租户起始配置为 `deploy/local/platform/tenants.json`（可用 `PLATFORM_TENANTS_FILE` 指向原受控配置），复用原 Rauthy 身份；酒店 manager 与制造 sup 可进入空白 `workspace`，manager 作为 host administrator 管理 `system`。这不是业务应用或数据迁移。原 `hospitality/tenants.json`、`manufacturing/tenants.json`、数据库日志与数据卷、对象存储、`.env`、签名及凭据主密钥保持留存；旧租户不装入当前组合，也不尝试用删数据来通过恢复检查。

更新 main 后运行 `deploy/local/update.sh`。它要求已提交源码，核对唯一 API 进程或镜像的实际版本，并逐个检查两入口的健康与页面资源。原生模式记录在 `.build/dev/environment.json`；启动和停止核对进程归属，不接管陌生端口。它是本地开发交付入口，自动自举换版仍需上文的 updater 监督命令。

`make rehearse` 运行当前组合的构建、自举 owner 与真实 PostgreSQL 检查点/恢复检查。数据库属于独立临时容器，退出时移除，不访问日常卷。原 Flow、设备和旧工作区演练入口已随其实现退出；该检查不等于旧业务迁移、完整页面运行时或浏览器验收。旧日志恢复与跨库移交仍须按原 owner 另行验收。

源码工具把动作 `payload`、具名读取 `inputs` 与工具的 `target` / `rationale` 分开，避免业务字段同名冲突。`agent.run` 的 JSON 步骤保留真实来源，逻辑文件路径不会冒充记录 ID；旧的已接受日志不重写。首次运行前，在 AI 中启用支持工具调用的模型，并在 Settings 的 `agent/model` 选择它；给 `ai/app-model` 设置默认值并不能替代智能体模型设置。

候选的原始字节和摘要共同保存，不把规范候选展开为可重排的 SQL JSON。完整系统变更还受原接受结果字节预算约束；文件/批次的路径闸门上限不保证一次提交均可接受，超限需按实际修改范围拆成独立提案。
