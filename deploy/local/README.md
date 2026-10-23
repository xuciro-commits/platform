# 本地环境

这里维护本地启动、地址、演示账号、外部配置、更新、演练与备份操作。系统结构见 [Architecture](../../docs/Architecture.md)，检查选择与交付纪律见 [AGENTS](../../AGENTS.md)，实际浏览器任务在 [web/e2e/tests](../../web/e2e/tests/)。

本页的密码和密钥都只用于本地环境，而且早已写在仓库的其他文件里（Rauthy 的初始数据、`rehearse.sh`）。**唯一不进仓库的是真实供应商的 API Key**，比如 OpenRouter：它放在 `deploy/local/.env`，该文件已被 git 忽略。

## 通用宿主

`make host ARGS='<部署参数>'` 运行同一份通用平台程序。启动、创建租户和隔离恢复使用同一个平台装配工厂，不装载原生行业应用；行业内容由包生成草稿后经原候选发布。指定 `-tenants=<文件>` 时继续使用原租户配置，`-admin` 只配置没有租户文件时的默认租户管理员。

独立验证使用新目录和新租户，不指向旧酒店或制造业务日志。例如在根目录执行：

```bash
make host ARGS='-profile=lightweight -data=.build/lite/platform-next -tenant=workspace-next -admin=user:builder -idp-new-key'
make host ARGS='-profile=lightweight -data=.build/lite/platform-next -tenant=workspace-next -admin=user:builder -addr=127.0.0.1:18498'
```

相对部署路径以仓库根目录解析。身份密钥只在首次显式创建，重启复用原目录。登录凭证使用原 `-mint-token=user:builder` 入口生成，不把凭证写入应用包。未安装和发布行业内容时，应用门户只有获权的平台入口。

## 日常开发（不打镜像）

两种模式都使用酒店 `http://localhost:8495`、制造 `http://localhost:8490`：

```bash
make local   # 停 Docker 宿主；启动基础设施/计算容器，两台 Go 宿主由 Air 重启，Vite 提供前端 HMR
make docker  # 停本地 Air/Vite；重建并校验 Docker 两台宿主，保留数据卷
make help    # 中文命令说明
```

`make local` 启动后台进程，终端退出后继续运行；重复执行不会重复启动。日志在 `.build/dev/`。启动等待实际就绪或进程退出，不因租户日志恢复超过固定时长而停止宿主；需要停止时可中断等待或执行 `make docker`。本地 Go API 使用内部 `18495/18490`，Vite 占用对外 `8495/8490` 并代理 API；容器模式由 Go 宿主直接提供同样的入口。同一租户不同时运行两个宿主。`make docker` 要求源码已提交，镜像版本和页面资源仍由 `update.sh` 核对。

底层命令仍可单独使用：`make infra`、`make dev SOLUTION=hospitality`、`make web SOLUTION=hospitality`（前端 5176；制造 5175）、`make infra-compute`。无 Docker 的内存探针用 `make dev-light`。首次缺依赖时运行 `make setup`。`make local` 复用数据卷和登录配置，不重建宿主镜像；计算容器使用独立的 `platform-dev-compute` 项目。

宿主的启动参数在 `deploy/dev/run.sh`，与下面 Compose 里的宿主一致（同一 PostgreSQL、同一 Rauthy、同一 tenants-next.json）；`deploy/local/.env` 的密钥同样被读取。

PostgreSQL 仍使用原 18 主版本和 `pgdata` 数据卷，镜像由 `Dockerfile.postgres` 加入 pgvector 0.8.7。首次升级检索依赖时运行 `docker compose -f deploy/local/compose.yaml build postgres`，再 `docker compose -f deploy/local/compose.yaml up -d --no-deps postgres`；后续使用原本地更新入口。宿主初始化扩展并转换可重建向量索引，不删除业务日志或重置数据卷。

## 验收栈与发布（Compose 镜像）


需要先有 Docker（OrbStack：`orb start`）。

容器模式下每个主机自己提供工作台页面，首次启动前先构建工作台：

```bash
pnpm --dir web/apps/workspace build
```

代码函数使用固定的 Go/TinyGo 工具链；先拉取一次以下镜像，之后租户编译完全离线：

```bash
docker pull golang@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414
docker pull ghcr.io/tinygo-org/tinygo@sha256:52907162ca3ba807c3c0914e07daffc1fe0c50ce91445ec43a0428337dc1b901
```

```bash
cd deploy/local
# Only on first setup; preserve this key across restarts. Never commit .env.
if ! rg -q '^PLATFORM_PERSONAL_TOKEN_KEY=' .env 2>/dev/null; then
  printf 'PLATFORM_PERSONAL_TOKEN_KEY=%s\n' "$(openssl rand -hex 32)" >> .env
fi
chmod 600 .env
chmod g+w hospitality/tenants-next.json manufacturing/tenants-next.json
docker compose up -d --build
```

