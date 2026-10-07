# 本地环境

这里是本地环境的全部资料：怎么启动、地址、账号密码、服务账号、接入外部系统要填什么。测试路线和测试方法在 [docs/Testing.md](../../docs/Testing.md)。

本页的密码和密钥都只用于本地环境，而且早已写在仓库的其他文件里（Rauthy 的初始数据、`rehearse.sh`）。**唯一不进仓库的是真实供应商的 API Key**，比如 OpenRouter：它放在 `deploy/local/.env`，该文件已被 git 忽略。

## 启动与停止

需要先有 Docker（OrbStack：`orb start`）。

每个主机自己提供工作台页面（ADR-0018），所以先构建一次工作台：

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
chmod g+w hospitality/tenants.json manufacturing/tenants.json
docker compose up -d --build
```

```bash
cd deploy/local && docker compose ps
```

- 停止：`docker compose stop`。数据保存在 Docker 卷 `pgdata` 和 `rauthy` 里，下次启动会重放日志，数据还在。
- 只重建主机：`docker compose up -d --build manufacturing-server hospitality-server webhook-sink`。改了前端要先重新构建工作台。
- 计算能力（ADR-0044）：`wasm-worker` 与 `code-builder` 共用私有 `compute-sockets` 卷，没有公开端口、业务数据库或自有队列。主机通过 `PLATFORM_WASM_WORKER_SOCKET` / `PLATFORM_CODE_BUILDER_SOCKET` 委托执行与编译。只有固定命令的构建 driver 持有 Docker daemon socket；业务主机、Wasm worker、租户编译容器均不挂载它。worker 无网络、只读根、2 GiB RSS/2 CPU 限额；编译容器无网络、只读根、65534 用户及 CPU/RSS/进程限额。源码经内存 tar 输入，Wasm 与构建元数据走现有制品存储和发布。
- **在 8495/8490 验收本次代码时**：先运行 `pnpm --dir web/apps/workspace build`，再在 `deploy/local` 运行 `docker compose up -d --no-deps --build hospitality-server`（或对应的 `manufacturing-server`），最后刷新浏览器。`scripts/verify.sh web` 的浏览器路线使用一次性的内存酒店主机 `18496`；它通过也不会自动更新你正在看的 8495 容器。两者的测试数据和登录方式也不同，视觉验收应以你实际使用的容器地址为准。
- **直接运行开发宿主的编译配置**：单独 `go run` 不会继承 Compose 的环境。需要使用 `cmd/code-builder` 与 `cmd/wasm-worker` 的私有 Unix socket，并将宿主的 `PLATFORM_CODE_BUILDER_SOCKET` / `PLATFORM_WASM_WORKER_SOCKET` 指向它们；构建 driver 的 `PLATFORM_GO_WASM_IMAGE` / `PLATFORM_TINYGO_WASM_IMAGE` 使用本文件上面的固定摘要。出现 “owner-configured toolchain image pinned by sha256” 表示宿主未配置对应工具链，不是租户源码错误。已有内存宿主保存着编辑内容时，不为补配置直接重启；另起已配置实例，保留原实例中的数据。
- 灌酒店业演示数据：`./seed-hospitality.sh`。可以重复执行，结果不变。
- 完整演练：在仓库根目录运行 `scripts/verify.sh deploy`。它用另一组端口和一套全新的数据，不会动你的本地数据；需 Docker、Node/pnpm 与 Google Chrome（或在 `CI=1` 下已安装的 Playwright Chromium）。两行业 OIDC 浏览器会在发布前及 PostgreSQL 恢复后执行共同函数/页面/Flow 路线，自动截图保存在 `web/e2e/test-results/deploy-*`；脚本结束后移除一次性容器。

真实供应商的密钥放在 `deploy/local/.env`，compose 启动时自动读取。一行一个：

```
OPENROUTER_API_KEY=sk-or-...
```

主机按名字取密钥（ADR-0014 D5）：名字 `openrouter` 对应环境变量 `PLATFORM_SECRET_OPENROUTER`。要加新的密钥名，就在 `compose.yaml` 的 `manufacturing-server` / `hospitality-server` 的 `environment` 里加一行 `PLATFORM_SECRET_<名字大写>: "${变量:-}"`，再把变量写进 `.env`。

## 轻量 profile（无 Docker 的单机运行，ADR-0049）

同一套语义可以在小设备上用一个二进制加一个数据目录跑起来：没有 Docker、没有 PostgreSQL、没有对象存储、没有外部身份服务。profile 是**声明**的，缺依赖就失败退出，不会悄悄回退到另一个 profile 的存储或身份。

```bash
go build -o /tmp/hospitality-server ./solutions/hospitality/cmd/hospitality-server

