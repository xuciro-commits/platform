# 测试

怎么测试平台，以及每条测试路线怎么走。环境（启动、地址、账号、开发令牌、接入外部系统要填什么）在 [deploy/local/README.md](../deploy/local/README.md)。

## 测的是什么

测试某个应用，目的不是找这个应用自己的业务漏洞，而是透过它看**平台的设计**：应用的整体设计、框架和内核有没有缺陷。一个问题如果换个应用也会出现，它就是平台的问题。

每次发现的问题：
- 记进 [WorkQueue.md](WorkQueue.md) 的"Open friction"，编号 `F-n`，写清楚现象、它暴露的是平台哪一层的缺陷、怎么解决；
- 修好以后删掉那一行，把结论写进对应的 ADR 或 [Platform.md](Platform.md)。

## 规则

1. **每条路线都在界面上走通过，才写进本文。** 演练脚本（`deploy/local/rehearse.sh`）和测试直接调接口，界面上走不通的地方它们发现不了（F-33）。路线写的是人点什么，不是接口怎么调。
2. **每个应用先能自己干**（ADR-0025 D3）：每条涉及外部系统的路线，都要有一条不接外部系统、只靠这个应用自己走通的对应路线。
3. **失败也是路线的一部分**：被拒绝、超时、没人处理时，谁在哪里看到了什么，要写出来。
4. 自动检查是 `scripts/verify.sh`（见 AGENTS.md "Verify"）；本文是人要亲手走的部分。

## 测试路线

1. **ERP 确认、纠正、邮件**（制造业主机，`sup@plant.test`，ADR-0024）：
   1. 添加邮件接收地址；在 ERP → 生产订单新建一张 P-100、数量 1 的订单并"下达"；
   2. 在 MES → 车间订单点"下达车间订单"：选 P-100、数量 1、SFC 1，计划订单留空（工厂自主下达，ADR-0025 D3），`op1` 做完三道工序；
   3. 工厂立刻拒绝确认（没有计划订单），主管收到通知和邮件；
   4. 在计划订单页"Correct and resend"，选刚才下达的 ERP 生产订单，ERP 确认，车间订单上显示凭证号 `MJ/…`；
   5. 如果让 AI 助手执行 `mes.order.reconfirm`，ERP 会拒绝（助手在 ERP 没有角色），要主管自己重发。
2. **协议与供应商切换**（酒店业主机，`manager@hotel.test`）：
   1. 用 CRM 订房（走 lodging 协议）；
   2. 在 Protocols 把供应商从 pms 切到 memstay 后再订一次；
   3. 两个供应商的入住记录都挂在同一个商机上。
3. **AI**（任一主机的管理员）：添加 OpenRouter → 启用一个免费模型 → Playground 提问 → Usage 看用量；再换 `op1` 或 `sales` 登录，看不同访问范围的效果。
4. **请假审批**（酒店业主机，HCM）：
   1. `sales@hotel.test` 在 Leave requests 新建一条请假，打开后点 Submit，提示"已提交审批"，My requests 里能看到；
   2. `manager@hotel.test` 在 Inbox 里批准；超过 5 天的请假还要第二级（部门负责人，也是 `manager-1`），再批一次；
   3. 批完后请假变成 approved，`sales` 收到通知。也可以试驳回、撤回，或者在审批期间取消请假（最后批准时会被拒绝并写明原因）。
5. **流程**（酒店业主机）：
   1. `sales@hotel.test` 在 CRM 的客户页给一个商机点 "Plan group stay"（比如 2 间 standard，填入住和离店日期），再点 Won；
   2. "团队住宿"流程会通过住宿协议一间一间订房，然后在收件箱问你：confirmed 还是 release；
   3. 选 release（或者两天没人回答），已订的房会按倒序取消；选 confirmed，流程结束；
   4. `manager@hotel.test` 在设置 → Processes → Flows 看所有流程和实例，点开实例能看到每一步和原因，卡住的可以重试、跳过、取消。
   工厂那边：订单最后一个 SFC 完成后，"ERP 确认"流程自动发确认；ERP 拒绝时主管收件箱里会有"修正并重发"的任务，重发后任务自动关闭。
