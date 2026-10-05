# ADR-0050：模型计费与运行范围（已实施）

**状态：** 已实施，2026-10-05。来源是 `docs/review/README.md` 的会合评审（细节 `docs/review/ai-priorities.md` 的 AI-01…AI-05）。负责人已授权自行决策：本 ADR 记录已落地的决定与边界，不再逐条等待答复。
**范围：** 流程/应用发起的 Agent 运行能看到什么（读范围）、取消/暂停与迟到回答的关系、`/v1/ai/chat` 与 Agent 的模型调用如何记账（本地限制、原子放行、按人计量），以及"0 到底表示什么"。不改变权限模型、不新增角色、不引入第二套运行状态机。
**设计日期：** 2026-10-05。代码事实基线：`2a952715` 与其上的工作树。

## 1. 背景：评审指出的五条静态风险

`docs/review/README.md` 的会合评审（静态读码，未跑测试、未做浏览器走查）把 AI 相关的风险排成 AI-01…AI-06，其中五条要在本稿落地：

| 编号 | 评审描述 | 结论 |
|---|---|---|
| AI-01 | 流程里的 Agent 工具 `context`/`search` 通过 `Tenant.host()` 读取，可能读到该 Agent 应用之外的范围 | 确认为缺陷：读取范围应等于运行自己的应用，不是宿主 |
| AI-02 | 取消/暂停后，已经发出的模型调用返回时没有运行状态/代际栅栏，仍可能提交工具意图 | 确认为缺陷：迟到回答必须按状态丢弃，且计量不能丢 |
| AI-03 | `/v1/ai/chat` 无本地体积上限；调用方可自定 `MaxTokens` 且不受平台上限约束；`Allow()`/`Meter()` 非原子；每日 tokens `0` = 不限 | 确认为缺陷：上限、原子放行、按人计量都要有 |
| AI-04 | 对话记录默认 30 天落盘；`transcript-days <= 0` 只停止清扫，不停止保存 | 确认为缺陷：`0` 必须表示不保留 |
| AI-05 | `0` 既可能表示"未知用量/成本"，也可能表示"零"，且运行没有 USD 预算 | 确认为缺陷：未知与零要分开，运行要有成本预算 |

AI-06（注入与记忆来源/撤回）不改代码，落到 `docs/Goal.md` 的不可信输入约定，见 §6。

## 2. 决定

| 决定 | 内容 | 落地位置 |
|---|---|---|
| D1 读取范围＝运行自己的应用 | Agent 运行以"该应用的应用读者"身份读取：`&Member{ID: "app:"+app, Roles: {app: AnyMember}}`。人代跑（`OnBehalf` 非空）时仍以那个人身份读取，与 ADR-0047 的委派语义一致 | `context.go` `appReader`、`agent_engine.go` `readsAs` |
| D2 上下文与检索拒绝陌生读者 | `Context`/`Search` 在读者缺失或读到别的租户/应用时返回 `NOT_FOUND`（检索返回空集），不再退回宿主。`Tenant.host()` 不再是读者 | `context.go` |
| D3 迟到回答先过栅栏 | 每次模型调用返回后先确认运行仍在跑（`stillRunning`），不在跑就记账并丢弃，绝不提交工具意图；`apply()` 再校验一次（只接受 `State=="running"` 的运行与 `State=="queued"` 的评测报告） | `agent_engine.go` `stillRunning`/`apply` |
| D4 一次思考一轮调用 | 模型的调用在租户锁之外进行：`take()` 取一轮，`agentStep()` 在锁外调用模型，`apply()` 带着栅栏写回。取消/暂停因此不会与一个正在飞行的调用相互等待 | `agent_engine.go` `take`/`agentStep`/`apply` |
| D5 放行＝原子预留 | 平台按人放行模型调用用"预留 + 释放"：`Reserve` 在同一次加锁下检查并占用配额（`Allow` 不再单独使用），`Meter` 在同一次加锁下归还最旧一笔并记账。回放（`Restore`）不带预留直接落账 | `apps/ai/ai.go`、`aicall.go` `limits`/`reserve`、各调用门 |
| D6 全部调用都要有上限与计量 | `/v1/ai/chat` 请求体上限 1 MiB；`max-tokens` 上限在 `Tenant.call` 前置里统一夹紧（请求缺省或超过上限时取上限；上限为 `0` = 用服务商默认）；Agent 的每一步、知识库嵌入、评测的试跑都走同一放行与计量路径 | `server.go`、`aicall.go`、`knowledge.go`、`agent_eval.go`、`agent_engine.go` |
| D7 未知不等于零 | 服务商没报用量时，平台按请求与回答**保守估算**（约每 3 字节 1 token）并标记为估算（`tokensEstimated`），所以"未知"既不读作免费、也不阻断记账；报 `0` 才记 `0`（`tokensReported`）。成本没报就只记"未报"（`costReported=false`），聚合里单列 `costUnknown` 的调用数，不假装是 `0`。运行的 USD 预算只对已报成本生效 | `apps/ai/ai.go` `Usage`/`Total`、`aicall.go` `estimateTokens`、`knowledge.go`、`platform/agent.go` `Budget.Cost` |
| D8 `0` 的含义逐项写明 | 每日 tokens `0` = 不限；单次上限 `0` = 用服务商默认；`transcript-days 0` = 不保留对话记录并遗忘已保留内容。每一项都写进设置描述与中文文案 | `agent.go`、`transcripts.go`、`i18n/zh-CN.json` |
| D9 运行预算在调用前检查 | 一步之前先看预算是否已花完（步数、token、USD），花完就直接收尾并写出原因，不再多花一次调用；跨过预算的那次回答照常记账与回报 | `agent_engine.go` `spentOut`/`budgetReason` |
| D10 评测也有总额 | 一次评测的额度是定义预算 × 计划运行数（用例数 × 每例 3 次），每个 dry run 只在"定义预算与评测余额中更小者"内进行，余额用尽不再开始新运行并写明原因；评测报告同时给出整次与每例的已报成本（USD），未知成本的调用不计入 | `agent_eval.go` `evalBudget`/`budgetLeft`/`minBudget`、`Evaluation.Cost`/`EvalCase.Cost` |