# 1. 第一次启动前生成签名密钥（数据目录里只做一次，之后绝不替换）
/tmp/hospitality-server -profile lightweight -data /var/lib/platform -idp-new-key

# 2. 给一个席位签发 token（token 从 stdout 输出，日志走 stderr；<user:email> 或 <client:id>）
TOKEN=$(/tmp/hospitality-server -profile lightweight -data /var/lib/platform -mint-token user:ops@example.com)

# 3. 启动；工作台（可选）用 -web 指向 web/apps/workspace 的构建
/tmp/hospitality-server -profile lightweight -data /var/lib/platform -addr 127.0.0.1:8496
```

- `-data` 就是**全部**持久状态：`journal/journal.jsonl`（条目）、`journal/snapshots/`（本代码最新的两份快照）、`journal/derived/`（向量与转写）、`files/<tenant>/<hash>`（文件字节，内容寻址）、`idp.key`（签名密钥，0600）。备份和迁移就是复制这一个目录。
- 身份：轻量宿主只接受自己用 `idp.key` 签发的 token；交付宿主的开发 token（把 subject 原文当凭据）在这里是 401。租户与席位来自 `-tenants`（宿主控制台新建的租户会追加进该文件）或内置的演示租户。
- 宿主控制台：`-host-admins user:ops@example.test,client:ci`（逗号分隔）列出可以打开 Host Console 的主体；他们不需要任何租户席位——`/v1/me` 对其 401，工作区改从 `/v1/host/me` 装载只有控制台的壳。不给这个参数时控制台对所有人 401。
- 包索引：`-packages /path/to/descriptors` 加载描述符目录供 Packages 页安装/升级；同一索引用于启动、新建与恢复的租户，安装状态各自随租户日志恢复。目录不可读或描述符无效时启动失败。
- 与轻量 profile 互斥的开关会被拒绝：`-database`、`-files`、`-oidc-issuer`、`-oidc-keys`、`-project`；交付 profile 则拒绝 `-data`、`-idp-key`。
- 重启语义与 PostgreSQL 日志一致：启动时读本代码的最新快照、重放其余条目、损坏日志则隔离该租户；`/v1/sign-in`、`/v1/me`、权限拒绝与审计不因 profile 而变。

无 Docker 的走查（ADR-0049 §3.5）：

```bash
rm -rf /tmp/platform-lightweight-rehearse
PLATFORM_REHEARSE_BIN=/tmp/hospitality-server bash deploy/local/rehearse-lightweight.sh walk    # 建密钥、自签登录、写决策、停止、再启动、读回
HOST=http://127.0.0.1:18499 TOKEN=$TOKEN bash deploy/local/rehearse-lightweight.sh verify        # 对运行中的宿主核对同一状态与同一决策
PLATFORM_REHEARSE_BIN=/tmp/hospitality-server bash deploy/local/rehearse-lightweight.sh backup   # 复制唯一的数据目录并在副本上再起一个宿主
```

## 地址

- 文件（ADR-0028）：存在 RustFS（S3 兼容），S3 接口 `http://127.0.0.1:9000`，管理界面 `http://127.0.0.1:9001`，账号 `platform`，密码 `platform-files-local-only`。日志里只记文件的哈希，备份时 RustFS 的卷要和 PostgreSQL 一起备份。
- 健康检查：`http://127.0.0.1:8490/healthz`、`http://127.0.0.1:8495/healthz`（进程是否活着）；租户的健康（队列、放弃的工作、超配额、熔断器）是管理员的 `/v1/health`，也显示在 设置 → 自动化。租户被隔离时，先查明并修复其权威日志或依赖；仅该租户管理员可在工作区的恢复提示中重试，或以同一身份调用 `POST /v1/recovery/retry`。该操作从完整日志创建新租户代际，验证成功后才替换旧代际并修复派生快照；失败仍保持隔离，不会修复损坏的权威日志。`/healthz` 仍可能显示共享进程在线，不能代替该租户的 `/v1/health`。
- 链路和指标（ADR-0027）：给主机设环境变量 `OTEL_EXPORTER_OTLP_ENDPOINT`（如 `http://otel-collector:4318`）就会按 OTLP 导出；不设则不导出。本地环境**没有**装采集器（Jaeger、Grafana 这类），所以现在没有地址可填、也没有界面可看；不装也不影响任何功能，平台里能看的是 设置 → 自动化 的健康状态和运行、流程页上的调用链。要看完整链路时再加一个采集器（需要新的镜像，先确认）。