6. **智能体**（MES，`sup@plant.test`）：
   1. 先在设置里给智能体选一个模型：App settings → Agents → "Model for agents"，填一个已启用、支持工具调用的模型，比如 `openrouter/<某个支持 tools 的模型>`，或者本地替身 `local/echo`（先在 AI → Providers 添加 local，地址 `http://webhook-sink:8080/v1`，再启用 echo）；
   2. 先在 ERP 下达一张 P-200、数量 8 的生产订单；再在 MES 下达一张不带计划订单的车间订单（P-200，数量 8），让 `op2` 把 SFC 做完；
   3. 工厂拒绝后，"ERP 确认"流程让工厂的智能体去找对应的计划订单，主管收件箱会出现"Resend WO-x to the ERP against MO-x?"，选 resend 后流程重发、ERP 确认；
   4. 智能体每一步（调用了什么工具、理由、结果、用了多少 token）记在它的运行记录上：设置 → Processes → Agents 下面的 Runs，点开能看到每一步和理由；主管在收件箱的回答（接受或自己改）也记在上面，作为以后评估的依据。没设模型时，智能体会停下，主管自己改。
   5. 手工纠正时，计划订单必须对得上：同一产品、数量够、没被别的订单占用，否则会被拒绝（目前只显示 invalid argument，原因见工作队列 F-23）。
7. **客户服务 CSM**（酒店业主机，`manager@hotel.test`，他是客户服务的 lead）：
   1. 和第 6 条一样先给酒店业主机设智能体模型（AI → Providers 添加 local 并启用 echo，或者用 OpenRouter 的支持 tools 的模型；再在 App settings → Agents 填模型）；在 Integrations 添加一个接收地址，勾选 effect `csm/reply`（本地可以用 `http://webhook-sink:8080/hook`，秘钥随便填，允许私有地址）；
   2. 打开 CSM → Open ticket，客户账号填 `ACME`（先在 CRM 建好这个账户和商机，模型才能查到）；
   3. 分诊智能体会分类、定优先级（优先级决定回复期限），再回复；因为是智能体写的回复，邮件会被扣住，Notifications 里会有"Approve Reply to the customer"，到 Integrations 批准后才发出去（本地在 `http://127.0.0.1:8497/received` 能看到）；
   4. 回复里承诺退款、补偿、折扣的会被智能体的规则拒绝，工单留给人处理；模型不可用时工单直接进客服的收件箱；到期还没回复的，lead 会收到"Late ticket"任务和通知。
8. **助手和评估**（任一主机）：
   1. 打开任一记录（比如 CRM 的商机，或 MES 的订单），点 "Ask the assistant"，选智能体（酒店业主机的 "Sales assistant"，MES 的 "ERP correction"），写下要做什么，比如"把这个商机标记为赢单"；
   2. 智能体替你干活时不会直接改数据：它要做的动作会作为草稿等你确认，你可以改字段后 Confirm，或写原因 Reject（它会接着想办法）；这些都记为这次运行的反馈；
   3. 左侧 Search 可以跨所有你能看的类型搜记录；
   4. 管理员在设置 → Processes → Evaluations 选一个智能体和一个候选模型点 Evaluate：它用候选模型把有人确认或纠正过的历史运行"干跑"一遍（只检查、不执行），报告里每条都会标出与人接受的一致、不一致、重犯被纠正的错误，或避开了它。
9. **知识与记忆**（酒店业主机，`manager@hotel.test`）：
   1. AI → Providers 启用本地替身的 `embed` 模型，再在 App settings → Knowledge 把 "Embedding model" 设为 `local/embed`（不设也能按关键词检索）；
   2. Knowledge → Documents 新建一篇"House rules"，比如写上 Wifi 密码在房卡上、前台可以重置；左侧 Search 搜 "wifi password" 能看到这段；
   3. 按第 7 条开一张"Wifi keeps dropping"的工单：分诊智能体的回复会引用 House rules，运行记录上列出引用的文档；管理员在运行页还能看到每次模型调用的完整请求和回答（保留 30 天）；
   4. 在第 8 条里改掉或驳回智能体的草稿后，它会提议一条记忆；助手面板里能看到"智能体记得关于你的事"，保留后下次运行会用上，也可以随时忘掉；设置 → Processes 列出所有记忆。
