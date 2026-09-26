# 本地环境

这里是本地环境的全部资料：怎么启动、地址、账号密码、服务账号、接入外部系统要填什么。测试路线和测试方法在 [docs/Testing.md](../../docs/Testing.md)。

本页的密码和密钥都只用于本地环境，而且早已写在仓库的其他文件里（Rauthy 的初始数据、`rehearse.sh`）。**唯一不进仓库的是真实供应商的 API Key**，比如 OpenRouter：它放在 `deploy/local/.env`，该文件已被 git 忽略。

## 启动与停止

需要先有 Docker（OrbStack：`orb start`）。

每个主机自己提供工作台页面（ADR-0018），所以先构建一次工作台：

```bash
pnpm --dir web/apps/workspace build
```

```bash
cd deploy/local && docker compose up -d --build
```

```bash
cd deploy/local && docker compose ps
```

- 停止：`docker compose stop`。数据保存在 Docker 卷 `pgdata` 和 `rauthy` 里，下次启动会重放日志，数据还在。
- 只重建主机：`docker compose up -d --build manufacturing-server hospitality-server webhook-sink`。改了前端要先重新构建工作台。
- 灌酒店业演示数据：`./seed-hospitality.sh`。可以重复执行，结果不变。
- 完整演练：在仓库根目录运行 `scripts/verify.sh deploy`。它用另一组端口和一套全新的数据，不会动你的本地数据。

真实供应商的密钥放在 `deploy/local/.env`，compose 启动时自动读取。一行一个：

```
OPENROUTER_API_KEY=sk-or-...
```

主机按名字取密钥（ADR-0014 D5）：名字 `openrouter` 对应环境变量 `PLATFORM_SECRET_OPENROUTER`。要加新的密钥名，就在 `compose.yaml` 的 `manufacturing-server` / `hospitality-server` 的 `environment` 里加一行 `PLATFORM_SECRET_<名字大写>: "${变量:-}"`，再把变量写进 `.env`。

## 地址

- 文件（ADR-0028）：存在 RustFS（S3 兼容），S3 接口 `http://127.0.0.1:9000`，管理界面 `http://127.0.0.1:9001`，账号 `platform`，密码 `platform-files-local-only`。日志里只记文件的哈希，备份时 RustFS 的卷要和 PostgreSQL 一起备份。
- 健康检查：`http://127.0.0.1:8490/healthz`、`http://127.0.0.1:8495/healthz`（进程是否活着）；租户的健康（队列、放弃的工作、超配额、熔断器）是管理员的 `/v1/health`，也显示在 设置 → 自动化。
- 链路和指标（ADR-0027）：给主机设环境变量 `OTEL_EXPORTER_OTLP_ENDPOINT`（如 `http://otel-collector:4318`）就会按 OTLP 导出；不设则不导出。

| 服务 | 地址 | 说明 |
|---|---|---|
| 身份认证 Rauthy | http://localhost:8480/auth/v1/ | OIDC 签发方；管理后台是 http://localhost:8480/auth/v1/admin |
| **工作台（酒店业主机 hospitality-server，租户 `hotel-a`）** | http://localhost:8495 | 登录一次，按角色打开 CRM、PMS、HCM、CSM、设置、收件箱，不用换页面 |
| **工作台（制造业主机 manufacturing-server，租户 `plant-sz`）** | http://localhost:8490 | 同一个工作台，显示工厂的应用：MES 和 ERP。日志为空时（第一次启动）主机给 ERP 建好科目、打开本月期间、建好钢材和两种产品 |
| webhook-sink | http://localhost:8497 | 本地的外部系统替身：webhook 接收方、ERP、邮件服务器、本地模型 |
| PostgreSQL | `localhost:5433`，库 `platform`，用户 `platform`，密码 `platform-local-only` | 两个租户的日志表 `journal` |

主机的接口也在同一个地址下（`/v1/...`）。开发工作台时用 `.claude/launch.json` 的配置：`workspace-hospitality`（连内存里的酒店业主机 8496，开发令牌）、`workspace-manufacturing`（连 8491）、`workspace-app`（连单个应用的开发主机 8499）、`workspace-oidc`（连 Docker 里的 8495，要登录），页面在 http://localhost:5176（`workspace-manufacturing` 是 5175，`workspace-app` 是 5177）。

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
| `sup@plant.test` | MES `plant-sz` | `sup-1` | mes 主管；platform、org、ai 管理员 | 工厂 `plant-sz`（管两条线） |
| `op1@plant.test` | MES | `op-l1` | mes 操作员；ai 用户 | 产线 `L1` |
| `op2@plant.test` | MES | `op-l2` | mes 操作员；ai 用户 | 产线 `L2` |
| `qa1@plant.test` | MES | `qa-1` | mes 质量；ai 用户 | — |
| `qa2@plant.test` | MES | `qa-2` | mes 质量；ai 用户（报废需要两个质量签名） | — |
| `sales@hotel.test` | 酒店业 `hotel-a` | `sales-1` | crm 销售、pms 前台、memstay 管家、hcm 员工；ai 用户 | 销售组、2026 年会项目 |
| `manager@hotel.test` | 酒店业 | `manager-1` | crm 销售经理、pms 经理、memstay 管家、hcm 人事（hr）、csm lead、work 管理员；platform、org、ai 管理员 | 酒店总经理等；`hotel-a` 经理、`hospitality` 负责人（两级审批人） |

- 本地 Rauthy 走 HTTP，所以 `rauthy/config.toml` 里设了 `[access] cookie_mode = 'danger-insecure'`：不设的话，Safari 会丢掉 Rauthy 的安全 cookie，浏览器登录会显示密码错误（密码其实是对的）。只用于本地。
- Rauthy 管理员：`admin@platform.test`，密码 `Admin-Local-Only-1`。
- 成员名单来自 `manufacturing/directory.json` 和 `hospitality/directory.json`。`sup@plant.test` 同时是 ERP 的主管会计（controller）。改了要重建对应主机才生效；在 Settings 里授予的角色是决策，会保存在日志里。

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

## 接入外部系统时填什么

这些都在 Settings 里添加（登录 `sup@plant.test` 或 `manager@hotel.test`）。

**Webhook 接收地址**（Integrations → Add endpoint → Webhook）

| 用途 | URL | 密钥名 | 勾选 |
|---|---|---|---|
| 通用 webhook | `http://webhook-sink:8080/hook` | `sink` | 勾"内部地址"；事件任选，如 `lodging.booking/1#canceled` |

sink 收到的 webhook 在 http://localhost:8497/received 查看。`POST http://localhost:8497/fail?on=true` 让它开始返回 503，用来测试重试；`?on=false` 恢复。工厂的 ERP 是同一主机里的 ERP 应用，不需要接收地址；接外部 ERP 见路线 16。

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