```bash
cd deploy/local && docker compose ps
```

- 停止：`docker compose stop`。数据保存在 Docker 卷 `pgdata` 和 `rauthy` 里，下次启动会重放日志，数据还在。
- 只重建主机：`docker compose up -d --build manufacturing-server hospitality-server webhook-sink`。改了前端要先重新构建工作台。
- 计算能力：`wasm-worker` 与 `code-builder` 共用私有 `compute-sockets` 卷，没有公开端口、业务数据库或自有队列。主机通过 `PLATFORM_WASM_WORKER_SOCKET` / `PLATFORM_CODE_BUILDER_SOCKET` 委托执行与编译。只有固定命令的构建 driver 持有 Docker daemon socket；业务主机、Wasm worker、租户编译容器均不挂载它。worker 无网络、只读根、2 GiB RSS/2 CPU 限额；编译容器无网络、只读根、65534 用户及 CPU/RSS/进程限额。源码经内存 tar 输入，Wasm 与构建元数据走现有制品存储和发布。
- **更新 8495/8490 的代码**：在根目录运行 `deploy/local/update.sh`，沿用当前本地或容器模式，更新两台宿主并校验实际版本、健康和页面资源，报告位于 `.build/local-update/<revision>.json`。保留原数据卷、密钥与租户配置。临时浏览器宿主 `18496/18497` 的测试不会更新这两个日常入口；视觉检查以实际使用的入口为准。
- **直接运行开发宿主的编译配置**：单独 `go run` 不会继承 Compose 的环境。需要使用 `cmd/code-builder` 与 `cmd/wasm-worker` 的私有 Unix socket，并将宿主的 `PLATFORM_CODE_BUILDER_SOCKET` / `PLATFORM_WASM_WORKER_SOCKET` 指向它们；构建 driver 的 `PLATFORM_GO_WASM_IMAGE` / `PLATFORM_TINYGO_WASM_IMAGE` 使用本文件上面的固定摘要。出现 “owner-configured toolchain image pinned by sha256” 表示宿主未配置对应工具链，不是租户源码错误。已有内存宿主保存着编辑内容时，不为补配置直接重启；另起已配置实例，保留原实例中的数据。
- 行业样本由包内显式初始化计划提供，按下节执行 `make initialize`；启动与安装不自动播种。
- 完整演练：在仓库根目录运行 `make rehearse`（即 `scripts/verify.sh deploy`）。它使用独立 Compose 项目、另一组端口和全新卷，验证真实 OIDC 身份、通用声明发布、在途迁移与 Flow、原离线请求键、七节点遥测原版本接续、快照重启、PostgreSQL 备份恢复及单租户隔离恢复，不安装酒店、制造或供暖应用。环境身份映射在 `rehearsal-fixtures.json`；应用业务验收由构建者在原应用测试与发布入口执行。需 Docker、Node/pnpm、Python 3 与 Google Chrome（或 `CI=1` 下已安装的 Playwright Chromium）；结束只移除本次项目资源，诊断保存在 `.build/retirement/rehearsal-*`，恢复后的浏览器截图在 `web/e2e/test-results/deploy-recovery`。
- 数据/定义迁移演练：本地 Air/Vite 与基础设施运行后，在根目录执行 `make rehearse PROFILE=migration`。使用当前 HEAD 的通用宿主二进制、两个配置实例及真实 OIDC/PostgreSQL/RustFS，创建独立临时数据库与进程，验证首批暂停、快照重启、备份恢复、全日志重建、原键续接/激活与旧/新 Flow；结束清理本次资源。不会重建镜像或更改 8495/8490 与现有租户数据；完整容器演练仍走上面的入口。

真实供应商的密钥放在 `deploy/local/.env`，compose 启动时自动读取。一行一个：

```
OPENROUTER_API_KEY=sk-or-...
```

主机按名字取密钥：名字 `openrouter` 对应环境变量 `PLATFORM_SECRET_OPENROUTER`。要加新的密钥名，就在 `compose.yaml` 的 `manufacturing-server` / `hospitality-server` 的 `environment` 里加一行 `PLATFORM_SECRET_<名字大写>: "${变量:-}"`，再把变量写进 `.env`。

## 声明式应用包初始化

通用宿主的本地与容器启动入口分别使用 `hospitality/tenants-next.json` 与 `manufacturing/tenants-next.json`，在原基础设施上启用新的测试范围；原 `tenants.json` 保留用于旧二进制回退。切换日常运行实例前先完成下述资产移交，再执行 `deploy/local/update.sh`。新范围不复用旧探针租户 ID：