| 服务 | 地址 | 说明 |
|---|---|---|
| 身份认证 Rauthy | http://localhost:8480/auth/v1/ | OIDC 签发方；管理后台是 http://localhost:8480/auth/v1/admin |
| **工作台（酒店业主机 hospitality-server，租户 `hotel-a`）** | http://localhost:8495 | 登录一次，按角色打开 CRM、PMS、HCM、CSM、设置、收件箱，不用换页面 |
| **工作台（制造业主机 manufacturing-server，租户 `plant-sz`）** | http://localhost:8490 | 同一个工作台，显示工厂的应用：MES 和 ERP。日志为空时（第一次启动）主机给 ERP 建好科目、打开本月期间、建好钢材和两种产品 |
| webhook-sink | http://localhost:8497 | 本地的外部系统替身：webhook 接收方、ERP、邮件服务器、本地模型 |
| PostgreSQL | `localhost:5433`，库 `platform`，用户 `platform`，密码 `platform-local-only` | 两个租户的日志表 `journal` |

主机的接口也在同一个地址下（`/v1/...`）。开发工作台时用 `.claude/launch.json` 的配置：`workspace-hospitality`（连内存里的酒店业主机 8496，开发令牌）、`workspace-manufacturing`（连 8491）、`workspace-app`（连单个应用的开发主机 8499）、`workspace-oidc`（连 Docker 里的 8495，要登录），页面在 http://localhost:5176（`workspace-manufacturing` 是 5175，`workspace-app` 是 5177）。

账号登录到不属于自己的宿主时，提示页提供“退出并切换账号”，结束当前身份提供商会话并回到登录页；无需清理浏览器数据。退出同时清除该工作台记住的开发身份与租户选择。

**本地 Rauthy 已经运行过的话**：它只在第一次启动时读取初始数据，所以还不认识新的登录客户端 `platform-web`，登录会报找不到客户端。重建一次身份认证的数据卷即可（里面只有测试账号，会按 `rauthy/bootstrap` 重新生成，账号密码不变）：

```bash
cd deploy/local && docker compose rm -sf rauthy && docker volume rm platform_rauthy && docker compose up -d rauthy
```

## 报表工具直连数据库（ADR-0019）

两个主机启动时会把各自租户的记录复制到 PostgreSQL，每个租户一个 schema（`tenant_hotel_a`、`tenant_plant_sz`），每个实体类型一张表（如 `crm_opportunity`），变更历史在 `<表名>_changes`。这些只是副本，主机每次启动会按日志重建，所以不要往里写，备份也只需要日志表 `journal`。

