# 本地环境与测试资料

这里是本地测试环境的全部资料：怎么启动、地址、账号密码、服务账号、接入外部系统要填什么，以及常用的测试路线。

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
- 只重建主机：`docker compose up -d --build mes-server sales-server webhook-sink`。改了前端要先重新构建工作台。
- 灌 sales 演示数据：`./seed-sales.sh`。可以重复执行，结果不变。
- 完整演练：在仓库根目录运行 `scripts/verify.sh deploy`。它用另一组端口和一套全新的数据，不会动你的本地数据。

真实供应商的密钥放在 `deploy/local/.env`，compose 启动时自动读取。一行一个：

```
OPENROUTER_API_KEY=sk-or-...
```

主机按名字取密钥（ADR-0014 D5）：名字 `openrouter` 对应环境变量 `PLATFORM_SECRET_OPENROUTER`。要加新的密钥名，就在 `compose.yaml` 的 `mes-server` / `sales-server` 的 `environment` 里加一行 `PLATFORM_SECRET_<名字大写>: "${变量:-}"`，再把变量写进 `.env`。

## 地址

| 服务 | 地址 | 说明 |
|---|---|---|
| 身份认证 Rauthy | http://localhost:8480/auth/v1/ | OIDC 签发方；管理后台是 http://localhost:8480/auth/v1/admin |
| **工作台（Sales 主机，租户 `hotel-a`）** | http://localhost:8495 | 登录一次，按角色打开 CRM、酒店、HR、设置、收件箱，不用换页面 |
| **工作台（MES 主机，租户 `plant-sz`）** | http://localhost:8490 | 同一个工作台，显示工厂的应用 |
| webhook-sink | http://localhost:8497 | 本地的外部系统替身：webhook 接收方、ERP、邮件服务器、本地模型 |
| PostgreSQL | `localhost:5433`，库 `platform`，用户 `platform`，密码 `platform-local-only` | 两个租户的日志表 `journal` |

主机的接口也在同一个地址下（`/v1/...`）。开发工作台时用 `.claude/launch.json` 的配置：`workspace`（连内存里的 sales 主机 8496，开发令牌）、`workspace-plant`（连 8491）、`workspace-oidc`（连 Docker 里的 8495，要登录），页面在 http://localhost:5176（`workspace-plant` 是 5175）。

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
| `sales@hotel.test` | Sales `hotel-a` | `sales-1` | crm 销售、hotel 前台、memstay 管家、hr 员工；ai 用户 | 销售组、2026 年会项目 |
| `manager@hotel.test` | Sales | `manager-1` | crm 销售经理、hotel 经理、memstay 管家、hr 人事、work 管理员；platform、org、ai 管理员 | 酒店总经理等；`hotel-a` 经理、`hospitality` 负责人（两级审批人） |

- 本地 Rauthy 走 HTTP，所以 `rauthy/config.toml` 里设了 `[access] cookie_mode = 'danger-insecure'`：不设的话，Safari 会丢掉 Rauthy 的安全 cookie，浏览器登录会显示密码错误（密码其实是对的）。只用于本地。
- Rauthy 管理员：`admin@platform.test`，密码 `Admin-Local-Only-1`。
- 成员名单来自 `mes/directory.json` 和 `sales/directory.json`。改了要重建对应主机才生效；在 Settings 里授予的角色是决策，会保存在日志里。

## 服务账号与 AI 代理（client credentials）

令牌地址是 `http://localhost:8480/auth/v1/oidc/token`，用 `grant_type=client_credentials`。

| client_id | 密钥 | 成员 | 用途 |
|---|---|---|---|
| `mes-gateway` | `gatewayLocalOnly000000000000000000000000000000000000000000000000` | `gateway-l1` | 产线网关，推送设备状态（`cmd/gateway-sim`） |
| `mes-erp` | `erpLocalOnly0000000000000000000000000000000000000000000000000000` | `erp` | ERP 计划订单轮询 |
| `mes-assistant` | `assistantLocalOnly0000000000000000000000000000000000000000000000` | `agent-l1` | **AI 代理**：产线 L1 的助手；它做的不可撤回外发要人批准（D6） |
| `platform-cli` | `cliLocalOnly0000000000000000000000000000000000000000000000000000` | — | 脚本用密码模式为人员换令牌（`rehearse.sh`、`seed-sales.sh`） |