| 原测试范围 | 新测试范围 | 处理 |
|---|---|---|
| `hotel-a` | `hotel-a-build` | 从应用包重建 CRM、PMS |
| `hotel-test` | `hotel-test-build` | 独立空白酒店验证范围 |
| `plant-sz` | `plant-sz-build` | 从应用包重建 MES、ERP |
| `acme` | 不自动映射 | 保留原范围，移交前不切换或重建 |

新配置保留对应成员的 ID、登录 subject 和平台设置。旧 CRM/PMS/MES/ERP 的有效业务角色明确映射到 Build 的 sales、salesmanager、desk、hotelmanager、supervisor、operator、quality、buyer、controller、accountant；管理员显式配置 Builder 与初始化所需 Core accountant，会计成员显式配置 Core accountant；采购成员仅配置 Core reference-reader，以读取科目和期间，不授予主数据维护、凭证或试算读取及账务写入。HCM/CSM、原 gateway/assistant 的角色不产生新业务授权，其身份仍保留。酒店旧组织任职不自动复制；制造 L1/L2 任职保留，`plant-sz` 的组织任职显式映射到初始化计划的 `sampleplant`，其他任职仍须在 Enterprise 受权配置。包安装本身不执行这些角色配置。

切换前必须沿原资产入口移交 `hotel-a` 的 WMS 应用 `wms`、对象 `employee`、`partitem`、`order`、`goodsreceipt`、`partimports`、页面 `item`/`goodsreceipt`、数据集 `hrstaff`、`hrclean`、`partimports`、`mesmat`、`sapmat`、`sapom`，以及管道 `hrtoent`、`hrtoemployee`、`pipehrclean`、`mesparts`、`omtoorg`、`sapmaterials`，保留构建定义和有效草稿。负责人已允许 WMS 测试数据和结构重建，不要求复制旧业务记录、状态或历史；原日志与数据卷仍归档保留。现有 `plant-sz` 的 `mesworkspace`/`mesorders` 依赖原生 MES，随替代验收退出，不能作为新 MES 的输入。`application.heating` 及范围外构建内容仍保留。原数据库卷、日志、文件、身份与部署密钥均不清理；移交未验证前不启用新配置替换日常入口。

旧客户端继续携带原租户标识、请求键、schema 及回调关联，不能重定向到新租户或重签为新请求。切换前停止旧测试范围写入并保留原二进制、`tenants.json` 和环境；回退恢复这组原输入，不将新租户日志交给旧二进制，也不承诺反向转换新模型数据。

新的通用宿主不在启动时创建行业组织、业务样本或业务角色。先在 Packages 选择并安装 `application.hospitality` 或 `application.manufacturing` 的确切版本，生成应用草稿，在 Application Studio 完成候选检查及发布。包卡片的“初始化计划”可逐项查看所需角色、目标与数据；“构建说明”列出后续业务角色和环境绑定。

当前分发包统一使用 `application.*` 标识。旧名称退出分发目录，已安装版本仍按原 ID、版本和封存内容保留，不重命名、不自动安装新包，也不重新生成或初始化已有应用。旧计划中途失败时，必须使用原封存描述符、原身份和运行键接续；新包 ID 会产生不同请求键，不能拿它重试旧初始化。维护已有包镜像时，将撤下的描述符原样归档到索引目录之外，不能改写封存内容或保留名称别名。

确认目标是本次显式重建的新测试租户后，用该租户已授权成员的 token 执行对应计划。凭据从环境变量读取，不写入命令参数或包文件：

```bash
make initialize ARGS='--package deploy/packages/hospitality.json --host http://127.0.0.1:8495 --tenant hotel-a-build --run-key initial-samples-v1'
make initialize ARGS='--package deploy/packages/manufacturing.json --host http://127.0.0.1:8490 --tenant plant-sz-build --run-key initial-samples-v1'
```

默认凭据变量为 `PLATFORM_INITIALIZE_TOKEN`，也可用 `--token-env <name>` 选择现有变量。操作者须已获得计划列出的 Enterprise、Core、Build 角色；此入口不授予角色或任职。每项请求由其原 owner 独立接受，前面成功、后面失败时不会整体回滚。保持相同包内容、身份和运行键重试，可核对原请求并接续；不要更换运行键来处理未知答复。若包内容或已发布定义发生变化，先核对已接受记录和原请求，不能把新请求当作原请求重放。原标识已有冲突时停止，不覆盖数据。安装、普通更新及重启均不会执行此计划。

## 轻量 profile（无 Docker 的单机运行）

同一套语义可以在小设备上用一个二进制加一个数据目录跑起来：没有 Docker、没有 PostgreSQL、没有对象存储、没有外部身份服务。profile 是**声明**的，缺依赖就失败退出，不会悄悄回退到另一个 profile 的存储或身份。