每个租户有一个只读角色（`tenant_hotel_a_reader`、`tenant_plant_sz_reader`），默认不能登录。要让 Metabase、Power BI、Excel 这类工具连进来，先给它开登录：

```bash
cd deploy/local && docker compose exec postgres psql -U platform -d platform -c "alter role tenant_hotel_a_reader login password 'reader-local-only'"
```

然后在工具里填：主机 `localhost`，端口 `5433`，库 `platform`，用户 `tenant_hotel_a_reader`，密码 `reader-local-only`。它只能读自己租户的 schema。注意：记录级权限不会带到外部工具里，谁拿到这个角色就能看到整个租户的数据，所以这是管理员的授权。

## 人员账号（登录用）

所有人的密码都是 **`Plant-Local-1`**。

| 邮箱 | 主机 / 租户 | 成员 ID | 角色 | 组织 |
|---|---|---|---|---|
| `sup@plant.test` | MES `plant-sz` | `sup-1` | mes 主管；platform、enterprise、ai 管理员；build 构建者（builder） | 工厂 `plant-sz`（管两条线） |
| `op1@plant.test` | MES | `op-l1` | mes 操作员；ai 用户 | 产线 `L1` |
| `op2@plant.test` | MES | `op-l2` | mes 操作员；ai 用户 | 产线 `L2` |
| `qa1@plant.test` | MES | `qa-1` | mes 质量；ai 用户 | — |
| `qa2@plant.test` | MES | `qa-2` | mes 质量；ai 用户（报废需要两个质量签名） | — |
| `sales@hotel.test` | 酒店业 `hotel-a` | `sales-1` | crm 销售、pms 前台、memstay 管家、hcm 员工；ai 用户 | 销售组、2026 年会项目 |
| `manager@hotel.test` | 酒店业 | `manager-1` | crm 销售经理、pms 经理、memstay 管家、hcm 员工、csm lead；work、flow、agent 管理员；platform、org、ai 管理员；build 构建者（builder） | 酒店总经理等；`hotel-a` 经理、`hospitality` 负责人（请假的两级审批人） |
| `hr@hotel.test` | 酒店业 | `hr-1` | hcm 人事（hr）；ai 用户 | `hotel-a` 人事专员：看得到病假的"医疗原因"，经理和员工看不到 |
| `desk@hotel.test` | 酒店业 | `desk-1` | csm 客服（desk）、pms 前台、hcm 员工；ai 用户；build 使用者（user） | 前台（`front-office`）：接工单、回复客户，模型不可用时工单进她的收件箱 |
| `deputy@hotel.test` | 酒店业 | `deputy-1` | crm 销售经理、csm lead、hcm 员工；ai 用户 | `hotel-a` 副总经理：经理不在时代批（委托审批的被委托人） |
| `buyer@plant.test` | ERP `plant-sz` | `buyer-1` | erp 采购员（buyer）；ai 用户 | 下采购订单、收货；超过审批限额的订单由 `sup@plant.test`（主管会计）审批 |
| `accountant@plant.test` | ERP | `acc-1` | erp 会计（accountant）；ai 用户 | 起草、过账、冲销凭证，登记供应商发票 |

制造主管的构建者角色通过控制面板的成员授权动作 `platform.member.grant`（app `build`、role `builder`）加入现有租户并记录在账本。新建演示租户也用此动作启用构建入口；已有接受结果的租户不要修改目录中成员的初始角色来代替授权，这会改变历史恢复的前置状态。