10. **智能体互调（A2A）**：
    1. MES（`sup@plant.test`）：按上面的表添加供应商智能体的接收地址，然后在任一记录上 "Ask the assistant"，选 "Material planner"，问 "What is the lead time of P-200?"，它通过 A2A 问供应商智能体，回答 12 天；
    2. 酒店业主机（`manager@hotel.test`）：按上面把 `csm.triage` 发布出去，用 curl 或任一 A2A 客户端发一条 `SendMessage`，任务完成后返回分诊结果；它以调用者的权限直接行动，外发的邮件照样要人批准。
11. **多语言**（任一主机，ADR-0023）：右上角头像菜单 → 语言 → 简体中文，页面会重新加载：导航、按钮、列表、记录页、设置，以及各应用的实体、字段、状态、动作名称都变成中文（记录内容是谁写的就是什么语言，不翻译）；在记录上"问助手"，智能体会用中文写理由和结果（需要真实模型，本地替身 echo 不会说中文）。切回 English 同样在这个菜单。你选的语言会存成你自己的偏好，换浏览器登录也一样；管理员可以在 App settings → Settings 设"Default language"（如 `zh-CN`）作为租户默认。通知、收件箱任务、审批和邮件也会按读者的语言显示（应用写的英文按词典里的句式翻译）。
    术语表：设置 → 知识 → 术语表 → 新建术语，比如术语 `单子`，含义"销售对商机的叫法"，指向 `crm.opportunity`；之后在搜索里输入"单子 年度"会只在商机里找"年度"，智能体的提示词里也会带上这些术语。术语只能指向已有的实体、字段或动作，不会改变它们本身。
12. **重启与恢复**：`docker compose restart manufacturing-server hospitality-server` 之后数据都在（日志重放）。已送达的 webhook 和邮件不会重发。
13. **新建一个应用**（不需要 Docker，按 `docs/Apps.md`，ADR-0023）：
    1. 在 `capabilities/server` 运行 `go run ./cmd/new-app -id purchasing -entity request -title "Purchase request" -zh 采购申请 -app-title Purchasing -app-zh 采购`，它会写好 `apps/purchasing/server`（实体、动作、审核流程、中文词典、测试、开发主机）和 `web/packages/purchasing`（界面，已登记到工作台）；
    2. `cd apps/purchasing/server && go test ./...` 应该直接通过；
    3. `pnpm --dir web/apps/workspace build`，再在 `apps/purchasing/server` 运行 `go run ./cmd/purchasing-server -web ../../../web/apps/workspace/dist`，打开 `http://127.0.0.1:8499`，用令牌 `member` 新建一张采购申请，再用令牌 `manager` 登录，收件箱里会有"审核 …"，点"完成"后申请变成"已完成"；
    4. 用完删掉：`rm -rf apps/purchasing web/packages/purchasing`，再 `git checkout web/apps/workspace web/pnpm-lock.yaml`。也可以让编码智能体用 `new-app` 技能照着这条路径加实体、动作、流程和翻译。