根目录入口为 `make lite` / `make lite SOLUTION=manufacturing`。先在 `web` 执行 `corepack pnpm install --frozen-lockfile`；只有 Corepack 时加 `PNPM="corepack pnpm"`。默认只绑定 `127.0.0.1`；Arena 等受信任网络预览显式加 `LITE_HOST=0.0.0.0`（会开放演示角色卡，不能视为公网生产认证）。沙箱 Go 模块代理不可用时按 AGENTS 执行 `scripts/sandbox-go.sh` 及其打印的 export 挂载工作区。每次启动先在 `web` 构建工作台、再从平台模块编译同一个宿主，避免旧页面与新宿主混用；角色卡由本机签名，不打印或持久化登录 token。

```bash
(cd capabilities/server && go build -o /tmp/platform-server ./cmd/platform-server)

# 1. 第一次启动前生成签名密钥（数据目录里只做一次，之后绝不替换）
/tmp/platform-server -profile lightweight -data /var/lib/platform -idp-new-key

# 2. 给一个席位签发 token（token 从 stdout 输出，日志走 stderr；<user:email> 或 <client:id>）
TOKEN=$(/tmp/platform-server -profile lightweight -data /var/lib/platform -mint-token user:ops@example.com)

# 3. 启动；工作台（可选）用 -web 指向 web/apps/workspace 的构建
/tmp/platform-server -profile lightweight -data /var/lib/platform -addr 127.0.0.1:8495 -tenants deploy/local/hospitality/tenants-next.json -packages deploy/packages
```

- 备份前停止宿主，再复制整个数据目录；不要只复制日志或在活动写入中把不同时间的文件拼成备份。恢复到受支持的同版本宿主时保留原 `idp.key`，不要重新初始化密钥。
- `-data` 就是**全部**持久状态：`journal/journal.jsonl`（条目）、`journal/snapshots/`（本代码最新的两份快照）、`journal/derived/`（向量与转写）、`files/<tenant>/<hash>`（文件字节，内容寻址）、`idp.key`（签名密钥，0600）。备份和迁移就是复制这一个目录。
- 身份：轻量宿主只接受自己用 `idp.key` 签发的 token；交付宿主的开发 token（把 subject 原文当凭据）在这里是 401。租户与席位来自 `-tenants`（宿主控制台新建的租户会追加进该文件）或内置的演示租户。
- 宿主控制台：`-host-admins user:ops@example.test,client:ci`（逗号分隔）列出可以打开 Host Console 的主体；他们不需要任何租户席位——`/v1/me` 对其 401，工作区改从 `/v1/host/me` 装载只有控制台的壳。不给这个参数时控制台对所有人 401。
- 包索引：`-packages /path/to/descriptors` 加载描述符目录供 Packages 页安装/升级；同一索引用于启动、新建与恢复的租户，安装状态各自随租户日志恢复。目录不可读或描述符无效时启动失败。
- 与轻量 profile 互斥的开关会被拒绝：`-database`、`-files`、`-oidc-issuer`、`-oidc-keys`、`-project`；交付 profile 则拒绝 `-data`、`-idp-key`。
- 重启语义与 PostgreSQL 日志一致：启动时读本代码的最新快照、重放其余条目、损坏日志则隔离该租户；`/v1/sign-in`、`/v1/me`、权限拒绝与审计不因 profile 而变。

轻量 profile 的重启、备份与自签登录由 `capabilities/server/lightweight_test.go` 证明（`make test-go RUN=Lightweight`），不再有单独的演练脚本。

## 地址

- 文件：存在 RustFS（S3 兼容），S3 接口 `http://127.0.0.1:9000`，管理界面 `http://127.0.0.1:9001`，账号 `platform`，密码 `platform-files-local-only`。日志里只记文件的哈希，备份时 RustFS 的卷要和 PostgreSQL 一起备份。
- 健康检查：`http://127.0.0.1:8490/healthz`、`http://127.0.0.1:8495/healthz`（进程是否活着）；租户的健康（队列、放弃的工作、超配额、熔断器）是管理员的 `/v1/health`，也显示在 设置 → 自动化。租户被隔离时，先查明并修复其权威日志或依赖；仅该租户管理员可在工作区的恢复提示中重试，或以同一身份调用 `POST /v1/recovery/retry`。该操作从完整日志创建新租户代际，验证成功后才替换旧代际并修复派生快照；失败仍保持隔离，不会修复损坏的权威日志。`/healthz` 仍可能显示共享进程在线，不能代替该租户的 `/v1/health`。
- 调用关系和指标：给主机设环境变量 `OTEL_EXPORTER_OTLP_ENDPOINT`（如 `http://otel-collector:4318`）就会按 OTLP 导出；不设则不导出。本地环境没有配置采集器；不配置时仍可在设置与运行页面查看宿主提供的健康状态和执行记录。需要外部遥测时配置实际采集地址。