- 本地 Rauthy 走 HTTP，所以 `rauthy/config.toml` 里设了 `[access] cookie_mode = 'danger-insecure'`：不设的话，Safari 会丢掉 Rauthy 的安全 cookie，浏览器登录会显示密码错误（密码其实是对的）。只用于本地。
- Rauthy 管理员：`admin@platform.test`，密码 `Admin-Local-Only-1`。
- 每个人只担一份职责，测试时换人登录就能看到权限的差别；要同时看两个人，用一个普通窗口加一个无痕窗口分别登录。
- 新加的账号要 Rauthy 重新初始化才有（本地数据可丢）：`cd deploy/local && docker compose rm -sf rauthy && docker volume rm platform_rauthy && docker compose up -d rauthy`，再重建两个主机。
- 租户、成员名单与起始设置来自 `manufacturing/tenants.json` 和 `hospitality/tenants.json`（ADR-0078）；模板在 `deploy/templates/`。`sup@plant.test` 同时是 ERP 的主管会计（controller）。改了要重建对应主机才生效；在 Settings 里授予的角色是决策，会保存在日志里。

## 服务账号与 AI 代理（client credentials）

令牌地址是 `http://localhost:8480/auth/v1/oidc/token`，用 `grant_type=client_credentials`。

| client_id | 密钥 | 成员 | 用途 |
|---|---|---|---|
| `mes-gateway` | `gatewayLocalOnly000000000000000000000000000000000000000000000000` | `gateway-l1` | 产线网关，推送设备状态（`cmd/gateway-sim`） |
| `mes-assistant` | `assistantLocalOnly0000000000000000000000000000000000000000000000` | `agent-l1` | **AI 代理**：产线 L1 的助手；它做的不可撤回外发要人批准（D6）；它在 ERP 没有角色，所以 ERP 会拒绝它发起的确认 |
| `platform-cli` | `cliLocalOnly0000000000000000000000000000000000000000000000000000` | — | 脚本用密码模式为人员换令牌（`rehearse.sh`、`seed-hospitality.sh`） |

以 AI 助手身份操作：

```bash
cd apps/mes/server && MES_AGENT_CLIENT=mes-assistant MES_AGENT_SECRET=assistantLocalOnly0000000000000000000000000000000000000000000000 go run ./cmd/mes-agent -server http://localhost:8490 -oidc-token http://localhost:8480/auth/v1/oidc/token actions
```

把末尾的 `actions` 换成 `do <动作> <目标> '<JSON>'` 就是执行动作，例如 `do mes.downtime.reason <停机 ID> '{"reason":"Setup"}'`。

## 开发令牌（不登录的演示模式）

用 `go run ./cmd/<server>` 直接启动的主机（不带 `-oidc-issuer`）接受开发令牌：令牌就是登录名。开发主机只在内存里，停掉数据就没了；不占 Docker 的端口。

**两个行业方案**（多个应用组合在一起）：

| 方案 | 启动（在该目录下） | 端口 | 启动配置 | 开发令牌 |
|---|---|---|---|---|
| 酒店业：CRM、PMS、HCM、CSM | `solutions/hospitality`：`go run ./cmd/hospitality-server` | 8496 | `workspace-hospitality` | `manager`、`sales`、`sales-only`、`desk` |
| 制造业：MES、ERP | `solutions/manufacturing`：`go run ./cmd/manufacturing-server`（加 `-erp external` 换成 ERP 适配器） | 8491 | `workspace-manufacturing` | `supervisor`（也是 ERP 主管会计）、`accountant`、`operator-l1`、`operator-l2`、`quality-1`、`quality-2`、`gateway-l1`、`assistant-l1`（AI 代理）、`erp` |

**每个应用单独运行**（ADR-0025：每个应用先能自己干，不依赖别的应用和外部系统）：在 `apps/<应用>/server` 下运行 `go run ./cmd/<应用>-server -web ../../../web/apps/workspace/dist`，端口都是 8499（一次起一个），启动配置 `workspace-app`：