## 3. 语义细节

- **放行的唯一权威**：`Reserve` 是"这次调用行不行"的唯一判断，`Allow` 只作为 `Reserve` 内部实现存在。任何绕过 `Reserve` 的调用路径都是缺陷（回放除外，回放是在恢复已记账的过去）。
- **成本预算的度量**：只累加服务商报告的成本；未知成本不阻止运行继续，也不假装是 `0`。已知成本达到预算即停止，原因里写明 `… USD`；完全没报成本的运行不显示 USD 段。
- **估算的分寸**：估算只在服务商没有报用量时使用，且只用于"这一天/这一运行花了多少"的保守记账（上限因此照常生效）；账目本身保留 `tokensEstimated` 标记，管理界面能区分"报的"和"估的"。
- **应用范围是严格收窄**：应用读者在该应用自己的类型上得到与宿主读者完全相同的判定（角色映射、字段可见性、参与者/直属判定都不变），差别只是看不到别的应用；因此这次改动不引入任何新的可读面。
- **对话记录**：`transcriptDays()` 读不出数字时按 `0` 处理（宁可少留），`<=0` 时既不写新记录也清掉已有的；`TranscriptsFor(run="")` 是"某人自己的对话列表"，不套用按运行的 withheld 规则。
- **上限夹紧的位置**：`Chat` 不再自己夹，统一在 `Tenant.call` 的前置里做，这样所有调用方（chat、Agent、知识库、评测）看到的是同一套上限。
- **不做**：不做 USD 硬上限的账户级配额（ADR-0029 D1 已明确推迟）；不新增计费系统；不改权限模型；不做对话记录的按应用隔离（超出本稿范围，见 §5）。

## 4. 证据

本批（未提交时的工作树）新增 `capabilities/server/review_ai_test.go`，五个用例覆盖 D1–D6：

1. 流程里的 Agent 只读自己的应用：别的应用的上下文被拒、`Seen` 里没有任何内容，检索返回空集，自己的工单可读，随后 `done`。
2. 人代跑的运行以那个人身份读取：有该产线权限的 ana 读到内容，没有组织席位的 bo 被拒。
3. 在飞行中的调用期间取消：运行 `stopped`、结果是"stopped by ana"、0 步、15 token 已计量、忙表已清。
4. 上限：服务商被问到 `[8192 8192 512]`；把 `ai/max-tokens` 设为 `256` 后是 `256`。
5. `/v1/ai/chat`：界内体积通过，超 1 MiB 返回 400。

新增用例（同文件，均通过）：`TestUnreportedUsageIsEstimatedAndZeroIsZero`（没报用量 → 估算入账、按估算值触顶被拒；报 0 → 记 0、账目数字不变；聚合里 reported/estimated/costUnknown 分开）、`TestRunBudgetCountsUnreportedUsageAndCost`（只有估算时 token 预算照样收尾；报了成本超 USD 预算即收尾且原因含 USD；没报成本不当作 0 也不编造）、`TestCallsInFlightHoldTheirLimit`（分钟上限 1 时，在途调用尚未计量期间第二次调用被拒，计量后放行记录正确）、`TestTranscriptsAreKeptOnlyWhenAsked`（默认保留、0 不新写且清除已有、非管理员读不到、管理员读自己的对话列表不走按运行检查）。

另：`TestAgents` 的预算用例按 D9 更新为"第 4 步收尾（480 token）"；`TestLanguages`/中文文案补齐新设置与新字段说明；`cmd/api-types` 重新生成 `web/packages/kernel/src/gen/host.ts`；全量 Go 套件（含真实 PostgreSQL 路径）与真实 PostgreSQL 上的 `rehearse-lite.sh` 一并跑过。

## 5. 已知边界

- AI-01 的"允许的应用范围"没有进一步细分：现在等于"运行自己的应用"。应用读者看不到 `relations.Links`（知识库按 `Document.Apps` 把关）；这是已知的粗粒度，若将来要"跨应用只读"再开新稿。
- AI-03 的每日 tokens `0` = 不限保持现状（ADR-0029 D1 推迟 USD 硬上限）；D7 只保证"未知"不被当成"零"，并把未知按保守估算计入上限。
- 估算不是 tokenizer：它只服务上限与账目区分，不作为对外账单口径；真实成本仍以服务商上报为准。
- `/v1/ai/chat` 的上限是请求体 1 MiB 与服务商默认（或租户设置）的 `max-tokens`；流式回答的输出没有本地截断（截断属于服务商侧）。
- AI-06 的注入与记忆来源/撤回按评审建议只落文档约定，不改代码。
- 评审本身没有跑测试也没有浏览器走查；本稿的对照证据是 Go 测试与代码路径，端到端浏览器走查仍归 ADR-0047 的 M4。

## 6. 关系

- `docs/review/README.md`、`docs/review/ai-priorities.md`：本稿的来源与排序。
- ADR-0029（模型调用与配额）、ADR-0038（接受结果与恢复）、ADR-0047（入口与委派）、ADR-0048（联合草稿与直接安装退场）：本稿不改变它们的语义。
- `docs/Goal.md`：不可信输入（文档、目标、记忆）只作为数据对待，不升级为指令——AI-06 的文档侧约定。