| 服务 | 地址 | 说明 |
|---|---|---|
| 身份认证 Rauthy | http://localhost:8480/auth/v1/ | OIDC 签发方；管理后台是 http://localhost:8480/auth/v1/admin |
| **工作台（酒店配置 hospitality-server，租户 `hotel-a-build`）** | http://localhost:8495 | 按角色打开应用包发布的 CRM、PMS，以及已移交的 WMS 和平台入口 |
| **工作台（制造配置 manufacturing-server，租户 `plant-sz-build`）** | http://localhost:8490 | 同一通用宿主运行应用包发布的 MES、ERP；科目、期间和业务样本由显式初始化计划创建 |
| webhook-sink | http://localhost:8497 | 本地 webhook、邮件、固定模型与状态窗口样本 |
| PostgreSQL | `localhost:5433`，库 `platform`，用户 `platform`，密码 `platform-local-only` | 两个租户的日志表 `journal` |

主机的接口也在同一个地址下（`/v1/...`）。开发工作台时用 `.claude/launch.json` 的配置：`workspace-hospitality`（连内存里的酒店业主机 8496，开发令牌）、`workspace-manufacturing`（连 8491）、`workspace-app`（连单个应用的开发主机 8499）、`workspace-oidc`（连 Docker 里的 8495，要登录），页面在 http://localhost:5176（`workspace-manufacturing` 是 5175，`workspace-app` 是 5177）。

账号登录到不属于自己的宿主时，提示页提供“退出并切换账号”，结束当前身份提供商会话并回到登录页；无需清理浏览器数据。退出同时清除该工作台记住的开发身份与租户选择。

已有 Rauthy 不会重新导入初始化账号。需要干净的测试身份时，按下文“从干净数据开始”使用新的数据命名空间，保留当前身份、业务数据卷与签名密钥；不要为增加演示账号删除现有卷。

## 报表工具直连数据库

面向报表的记录投影位于各租户的 schema（新范围为 `tenant_hotel_a_build`、`tenant_plant_sz_build`），每个实体类型一张表（如 `build_crmopportunity`），变更历史在 `<表名>_changes`。发布或升级对象后自动同步表与可导出列，不要求重启。它们是可重建的只读投影，不是业务写入入口。正式当前记录、接受结果和恢复位置另存于同一数据库的运行状态表；备份整个数据库及配套文件存储，不要仅导出报表表或 `journal` 表。

每个租户有一个只读角色（新范围为 `tenant_hotel_a_build_reader`、`tenant_plant_sz_build_reader`），默认不能登录。要让 Metabase、Power BI、Excel 这类工具连进来，先给它开登录：

```bash
cd deploy/local && docker compose exec postgres psql -U platform -d platform -c "alter role tenant_hotel_a_build_reader login password 'reader-local-only'"
```

然后在工具里填：主机 `localhost`，端口 `5433`，库 `platform`，用户 `tenant_hotel_a_build_reader`，密码 `reader-local-only`。它只能读自己租户的 schema。注意：记录级权限不会带到外部工具里，谁拿到这个角色就能看到整个租户的数据，所以这是管理员的授权。

## 人员账号（登录用）

所有人的密码都是 **`Plant-Local-1`**。

| 邮箱 | 主机 / 租户 | 成员 ID | 角色 | 组织 |
|---|---|---|---|---|
| `sup@plant.test` | 制造 `plant-sz-build` | `sup-1` | Build 主管、controller、builder；平台与组织管理员；Core accountant | `sampleplant`，含 L1/L2 |
| `op1@plant.test` | 制造 | `op-l1` | Build operator；AI user | L1 |
| `op2@plant.test` | 制造 | `op-l2` | Build operator；AI user | L2 |
| `qa1@plant.test`、`qa2@plant.test` | 制造 | `qa-1`、`qa-2` | Build quality；AI user | 双签使用不同成员 |
| `buyer@plant.test` | 制造 | `buyer-1` | Build buyer；Core reference-reader；AI user | 采购、收货，不获得账务写入 |
| `accountant@plant.test` | 制造 | `acc-1` | Build accountant；Core accountant；AI user | 业务单据与原总账分别接受 |
| `sales@hotel.test` | 酒店 `hotel-a-build` | `sales-1` | Build sales、desk；AI user | 本人商机 |
| `manager@hotel.test` | 酒店 | `manager-1` | Build salesmanager、hotelmanager、builder；平台管理员；Core accountant | 酒店业务与初始化 |
| `desk@hotel.test` | 酒店 | `desk-1` | Build user、desk；AI user | 预订与原 WMS 使用资格 |
| `deputy@hotel.test` | 酒店 | `deputy-1` | Build salesmanager；AI user | 销售管理 |