| 应用 | 开发令牌 |
|---|---|
| `crm` | `manager`、`sales` |
| `erp` | `controller`、`accountant`、`buyer`（已建好科目、本月期间、供应商和物料） |
| `mes` | `supervisor`、`operator-l1`、`operator-l2`、`quality-1`、`quality-2`、`gateway-l1`、`assistant-l1` |
| `pms` | 见 `apps/pms/server/cmd/pms-server`（两个租户，Tauri 桌面端连它） |
| `hcm` | `employee`、`manager`、`head`、`hr` |
| `csm` | `desk`、`lead` |
| `erpadapter` | `planner`、`erp` |

右上角的身份菜单可以切换开发身份。要带 OpenRouter 密钥，就先 `set -a; . deploy/local/.env; set +a`，再加上 `PLATFORM_SECRET_OPENROUTER=$OPENROUTER_API_KEY`。

制造内存演示宿主的 `operator-l1`、`operator-l2` 同时持有 `build.user`，用于路线 43 的对象操作与人工收件箱探针；它们不能编辑构建器定义。此演示角色不修改 OIDC 部署的角色绑定。

### WMS 装配走查环境

当前 WMS 权限与收货走查入口为 `http://127.0.0.1:18505`，应用切换器选择 **WMS / 仓库管理**。构建者为 `manager`（`manager-1`），普通操作员为 `desk`（`desk-1`），业务主管为 `business-supervisor`（`business-supervisor-1`，仅 `build.supervisor`，由装配脚本经原授权动作配置）；这里只复用酒店组合宿主提供平台应用，没有添加 WMS 源码服务。应用编辑器 `/#/application?id=WMS-APP` 归集五个对象、收货流程及 Go/Wasm 算法，页面单独管理导航；`/#/release-review?kind=app&id=WMS-APP` 从应用审查完整候选。`PUT-001` 实收 125 件，经真实 Go/Wasm 计算 3 个托盘并由主管批准后已上架。18503 保留 `PUT-PARTIAL` / `PUT-OVER` 的跨对象校验样本，18502 保留前一版 `PUT-BOUND` 待审批样本，18501 保留此前收货样本及用户数据。

宿主以 `PLATFORM_CODE_BUILDER_SOCKET`、`PLATFORM_WASM_WORKER_SOCKET` 连接已有 `.build/compute-review/builder.sock` 和 `worker.sock`，从 `solutions/hospitality` 启动：

```sh
PLATFORM_CODE_BUILDER_SOCKET="$PWD/../../.build/compute-review/builder.sock" \
PLATFORM_WASM_WORKER_SOCKET="$PWD/../../.build/compute-review/worker.sock" \
go run ./cmd/hospitality-server -addr 127.0.0.1:18505 -web ../../web/apps/workspace/dist
```