14. **ERP 记账**（不需要 Docker，ADR-0024）：
    1. `pnpm --dir web/apps/workspace build`，再在 `apps/erp/server` 运行 `go run ./cmd/erp-server -web ../../../web/apps/workspace/dist`，打开 `http://127.0.0.1:8499`，用令牌 `controller`（主管会计）登录；开发主机已经建好一套科目、打开本月的会计期间，本位币是 CNY；
    2. 财务 → 会计凭证 → 新建凭证：选科目、填借方和贷方（可以"添加一行"），保存草稿；打开这张凭证点"过账"：借贷不平、科目不存在、期间已关账都会被拒绝，被拒绝的不占编号；过账成功后得到编号 `GJ/2026/00001`，下面"分录"列出每一行；
    3. 过账后的凭证不能再改，只能"冲销"：会生成一张借贷互换的冲销凭证，编号接着排；
    4. 试算平衡表：每个科目的借方、贷方合计和余额，借方合计等于贷方合计；会计期间：关账后该月的凭证不能过账，"重新打开"后可以；
    5. 客户服务（CSM，酒店业主机）新开的工单也会有连续编号 `CS-2026-0001`；
    6. 采购：用令牌 `buyer`（采购员）登录，采购与库存 → 采购订单 → 新建采购订单，选供应商 Suzhou Steel Co.、物料 M-STEEL，填数量和单价，保存草稿；打开订单点"下单"（得到 `PO/2026/00001`）。合计达到 10 000 的订单要主管会计先审批：用 `controller` 登录，在收件箱里批准后订单才下单（限额在应用设置 ERP → 审批限额 里改）；
    7. 到货后点"收货"：现存量里出现这批货（按标准成本计价），试算平衡表里原材料按标准成本入账、暂估应付按订单价格、差额记到采购价格差异；再用 `controller` 或 `accountant` 点"登记发票"，暂估应付被冲平，计入应付账款。
15. **ERP 与 MES 联动：生产订单**（不需要 Docker，ADR-0024）：
    1. `pnpm --dir web/apps/workspace build`，再在 `solutions/manufacturing` 运行 `go run ./cmd/manufacturing-server -web ../../web/apps/workspace/dist`，打开 `http://127.0.0.1:8491`，用令牌 `supervisor` 登录（他同时是 ERP 的主管会计）；ERP 已经有科目、本月期间、钢材 M-STEEL 和两种产品（P-100 泵壳每件用 2 kg 钢）；
    2. ERP → 生产订单 → 新建：产品 P-100、数量 2，保存后打开点"下达"，得到 `MO/2026/00001`；
    3. 工厂 → 下达车间订单：产品 P-100、数量 2，计划订单填 `MO-…`（ERP 生产订单的 ID）；超过计划数量或草稿状态的订单会被拒绝；
    4. 用 `operator-l1` 登录，把 SFC 依次在 FURNACE-1、CNC-11、CMM-1 开工、完工；订单完成后，确认流程通过协议 `production.orders/1` 直接报给 ERP：工厂订单上显示"已确认 MJ/2026/00001"（ERP 生产日记账的凭证号），ERP 的生产订单变为已确认，记下车间订单和产出；
    5. ERP 里看结果：现存量里钢材减少、P-100 增加；试算平衡表里原材料转入生产成本再转入库存商品，差额计入生产差异；如果 ERP 的期间已关账，工厂会立刻收到"ERP refused"，重新打开期间后主管在工厂订单上"重新发送"即可。
16. **外部 ERP 适配器**（不需要 Docker，ADR-0024 7d）：工厂接 SAP 这类外部 ERP 时，把 ERP 应用换成 ERP 适配器 `erpadapter`，工厂这边完全不变：
    1. 先起 Docker（用它的 sink 当外部 ERP），再在 `solutions/manufacturing` 运行 `PLATFORM_SECRET_SINK=sinkLocalOnly0000000000000000000000000000000000000000000000000000 go run ./cmd/manufacturing-server -erp external -web ../../web/apps/workspace/dist`；
    2. 外部 ERP 的轮询程序用令牌 `erp` 送一页计划订单：`curl -H 'Authorization: Bearer erp' http://127.0.0.1:8491/v1/connectors/planned-orders -d '{"cursorTo":"p1","orders":[{"id":"PO-9001","product":"P-100","quantity":2}]}'`；同一页再送一次会被拒绝；
    3. 用 `supervisor` 登录，Integrations 添加 Webhook 接收地址 `http://127.0.0.1:8497/hook`，密钥名 `sink`，勾"内部地址"，外发类型勾 `erpadapter/confirmation`（sink 收下就算 ERP 确认，但不给确认号）；
    4. 按 PO-9001 下达车间订单并做完：工厂订单先显示"已发送"，ERP 回复后变成"已确认"；ERP 拒绝或送不到时（`POST http://127.0.0.1:8497/fail?on=true`，等重试用完）变成"失败"，主管收到通知，可以纠正后重发；ERP 订单在 ERP adapter → ERP 订单里能看到每次发送和回复。