原 HCM/CSM 与专用 gateway/assistant 身份保留，但不授予退出应用的权限。初始角色只用于新范围；运行中的授权使用控制面板原成员动作，不通过改起始配置重写历史。

- 本地 Rauthy 走 HTTP，所以 `rauthy/config.toml` 里设了 `[access] cookie_mode = 'danger-insecure'`：不设的话，Safari 会丢掉 Rauthy 的安全 cookie，浏览器登录会显示密码错误（密码其实是对的）。只用于本地。
- Rauthy 管理员：`admin@platform.test`，密码 `Admin-Local-Only-1`。
- 每个人只担一份职责，测试时换人登录就能看到权限的差别；要同时看两个人，用一个普通窗口加一个无痕窗口分别登录。
- 初始化账号只在首次创建身份库时导入。新增真实成员走原身份管理与平台授权；需要重新导入演示身份时使用新的数据命名空间，不删除日常身份卷。
- 租户、成员名单与起始设置来自 `manufacturing/tenants-next.json` 和 `hospitality/tenants-next.json`；模板在 `deploy/templates/`。`sup@plant.test` 持有声明式 ERP 的 controller 资格。改了要重建对应主机才生效；在 Settings 里授予的角色是决策，会保存在日志里。

## 服务账号与 AI 代理（client credentials）

令牌地址是 `http://localhost:8480/auth/v1/oidc/token`，用 `grant_type=client_credentials`。

| client_id | 密钥 | 成员 | 用途 |
|---|---|---|---|
| `mes-gateway` | `gatewayLocalOnly000000000000000000000000000000000000000000000000` | `gateway-l1` | 保留的旧网关身份，新配置无旧推送权限 |
| `mes-assistant` | `assistantLocalOnly0000000000000000000000000000000000000000000000` | `agent-l1` | 保留的旧助手身份，新 AI 任务由平台声明重建 |
| `platform-cli` | `cliLocalOnly0000000000000000000000000000000000000000000000000000` | — | 脚本用密码模式为人员换令牌（`rehearse.sh`、`initialize.py`） |

旧原生服务账号、密钥和身份卷继续保留；`gateway-sim`、`mes-agent` 与应用专用启动程序已退出，新配置不将旧输入重定向到 Build。设备观测使用原 Observation 来源，AI 任务使用平台 Agent/Function 及人工检查点。外部协议服务必须另行配置其明确授权与命名密钥。

## 开发令牌（内存验证）

使用 `make dev-light`，或在平台模块运行 `go run ./cmd/platform-server -tenants ../../web/e2e/tenants.json -web ../../web/apps/workspace/dist`。未配置 OIDC 的开发模式以配置的 subject 为令牌，仅供本机验证；关闭进程后内存状态消失。需要持久状态与本机签名角色卡时使用 `make lite`。

### WMS 与应用工作台的部署关系

本地只有两个业务工作区入口：酒店平台实例 `http://localhost:8495/`、制造平台实例 `http://localhost:8490/`。各自的宿主提供 API 与工作区；共同使用本地 PostgreSQL、RustFS、Rauthy 和计算服务，记录/日志/权限按租户隔离，不是两套独立的物理基础设施。

WMS 是工作台中保存并发布的 `build.app` 与对象/页面/流程/代码定义，复用所属宿主，不另建应用容器或端口。构建者在 Applications 创建应用，复用 `core.material`、`core.location` 等主数据，配置对象、动作、页面与流程，在发布工作台保存封存候选后激活；业务成员从应用门户进入。原共同应用构建任务可用 `make e2e SPEC=workspaces` 在独立开发宿主执行；它不代表现场 WMS 已交付。

每次更新 main 后运行 `deploy/local/update.sh`：构建工作区、重建两台宿主、校验 Git 版本标记、健康及实际提供的页面资源。原数据卷和租户配置保留。

### 从应用包构建供暖应用

1. 运行 `make packages`，在 Packages 明确安装协议包与 `application.heating`，选择 **Create application drafts**。安装本身不发布应用、不授予成员权限、不导入业务记录。
2. 在应用的 Integrations 配置 `heatingfield` 的网关地址、密钥引用和设备点表，再 Check；模拟配置见 `deploy/examples/heating/harbin.json` 与 `jinan.json`。`propertyfeed` 配置独立 HTTP 接收方并 Check，告警路径为 `alarms`。
3. 配置并启用 AI 提供方的 `heating/health` 模型；在发布审阅运行 `heatingadvicequality` 评估，查看合成样本与业务测试计划后保存候选并激活。未配置模型时不把合成结果当成真实诊断。
4. 普通成员从应用门户进入楼宇、设备、告警、门禁与健康页面。阀位申请需原审批；投递成功不代表设备完成，必须由匹配的新鲜观测或授权人工证据确认。通行证独立于平台角色，设备 profile 未开放的开门操作不会出现。