以 AI 助手身份操作：

```bash
cd slices/manufacturing/server && MES_AGENT_CLIENT=mes-assistant MES_AGENT_SECRET=assistantLocalOnly0000000000000000000000000000000000000000000000 go run ./cmd/mes-agent -server http://localhost:8490 -oidc-token http://localhost:8480/auth/v1/oidc/token actions
```

把末尾的 `actions` 换成 `do <动作> <目标> '<JSON>'` 就是执行动作，例如 `do mes.order.reconfirm WO-3 '{"planned":"PO-9001"}'`。

## 开发令牌（不登录的演示模式）

用 `go run ./cmd/<server>` 直接启动的主机（不带 `-oidc-issuer`）接受开发令牌：令牌就是登录名。
- sales：`manager`、`sales`、`sales-only`、`desk`
- MES：`supervisor`、`operator-l1`、`operator-l2`、`quality-1`、`quality-2`、`gateway-l1`、`erp`、`assistant-l1`（AI 代理）

开发令牌的主机不占 Docker 的端口：在 `solutions/sales` 下运行 `go run ./cmd/sales-server -addr 127.0.0.1:8496`，或在 `slices/manufacturing/server` 下运行 `go run ./cmd/mes-server -addr 127.0.0.1:8491`，再用启动配置 `workspace` / `workspace-plant` 打开工作台，右上角的身份菜单可以切换开发身份。要带 OpenRouter 密钥，就先 `set -a; . deploy/local/.env; set +a`，再加上 `PLATFORM_SECRET_OPENROUTER=$OPENROUTER_API_KEY`。开发主机只在内存里，停掉数据就没了。

## 接入外部系统时填什么

这些都在 Settings 里添加（登录 `sup@plant.test` 或 `manager@hotel.test`）。

**Webhook 接收地址**（Integrations → Add endpoint → Webhook）

| 用途 | URL | 密钥名 | 勾选 |
|---|---|---|---|
| 通用 webhook | `http://webhook-sink:8080/hook` | `sink` | 勾"内部地址"；事件任选，如 `lodging.booking/1#canceled` |
| ERP 确认回写（MES） | `http://webhook-sink:8080/erp` | `sink` | 勾"内部地址"；外发类型 `mes/erp-confirmation` |

sink 收到的 webhook 和 ERP 确认号在 http://localhost:8497/received 查看。`POST http://localhost:8497/fail?on=true` 让它开始返回 503，用来测试重试；`?on=false` 恢复。ERP 替身在确认里没写计划订单时返回 422，用来测试 ERP 拒绝和纠正。

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

## 常用测试路线

1. **ERP 回写、纠正、D6 批准、邮件**（MES，`sup@plant.test`）：
   1. 添加 ERP 和邮件两个接收地址；
   2. 在 MES 下达一张不带计划订单的订单，`op1` 做完三道工序；
   3. ERP 会拒绝，主管收到通知和邮件；
   4. 在计划订单页"Correct and resend"，或者用 AI 助手执行 `mes.order.reconfirm`；
   5. 助手重发的那条外发会被扣住，在 Integrations 批准后 ERP 确认。
2. **协议与供应商切换**（Sales，`manager@hotel.test`）：
   1. 用 CRM 订房（走 lodging 协议）；
   2. 在 Protocols 把供应商从 hotel 切到 memstay 后再订一次；
   3. 两个供应商的入住记录都挂在同一个商机上。
3. **AI**（任一主机的管理员）：添加 OpenRouter → 启用一个免费模型 → Playground 提问 → Usage 看用量；再换 `op1` 或 `sales` 登录，看不同访问范围的效果。
4. **请假审批**（Sales）：
   1. `sales@hotel.test` 在 Leave requests 新建一条请假，打开后点 Submit，提示"已提交审批"，My requests 里能看到；
   2. `manager@hotel.test` 在 Inbox 里批准；超过 5 天的请假还要第二级（部门负责人，也是 `manager-1`），再批一次；
   3. 批完后请假变成 approved，`sales` 收到通知。也可以试驳回、撤回，或者在审批期间取消请假（最后批准时会被拒绝并写明原因）。