编译 driver 仍需上文固定镜像与运行中的 Docker；装配命令见 [Apps](../../docs/Apps.md#wms-受控装配探针)。这些独立开发宿主是内存验证环境，停机丢失记录/制品；源码定义可重装，操作记录不能据此恢复。原 18498、18500、18501、18502、18503、18504 走查实例保留，未为此重启。需要持久化时使用已有 PostgreSQL/FileStore 部署路径，当前结果不构成持久部署验收。

## 接入外部系统时填什么

这些都在 Settings 里添加（登录 `sup@plant.test` 或 `manager@hotel.test`）。

**Webhook 接收地址**（Integrations → Add endpoint → Webhook）

| 用途 | URL | 密钥名 | 勾选 |
|---|---|---|---|
| 通用 webhook | `http://webhook-sink:8080/hook` | `sink` | 勾"内部地址"；事件任选，如 `lodging.booking/1#canceled` |

sink 收到的 webhook 在 http://localhost:8497/received 查看。`POST http://localhost:8497/fail?on=true` 让它开始返回 503，用来测试重试；`?on=false` 恢复。工厂的 ERP 是同一主机里的 ERP 应用，不需要接收地址；接外部 ERP 见 [docs/Testing.md](../../docs/Testing.md) 场景 P2-1。

**邮件接收地址**（Integrations → Add endpoint → Email）

| URL | 发件人 | 密钥名 | 勾选 |
|---|---|---|---|
| `smtp://webhook-sink:2525` | 任意，如 `plant@plant.test` | 留空 | 勾"内部地址"；应用勾 `mes`、`platform` |

收到的邮件在 http://localhost:8497/mail 查看。只有以邮箱登录的成员会收到邮件，服务账号和 AI 代理不会。真实 SMTP 服务器的写法是 `smtp://用户名@smtp.example.com:587`，密码放在密钥里（密钥名填在"密钥名"一栏）。

**AI 供应商**（AI → Providers and models → Add provider）

| 类型 | 填写 | 密钥名 |
|---|---|---|
| 供应商 OpenRouter | ID `openrouter`，供应商选 OpenRouter | `openrouter`（`.env` 里的 `OPENROUTER_API_KEY`） |
| 供应商 OpenAI / Gemini / Moonshot / DeepSeek / Qwen / 智谱 | 选对应供应商 | 自定义名字，并按"启动与停止"一节加上对应的环境变量 |
| 第三方兼容接口 | Base URL `https://…/v1` | 必填 |
| 本地模型：LM Studio / Ollama / llama.cpp | `http://host.docker.internal:1234/v1`、`:11434/v1`、`:8080/v1` | 可留空 |
| 本地替身（无需安装） | `http://webhook-sink:8080/v1`，模型 `echo` | 留空 |

- 添加后点 Models 读取模型目录。勾 "free only" 只看免费模型：OpenRouter 目前有约 20 个免费模型，常被上游限流（429），换一个就行。
- 选 everyone 或 ai users 启用，然后在 Playground 调用，在 Usage 看用量。
- 没有 `ai` 角色的成员（例如 AI 助手 `agent-l1`）只能用开放给 everyone 的模型。

**MCP**：`POST http://localhost:8490/mcp`（或 8495），带 `Authorization: Bearer <该成员的令牌>`。工具列表就是这个成员的动作目录和可读数据。

**外部智能体（A2A）**（Integrations → Add endpoint → A2A，只在 MES 用到）

| URL | 密钥名 | 勾选 |
|---|---|---|
| `http://webhook-sink:8080/a2a`（供应商智能体替身） | 可留空（真实对方填存放其令牌的密钥名） | 勾"内部地址"；外发类型 `mes/lead-time` |

发布我们自己的智能体：App settings → Agents → "Published over A2A" 填 `csm.triage`；卡片在 `http://localhost:8495/a2a/hotel-a/csm.triage/.well-known/agent-card.json`，调用方用成员令牌按 A2A 1.0 JSON-RPC 发 `SendMessage`（请求头 `A2A-Version: 1.0`）。

宿主控制台的管理员由 `-host-admins` 显式指定 OIDC subject，租户的 platform.admin 不自动获得宿主权限。本地酒店配置 `user:manager@hotel.test`，制造配置 `user:sup@plant.test`；其它账号访问 `/v1/host/*` 被拒绝。新租户由模板创建并写入可写的 tenants.json，需在生产配置中选择实际宿主运维身份。

新建租户需要写回挂载的 `tenants.json`。宿主仍以 `nobody` 运行，Compose 用 `TENANTS_GID` 加入文件所属组（macOS 的默认 staff 为 20）；启动前运行 `chmod g+w deploy/local/{hospitality,manufacturing}/tenants.json`，其他系统以 `TENANTS_GID=$(id -g) docker compose -f deploy/local/compose.yaml up -d --build` 启动。文件只含租户配置与身份引用，不含登录密钥。

OIDC delivery 使用 `PLATFORM_PERSONAL_TOKEN_KEY`（至少 32 字节）签发个人令牌，未配置时拒绝启动；该密钥独立于 OIDC 公钥，重启时必须保留，轮换会使已签发的个人令牌失效。开发令牌宿主只用于开发；lightweight 使用其已有的私有签名密钥。