供暖业务用例在包内 TestPlan 与发布评估入口维护；协议安装与同版重装的机制任务为 `make e2e SPEC=protocol-packages`。包卸载前先准备并等待在途工作结束，卸载保留应用、业务数据与历史；原版本内容一致才可重新安装。真实地址、现场参数、设备兼容性、模型质量与生产容量仍需在目标环境验证。

### 从干净数据开始

需要从头走查时，停止当前 Compose（`docker compose -f deploy/local/compose.yaml down`，**不加 `-v`**），在 `deploy/local/.env` 设置一个新的 `PLATFORM_DATA_NAMESPACE`（如 `platform-review-20261007`）。四个数据卷会使用这个前缀；旧卷保留，`.env` 中的签名密钥和供应商密钥保留。默认未设置前缀时仍使用 Compose 项目名，独立排演项目的数据不会混用。

先构建工作区，带 `PLATFORM_REVISION=$(git rev-parse HEAD)` 运行 `docker compose -f deploy/local/compose.yaml up -d --build`，再按应用包章节显式发布与初始化，运行 `deploy/local/update.sh` 检查实际版本。新命名空间不包含旧数据；已有 WMS 定义与有效草稿必须从原 owner 移交，不能因为重建测试业务而丢弃构建内容。旧日志、数据卷、身份、密钥与配置均保留。

酒店宿主内的 `hotel-test-build` 是晋级目标，与 `hotel-a-build` 共用 `8495` 入口；租户之间隔离记录与权限。用 `manager@hotel.test` 的 Host Console 开目标成员支持会话后晋级；更改存储的 v2 先不激活地晋级，再在目标租户发布工作台审阅其升级计划。`8490` 制造宿主是另一进程，不是这条同宿主晋级路线的目标。在目标 Host Console 选择源环境与原候选，晋级后切到目标租户的发布工作台检查封存内容和升级计划，再显式激活。晋级不会把源租户的业务记录或成员授权一并复制；需要搬迁记录时使用原迁移任务并核对原行结果。

`deploy/local/rehearse.sh` 不受这个前缀影响：它把 `PLATFORM_DATA_NAMESPACE` 设为本次独立的 `platform-rehearsal-<pid>`，用自己的卷并在结束时删除，不会碰走查数据卷；`scripts/verify.sh` 不带参数时也不再包含 `deploy` 步骤，排演需显式 `scripts/verify.sh deploy`。

回退时先停 Compose，再把 `PLATFORM_DATA_NAMESPACE` 改回旧前缀（原始环境删除这条变量），重新启动；不要把两个命名空间的数据卷混配。删除旧卷由负责人单独决定。


## 接入外部系统时填什么

这些都在 Settings 里添加（登录 `sup@plant.test` 或 `manager@hotel.test`）。

**Webhook 接收地址**（Integrations → Add endpoint → Webhook）

| 用途 | URL | 密钥名 | 勾选 |
|---|---|---|---|
| 通用 webhook | `http://webhook-sink:8080/hook` | `sink` | 勾"内部地址"；事件任选，如 `lodging.booking/1#canceled` |

sink 收到的 webhook 在 http://localhost:8497/received 查看。`POST http://localhost:8497/fail?on=true` 让它开始返回 503，用来测试重试；`?on=false` 恢复。工厂的 ERP 是同一主机里的 ERP 应用，不需要接收地址。外部 ERP 通过 Connection、Writeback 和 Confirmation 声明请求、回读与结果映射；HTTP 成功不等于业务确认。原失败恢复任务为 `make e2e SPEC=business-confirmation`，完整来源到业务应用任务为 `make e2e SPEC=integration-fabric`。

**邮件接收地址**（Integrations → Add endpoint → Email）

| URL | 发件人 | 密钥名 | 勾选 |
|---|---|---|---|
| `smtp://webhook-sink:2525` | 任意，如 `plant@plant.test` | 留空 | 勾"内部地址"；应用勾 `build`、`platform` |

收到的邮件在 http://localhost:8497/mail 查看。只有以邮箱登录的成员会收到邮件，服务账号和 AI 代理不会。真实 SMTP 服务器的写法是 `smtp://用户名@smtp.example.com:587`，密码放在密钥里（密钥名填在"密钥名"一栏）。

**AI 供应商**（AI → Providers and models → Add provider）