5. **流程**（Sales）：
   1. `sales@hotel.test` 在 CRM 的客户页给一个商机点 "Plan group stay"（比如 2 间 standard，填入住和离店日期），再点 Won；
   2. "团队住宿"流程会通过住宿协议一间一间订房，然后在收件箱问你：confirmed 还是 release；
   3. 选 release（或者两天没人回答），已订的房会按倒序取消；选 confirmed，流程结束；
   4. `manager@hotel.test` 在设置 → Processes → Flows 看所有流程和实例，点开实例能看到每一步和原因，卡住的可以重试、跳过、取消。
   工厂那边：订单最后一个 SFC 完成后，"ERP 确认"流程自动发确认；ERP 拒绝时主管收件箱里会有"修正并重发"的任务，重发后任务自动关闭。
6. **智能体**（MES，`sup@plant.test`）：
   1. 先在设置里给智能体选一个模型：App settings → Agents → "Model for agents"，填一个已启用、支持工具调用的模型，比如 `openrouter/<某个支持 tools 的模型>`，或者本地替身 `local/echo`（先在 AI → Providers 添加 local，地址 `http://webhook-sink:8080/v1`，再启用 echo）；
   2. 下达一张不带计划订单的订单（比如 P-200，数量 8），让操作员把 SFC 做完；
   3. ERP 拒绝后，"ERP 确认"流程让工厂的智能体去找对应的计划订单，主管收件箱会出现"Resend WO-x to the ERP against PO-xxxx?"，选 resend 后流程重发、ERP 确认；
   4. 智能体每一步（调用了什么工具、理由、结果、用了多少 token）记在它的运行记录上：设置 → Processes → Agents 下面的 Runs，点开能看到每一步和理由；主管在收件箱的回答（接受或自己改）也记在上面，作为以后评估的依据。没设模型时，智能体会停下，主管自己改。
   5. 手工纠正时，计划订单必须对得上：同一产品、数量够、没被别的订单占用，否则会被拒绝（目前只显示 invalid argument，原因见工作队列 F-23）。
7. **帮助台**（Sales，`manager@hotel.test`，他是帮助台 lead）：
   1. 和第 6 条一样先给 Sales 设智能体模型（AI → Providers 添加 local 并启用 echo，或者用 OpenRouter 的支持 tools 的模型；再在 App settings → Agents 填模型）；在 Integrations 添加一个接收地址，勾选 effect `helpdesk/reply`（本地可以用 `http://webhook-sink:8080/hook`，秘钥随便填，允许私有地址）；
   2. 打开 Helpdesk → Open ticket，客户账号填 `ACME`（先在 CRM 建好这个账户和商机，模型才能查到）；
   3. 分诊智能体会分类、定优先级（优先级决定回复期限），再回复；因为是智能体写的回复，邮件会被扣住，Notifications 里会有"Approve Reply to the customer"，到 Integrations 批准后才发出去（本地在 `http://127.0.0.1:8497/received` 能看到）；
   4. 回复里承诺退款、补偿、折扣的会被智能体的规则拒绝，工单留给人处理；模型不可用时工单直接进帮助台的收件箱；到期还没回复的，lead 会收到"Late ticket"任务和通知。
8. **助手和评估**（任一主机）：
   1. 打开任一记录（比如 CRM 的商机，或 MES 的订单），点 "Ask the assistant"，选智能体（Sales 的 "Sales assistant"，MES 的 "ERP correction"），写下要做什么，比如"把这个商机标记为赢单"；
   2. 智能体替你干活时不会直接改数据：它要做的动作会作为草稿等你确认，你可以改字段后 Confirm，或写原因 Reject（它会接着想办法）；这些都记为这次运行的反馈；
   3. 左侧 Search 可以跨所有你能看的类型搜记录；
   4. 管理员在设置 → Processes → Evaluations 选一个智能体和一个候选模型点 Evaluate：它用候选模型把有人确认或纠正过的历史运行"干跑"一遍（只检查、不执行），报告里每条都会标出与人接受的一致、不一致、重犯被纠正的错误，或避开了它。
9. **重启与恢复**：`docker compose restart mes-server sales-server` 之后数据都在（日志重放）。已送达的 webhook 和邮件不会重发。