| 类型 | 填写 | 密钥名 |
|---|---|---|
| 供应商 OpenRouter | ID `openrouter`，供应商选 OpenRouter | `openrouter`（`.env` 里的 `OPENROUTER_API_KEY`） |
| 供应商 OpenAI / Gemini / Moonshot / DeepSeek / Qwen / 智谱 | 选对应供应商 | 自定义名字，并按"启动与停止"一节加上对应的环境变量 |
| 第三方兼容接口 | Base URL `https://…/v1` | 必填 |
| 本地模型：LM Studio / Ollama / llama.cpp | `http://host.docker.internal:1234/v1`、`:11434/v1`、`:8080/v1` | 可留空 |
| 本地替身（无需安装） | 容器宿主用 `http://webhook-sink:8080/v1`，Air 宿主用 `http://127.0.0.1:8497/v1` | 留空 |

- 添加后点 Models 读取模型目录。勾 "free only" 只看免费模型：可用模型与限流由上游实时返回；遇到 429 时按实际答复重试或选择其他已授权模型。
- 选 everyone 或 ai users 启用，然后在 Playground 调用，在 Usage 看用量。
- 没有 `ai` 角色的成员（例如 AI 助手 `agent-l1`）只能用开放给 everyone 的模型。

本地替身的 `echo` 回显输入，`review` 给出固定的结构化复核建议或人工询问；后者用于声明式销售、制造任务的机制走查，不代表真实模型质量。同一服务的 `/messages` 提供空渠道样本，`/observations` 提供最长五分钟、每十秒一点的固定 `run/down` 窗口。Observation 的 Connection 地址填服务根地址，路径填 `/observations`；制造包的 `supplieragent` 也可指向服务根地址，其 `rpc` 写回获得固定交期文本。真实渠道、设备、供应方与模型需分别配置。

CRM 的 `lodgingupstream`、MES 的 `productionupstream` 应连接实际提供方。Connection 的密钥名分别使用 `lodging-upstream`、`production-upstream`；在 `.env` 中以 `PLATFORM_SECRET_LODGING_UPSTREAM`、`PLATFORM_SECRET_PRODUCTION_UPSTREAM` 保存提供方签发的受限令牌，值为完整的 `Bearer …` 授权头并用引号包住。个人令牌沿提供方原 Token 入口签发和撤销，不把它写入包。相同租户内的对象仍共用 Build 权威，独立权威验证必须使用独立租户或系统。

**MCP**：`POST http://localhost:8490/mcp`（或 8495），带 `Authorization: Bearer <该成员的令牌>`。工具列表就是这个成员的动作目录和可读数据。

**外部智能体（A2A）**（Integrations → Add endpoint → A2A，只在 MES 用到）

| URL | 密钥名 | 勾选 |
|---|---|---|
| `http://webhook-sink:8080/a2a`（供应商智能体替身） | 可留空（真实对方填存放其令牌的密钥名） | 勾"内部地址"；外发类型 已发布的供应方查询类型 |

发布我们自己的智能体：App settings → Agents → "Published over A2A" 填已发布的 `build.salesassistant`；卡片在 `http://localhost:8495/a2a/hotel-a-build/build.salesassistant/.well-known/agent-card.json`，调用方用成员令牌按 A2A 1.0 JSON-RPC 发 `SendMessage`（请求头 `A2A-Version: 1.0`）。

宿主控制台的管理员由 `-host-admins` 显式指定 OIDC subject，租户的 platform.admin 不自动获得宿主权限。本地酒店配置 `user:manager@hotel.test`，制造配置 `user:sup@plant.test`；其它账号访问 `/v1/host/*` 被拒绝。新租户由模板创建并写入可写的 tenants.json，需在生产配置中选择实际宿主运维身份。

新建租户需要写回挂载的租户配置。宿主仍以 `nobody` 运行，Compose 用 `TENANTS_GID` 加入文件所属组（macOS 的默认 staff 为 20）；启动前运行 `chmod g+w deploy/local/{hospitality,manufacturing}/tenants-next.json`，其他系统以 `TENANTS_GID=$(id -g) docker compose -f deploy/local/compose.yaml up -d --build` 启动。文件只含租户配置与身份引用，不含登录密钥。

OIDC delivery 使用 `PLATFORM_PERSONAL_TOKEN_KEY`（至少 32 字节）签发个人令牌，未配置时拒绝启动；该密钥独立于 OIDC 公钥，重启时必须保留，轮换会使已签发的个人令牌失效。开发令牌宿主只用于开发；lightweight 使用其已有的私有签名密钥。

种子席位的角色只用于新租户。已有日志的租户以已接受的成员状态为准；若管理员尚无 `core:steward`，在 Control Panel → Members 正式授予该角色，重启不会替代授权决定。

运行模式保存在 `.build/dev/environment.json`，与监督进程是否存活分开。普通更新沿用上次选择，停止或异常退出不会自动切换到 Docker；`make local` / `make docker` 显式切换。Docker 更新先启动既有基础服务，全部路径保留原数据卷。
