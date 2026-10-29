# 文件删除、合并与目录收敛对照清单

本附件属于本次设计（不是执行日志）。负责人已授权按表实施，部分原路径已迁移或删除；各change的tasks记录真实完成状态，不能把目标预算当作当前树。现有路径与声明按2026-10-27工作树核对（体积基线取修改守门前的HEAD `457202df`），目标是职责边界和删除前提，不是把文件直接拼接后宣称重构完成。

## 1. 同口径基线与目标约束

- 统计范围：`capabilities/server/**/*.go`，排除所有`*_test.go`，包含命令、纯基础包及所有平台应用；679个文件、5,124,183字节。条件编译文件仍计入，不因本机未构建就消失。
- 重点库存：root263、platform166（其中page78、非page88）、apps/build73、internal/construction42、internal/host15，共559。其余120个生产文件也逐一纳入下表以保持分母一致。
- 页契约78→3及其安全/兼容责任以[页面变更完整附表](../archive/2026-10-28-decouple-page-presentation-contract/design.md)为准；UAF的13个外移文件和19个组织owner文件见[自举设计](../platform-self-hosting/design.md)。本表只计其中6个生产Go外移，不计JSON、XMI、HTML和测试为手写Go删除收益。
- 目标上限为339个非测试Go文件（679口径下降至少50%）。当前逐文件映射经去重并包含2个新增阶段/训练职责后形成 **215个目标生产路径**、每包最多25个；这是一份容量规划，不是已实现结果。与AI/Page/UAF交付最终合并后必须重新计数；差异不能靠重命名、删除测试、提高配额或漏计文件掩盖。
- 目标路径数量没有证明净LOC减少。每批分别记录生产Go新增/删除行、纯移动、生成物和外移，且比较职责/原行为；只拼大文件或把Go逻辑搬为同量JS不能作为减脂完成。安全必需实现超过预算时先修订设计，不删格式/权限/历史或削弱断言凑数。

## 2. Build与construction的29组同名文件

这29组是两目录的精确交集；五个重点位置共有66组同名，全仓守门基线为82组/195路径。名字相同只提供定位线索。公共协议、作者草稿、不可变发布描述、运行状态和编译步骤不能按同名认定为一份冗余。

| 同名文件 | 已核实职责差异及处理 | 实际声明/入口示例 |
|---|---|---|
| `actions.go` | 状态/动作/Input/Set声明与标准动作编译分离，不是同一实现；同owner共享声明，保留到原Action的有类型编译。 | Build: `creator`、`checkProcess`、`checkActionCondition`；construction: `State`、`Action`、`Field`、`ValueConditionValue` |
| `activation.go` | 接受后的publication元数据与候选物化不同；合并归属，不删保存版本/后置安装责任。 | Build: `Draft`、`prepareReleasePublications`、`generatedReleasePage`；construction: `ReleasePublication` |
| `alert.go` | Alert声明及Matches与草稿检查/发布不同；保留匹配算法，移除跨包承载层。 | Build: `alertEntity`、`checkAlert`；construction: `Alert`、`Definition`、`Matches` |
| `application.go` | Application草稿/安装映射与Group值类型；可内聚，权限过滤投影仍不可省略。 | Build: `Application`、`applicationEntity`、`hand`；construction: `Group` |
| `build.go` | Object/Field原声明与Build注册/私有ledger不同；不是双账本，统一类型与owner而非删状态。 | Build: `Build`、`ReleaseProfile`、`AttachBuild`、`New`；construction: `Object`、`Field`、`TypeOf`、`ValueChoices` |
| `code.go` | construction仅CodeType常量；并回原Code owner，保留编译证据、受控制品及发布。 | Build: `Code`、`Definition`、`sourceHash`；construction: 常量/协议声明 |
| `connection.go` | 可变地址/凭据引用记录与受控连接决定；有意保留运行配置和不可变逻辑描述的安全差异。 | Build: `connectionEntity`、`connectionActions`；construction: `Connection`、`ConnectionCheck`、`Resolve` |
| `dataset.go` | 版本记录及推导类型与装载动作不同；合并类型归属，保留版本身份、原内容与schema。 | Build: `datasetEntity`、`datasetVersionEntity`；construction: `Dataset`、`DatasetField`、`ValueTypeOf`、`VersionID` |
| `evaluation.go` | 运行/尝试记录及输入与评估提交/比较不同；保留确切候选和配置绑定。 | Build: `evaluationEntity`、`evaluationActions`；construction: `Evaluation`、`EvaluationAttempt` |
| `formula.go` | 旧语法parser/lowering与schema检查/计算字段赋值不同；只用原rules求值，纯lowering归原纯模块。 | Build: `formulaExpr`、`checkFormulas`；construction: `ValueFormulaNode`、`ValueFormulaParser`、`ValueParseFormula`、`Skip` |
| `function.go` | Function草稿/保留版本与安装/校验不同；消除重复定义，保持版本冻结与权限。 | Build: `functionEntity`、`checkFunction`；construction: `Function`、`Definition` |
| `function_call.go` | FunctionRun事实与发起/结果动作不同；保留建议和采纳分离及来源。 | Build: `functionCallEntity`、`functionCallActions`；construction: `FunctionRun` |
| `integration_release.go` | 带本地状态的记录到portable descriptor的投影是安全边界，不是可删的全部转换；仅内聚重复封装。 | Build: `integrationType`、`integrationKindOf`；construction: `ConnectionDefinition`、`SourceDefinition`、`ValueIntegrationKind`、`ValueDecodeIntegration` |
| `joint_release.go` | JointDraftRef/DraftChange协议与闭包/候选算法不同；声明进入公共资产角色，原planner仍一份。 | Build: `draftInventories`、`recordKind`、`lookupEntity`；construction: `JointDraftRef`、`DraftChange` |
| `journal.go` | 记账声明/分录构造与动作绑定/编号不同；保留跨权威接受、冲销及历史事实。 | Build: `checkJournal`、`numberingOf`；construction: `JournalPost`、`JournalPostLine`、`Reversal`、`Entry` |
| `link_type.go` | construction仅类型/schema常量；合入LinkType owner，真正端点/版本保证保留。 | Build: `LinkType`、`Definition`、`linkTypeEntity`；construction: 常量/协议声明 |
| `match.go` | 纯key标准化/Resolver与受控声明发布不同；保留确定性匹配，只消除包装层。 | Build: `matchEntity`、`publishMatch`；construction: `Match`、`MatchKey`、`KeyOf`、`ValueNormalize` |
| `observation.go` | 便携观测声明与发布/原对象动作检查不同；不能删窗口、覆盖或未知语义。 | Build: `observationEntity`、`observationDefinition`；construction: `Observation` |
| `page.go` | construction仅PageType/SchemaRelease常量；草稿/原页面编译按页面change重构，安全约束保留。 | Build: `Page`、`InlineEdit`、`pageEntity`、`pageDraftEntity`；construction: 常量/协议声明 |
| `pipeline.go` | 声明与纯Execute/Apply混在construction，Build持决定/发布；纯变换归engine，草稿/编译归studio。 | Build: `pipelineEntity`、`pipelineActions`；construction: `Pipeline`、`Step`、`Due`、`Execute` |
| `posts.go` | 余额Post/值运算与原约束/来源检查不同；保留余额身份与一次接受，不能合成第二账本。 | Build: `BalanceID`、`checkPosts`；construction: `Post`、`ValueNumber` |
| `process.go` | 作者Process/ProcessStep与原Flow编译不同IR；单一编译保留，执行仍归原Flow owner。 | Build: `processEntity`、`publishProcess`；construction: `Process`、`ProcessLane` |
| `property_type.go` | construction仅PropertyTypeType常量；合入原属性owner，保留固定版本与兼容保证。 | Build: `PropertyType`、`Definition`、`propertyTypeEntity`；construction: 常量/协议声明 |
| `query.go` | construction仅QueryType常量；查询声明/编译/保留版本与运行窗口不能混成无类型JSON。 | Build: `Query`、`Definition`、`queryEntity`；construction: 常量/协议声明 |
| `release.go` | CompatibleObjectExpansion纯保证与release inventory/closure不同；共享原语义检查，保留所有pinned约束。 | Build: `ReleaseAssets`、`FlowReleaseAsset`；construction: `CompatibleObjectExpansion` |
| `source.go` | Source记录/Decode/MapRows/cursor与准备/执行动作不同；原游标及字节/事实投影保留。 | Build: `sourceEntity`、`sourceActions`；construction: `Source`、`SourceField`、`Due`、`Decode` |
| `table.go` | 表声明/条件lowering与原Operation发布/Compute不同；只有一个规则语义，编译/执行各守原责任。 | Build: `tableEntity`、`publishTable`；construction: `Table`、`TableColumn`、`ValueColumnSchema`、`Definition` |
| `testplan.go` | 样本/步骤/评估策略声明与只验证所供字段的草稿检查不同；不重写或削弱验收。 | Build: `testPlanEntity`、`checkTestPlanFields`；construction: `TestPlan`、`EvaluationPolicy` |
| `writeback.go` | Writeback逻辑映射/transport检查与发起/答复状态不同；保留冻结意图、补偿和原请求身份。 | Build: `writebackEntity`、`writebackActions`；construction: `Writeback`、`WritebackAns`、`Schema`、`Request` |

## 3. 四职责与公共API的目标文件预算

目录是职责边界；子包仍承担真实owner，不是新增转发层。基础`contract`、`journal`、`idp`和现有纯`internal/{rules,records}`按原职责保留；不能为凑四个目录强制把它们改成循环依赖。`internal/construction`和`internal/host`必须清空删除；旧apps/build不保留兼容转发壳。

| 目标package | 规划生产文件数 | 目标文件（完整清单） |
|---|---:|---|
| `capabilities/server/aigateway` | 6 | `gateway_calls.go`、`gateway_model_policy.go`、`gateway_stage_policy.go`、`gateway_tools.go`、`gateway_training.go`、`gateway_transcripts.go` |
| `capabilities/server/aigateway/providers` | 3 | `provider_anthropic.go`、`provider_openai.go`、`provider_transport.go` |
| `capabilities/server/apps/ai` | 2 | `accepted_state.go`、`ai.go` |
| `capabilities/server/apps/core` | 2 | `books.go`、`core.go` |
| `capabilities/server/apps/files` | 2 | `files.go`、`regions.go` |
| `capabilities/server/apps/knowledge` | 1 | `knowledge.go` |
| `capabilities/server/apps/relations` | 1 | `relations.go` |
| `capabilities/server/apps/system` | 2 | `definitions.go`、`system.go` |
| `capabilities/server/apps/work` | 5 | `policy.go`、`redirect.go`、`refusal.go`、`retirement.go`、`work.go` |
| `capabilities/server/cmd/api-types` | 1 | `main.go` |
| `capabilities/server/cmd/check-boundaries` | 1 | `main.go` |
| `capabilities/server/cmd/code-builder` | 1 | `main.go` |
| `capabilities/server/cmd/install-bundle` | 1 | `main.go` |
| `capabilities/server/cmd/platform-server` | 1 | `main.go` |
| `capabilities/server/cmd/platform-updater` | 2 | `main.go`、`serve.go` |
| `capabilities/server/cmd/rule-browser` | 1 | `main.go` |
| `capabilities/server/cmd/storage-files` | 1 | `main.go` |
| `capabilities/server/cmd/wasm-worker` | 1 | `main.go` |
| `capabilities/server/cmd/webhook-sink` | 1 | `main.go` |
| `capabilities/server/composition` | 3 | `http_routes.go`、`runtime_wiring.go`、`tenant.go` |
| `capabilities/server/engine/agents` | 3 | `agent_context.go`、`agent_functions.go`、`agent_runtime.go` |
| `capabilities/server/engine/applications` | 1 | `application_runtime_views.go` |
| `capabilities/server/engine/authorization` | 1 | `policy_decisions.go` |
| `capabilities/server/engine/automation` | 8 | `automation_alerts.go`、`automation_approvals.go`、`automation_continuous.go`、`automation_continuous_lifecycle.go`、`automation_engine.go`、`automation_host.go`、`automation_owner.go`、`automation_work.go` |
| `capabilities/server/engine/business` | 6 | `business_books.go`、`business_constraints.go`、`business_relations.go`、`business_rule_tables.go`、`business_transactions.go`、`refusal_engine.go` |
| `capabilities/server/engine/compute` | 3 | `compute_execution.go`、`compute_isolation.go`、`compute_lifecycle.go` |
| `capabilities/server/engine/integrations` | 6 | `integration_confirmations.go`、`integration_observations.go`、`integration_pipelines.go`、`integration_protocols.go`、`integration_sources.go`、`integration_transforms.go` |
| `capabilities/server/engine/knowledge` | 2 | `knowledge_index.go`、`knowledge_retrieval.go` |
| `capabilities/server/engine/organization` | 5 | `organization_model.go`、`organization_owner.go`、`organization_queries.go`、`organization_relations.go`、`organization_state.go` |
| `capabilities/server/engine/resources` | 1 | `resource_files.go` |
| `capabilities/server/idp` | 2 | `local.go`、`oidc.go` |
| `capabilities/server/internal/agents` | 2 | `json.go`、`prompt.go` |
| `capabilities/server/internal/aistate` | 1 | `state.go` |
| `capabilities/server/internal/records` | 13 | `access.go`、`fields.go`、`history.go`、`index.go`、`query.go`、`recent.go`、`references.go`、`row.go`、`rows.go`、`unique.go`、`validation.go`、`values.go`、`write.go` |
| `capabilities/server/internal/rules` | 9 | `aggregate.go`、`compare.go`、`compile.go`、`eval.go`、`formula_lowering.go`、`model.go`、`page.go`、`value.go`、`window.go` |
| `capabilities/server/internal/sink` | 1 | `sink.go` |
| `capabilities/server/internal/systembuild` | 2 | `source.go`、`systembuild.go` |
| `capabilities/server/internal/systemscope` | 1 | `systemscope.go` |
| `capabilities/server/journal` | 17 | `file.go`、`journal.go`、`memory.go`、`postgres.go`、`postgres_checkpoint.go`、`postgres_read_queries.go`、`postgres_read_view.go`、`postgres_snapshot_state.go`、`postgres_state.go`、`postgres_state_upgrade.go`、`postgres_unique.go`、`postgres_value_selection.go`、`postgres_vector.go`、`read_position.go`、`state.go`、`state_replacement.go`、`vector.go` |
| `capabilities/server/kernel` | 10 | `acceptance_codec.go`、`acceptance_pipeline.go`、`acceptance_records.go`、`doc.go`、`tenant_coordinator.go`、`tenant_diagnostics.go`、`tenant_projection.go`、`tenant_reads.go`、`tenant_recovery.go`、`tenant_submission.go` |
| `capabilities/server/kernel/acceptedformat` | 10 | `format_batch.go`、`format_effect.go`、`format_effect_transition.go`、`format_envelope.go`、`format_evidence.go`、`format_input.go`、`format_record.go`、`format_refusal.go`、`format_release.go`、`format_usage.go` |
| `capabilities/server/kernel/control` | 5 | `control_credentials.go`、`control_host.go`、`control_identity.go`、`control_settings.go`、`control_tenants.go` |
| `capabilities/server/kernel/records` | 4 | `record_access.go`、`record_collections.go`、`record_enterprise_fields.go`、`record_history_index.go` |
| `capabilities/server/kernel/transport` | 2 | `transport_declarations.go`、`transport_http.go` |
| `capabilities/server/platform` | 25 | `actions.go`、`ai.go`、`app.go`、`asset_lifecycle.go`、`composition.go`、`credential.go`、`definition.go`、`dependencies.go`、`entity.go`、`files.go`、`flow.go`、`ledger.go`、`model_configuration.go`、`operations.go`、`org.go`、`page_model.go`、`page_queries.go`、`page_rules.go`、`protocol.go`、`record_image.go`、`revision.go`、`rules.go`、`snapshot.go`、`transaction.go`、`work.go` |
| `capabilities/server/platform/authz` | 1 | `authz.go` |
| `capabilities/server/studio/authoring` | 2 | `authoring_ai_evaluation.go`、`authoring_compiler.go` |
| `capabilities/server/studio/build` | 23 | `build_actions.go`、`build_agents.go`、`build_applications.go`、`build_artifacts.go`、`build_blueprints.go`、`build_compute.go`、`build_constraints.go`、`build_evaluations.go`、`build_formulas.go`、`build_functions.go`、`build_integrations.go`、`build_links.go`、`build_objects.go`、`build_owner.go`、`build_pages.go`、`build_processes.go`、`build_properties.go`、`build_publications.go`、`build_queries.go`、`build_release.go`、`build_tests.go`、`build_transactions.go`、`build_versions.go` |
| `capabilities/server/studio/packages` | 1 | `package_owner.go` |
| `capabilities/server/studio/pages` | 3 | `page_asset_navigation.go`、`page_bound_queries.go`、`page_safe_bindings.go` |
| `capabilities/server/studio/releases` | 6 | `release_candidates.go`、`release_installation.go`、`release_registry.go`、`release_retention.go`、`release_review_checks.go`、`release_system_history.go` |
| `capabilities/server/studio/trials` | 1 | `trial_runtime.go` |

新增的 `aigateway/gateway_stage_policy.go` 与 `aigateway/gateway_training.go` 只承接AI变更中当前缺失的阶段/样本职责；provider_transport从原aicall拆出。Agent、函数、知识检索仍归engine，候选评估归studio；原通用breakers及工作尝试quota不挪作网关专属账本。以下679行旧路径与上述新增目标共同构成215个路径预算。

## 4. root 263个文件

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/a2a.go` | `capabilities/server/aigateway/gateway_tools.go` | 协议和工具适配，复用原授权动作与typed结果；`a2aPart`、`a2aMessage`、`text`、`published` |
| `capabilities/server/accepted_agent.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`acceptedAgent`、`digestAgent`、`agentHash` |
| `capabilities/server/accepted_approval.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`RequestApproval`、`requestApproval` |
| `capabilities/server/accepted_batch.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`preparedAcceptedBatch`、`batchResult`、`applyAcceptedBatch` |
| `capabilities/server/accepted_callback_rows.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`callbackWrite`、`savedCallbackRows`、`prepareCallbackRows` |
| `capabilities/server/accepted_connectors.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`markFor`、`validateAcceptedDeliveries` |
| `capabilities/server/accepted_control.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`controlWork`、`controlDecision`、`captureControl`、`decodeControl` |
| `capabilities/server/accepted_effect.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`applyAcceptedEffect`、`settleAccepted` |
| `capabilities/server/accepted_host_control.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`hostControlImage`、`hostControlReceipt`、`image`、`validateHostImage` |
| `capabilities/server/accepted_input.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`inputResult`、`encodeAcceptedInputRefusal` |
| `capabilities/server/accepted_intents.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`validateAcceptedIntents`、`installAcceptedIntents` |
| `capabilities/server/accepted_notices.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`recipients`、`Notify` |
| `capabilities/server/accepted_observation.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`stageObservers`、`validateAcceptedObservations` |
| `capabilities/server/accepted_plan.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`preparedAcceptedChanges`、`savedChangePlan`、`ChangePlan` |
| `capabilities/server/accepted_refusal.go` | `capabilities/server/kernel/acceptance_codec.go` | 结果封存/身份摘要/持久回执；复用现有版本化codec；`resultRequestHash`、`acceptedIdentity` |
| `capabilities/server/accepted_release.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`applyAcceptedRelease`、`SaveReleaseCandidate` |
| `capabilities/server/accepted_requests.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`answerRequests`、`answerRequest` |
| `capabilities/server/accepted_result.go` | `capabilities/server/kernel/acceptance_codec.go` | 结果封存/身份摘要/持久回执；复用现有版本化codec；`sealResult`、`result` |
| `capabilities/server/accepted_rows.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`captureDirectRows`、`prepareAcceptedDirectRows` |
| `capabilities/server/accepted_state.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`stateApp`、`attachPrivateHosts` |
| `capabilities/server/accepted_store.go` | `capabilities/server/kernel/acceptance_codec.go` | 结果封存/身份摘要/持久回执；复用现有版本化codec；`acceptResult`、`recordsAcceptedResults` |
| `capabilities/server/accepted_unique.go` | `capabilities/server/kernel/acceptance_records.go` | 私有行/状态/控制/通知/唯一键及联合提升，保留SQL与发布映像；`uniqueReplacements`、`savedUniqueChanges` |
| `capabilities/server/accepted_usage.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`usagePredecessor`、`acceptUsage` |
| `capabilities/server/accepted_work.go` | `capabilities/server/kernel/acceptance_pipeline.go` | 原结果族的调度/前驱/接受与回调，保留专属语义；`acceptedWork`、`stateOfTask`、`workInputHash` |
| `capabilities/server/access.go` | `capabilities/server/engine/authorization/policy_decisions.go` | 原固定合成顺序/显式deny/类型投影，不造另一个授权器；`CustomRole`、`PolicyRecord`、`accessActions`、`decideAccess` |
| `capabilities/server/agent.go` | `capabilities/server/engine/agents/agent_runtime.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`AgentRunRecord`、`Draft`、`AttachAgent`、`NewAgents` |
| `capabilities/server/agent_candidate.go` | `capabilities/server/studio/authoring/authoring_ai_evaluation.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`agentEvaluationApp`、`Manifest`、`AgentEvaluationApp` |
| `capabilities/server/agent_control.go` | `capabilities/server/engine/agents/agent_runtime.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`Switch`、`AgentOverview`、`switchActions`、`switchDecision` |
| `capabilities/server/agent_engine.go` | `capabilities/server/engine/agents/agent_runtime.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`stepBody`、`AgentTurn`、`Think`、`turns` |
| `capabilities/server/agent_eval.go` | `capabilities/server/studio/authoring/authoring_ai_evaluation.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`Evaluation`、`EvaluationInput`、`decision`、`normal` |
| `capabilities/server/agent_host.go` | `capabilities/server/engine/agents/agent_context.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`ProtocolActions`、`ProtocolProvider` |
| `capabilities/server/agent_memory.go` | `capabilities/server/engine/agents/agent_context.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`Memory`、`memoryEntity`、`mayRemember` |
| `capabilities/server/agent_roles.go` | `capabilities/server/engine/agents/agent_runtime.go` | 原Agent运行/候选/取消/来源，调用stage见AI变更；`AgentRuntime`、`AgentCardDeclaration` |
| `capabilities/server/aggregate.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`AggregateQuery`、`Aggregate`、`aggregateGroupValue`、`aggregate` |
| `capabilities/server/aggregate_histogram.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`HistogramQuery`、`HistogramBucket`、`histogramNumber`、`histogramBound` |
| `capabilities/server/aggregate_window.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`AggregateWindowQuery`、`AggregateWindowEdge`、`windowThreshold`、`windowTime` |
| `capabilities/server/aicall.go` | `capabilities/server/aigateway/gateway_calls.go`；`capabilities/server/aigateway/providers/provider_openai.go`；`capabilities/server/aigateway/providers/provider_transport.go` | 原模型准备/执行/收据和有效配置，provider分离见AI变更；`Message`、`Tool`、`unavailable`、`limits` |
| `capabilities/server/alerts.go` | `capabilities/server/engine/automation/automation_alerts.go` | 原告警匹配与事件，不改规则所有者；`raiseAlerts` |
| `capabilities/server/anthropic.go` | `capabilities/server/aigateway/providers/provider_anthropic.go` | 真实供应商实现，非重复业务包装；`anthropicClient`、`completeAnthropic` |
| `capabilities/server/api.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`Route`、`Param`、`OpenAPI`、`operationID` |
| `capabilities/server/application_ai.go` | `capabilities/server/aigateway/gateway_calls.go` | 原模型准备/执行/收据和有效配置，provider分离见AI变更；`ApplicationAI`、`ApplicationAIEvaluation`、`applicationAILocked`、`applicationAIUsage` |
| `capabilities/server/application_binding.go` | `capabilities/server/engine/applications/application_runtime_views.go` | 原已发布应用、运行归因与成员投影，无第二运行账本；`bindApplications`、`compareApplicationRef` |
| `capabilities/server/application_runs.go` | `capabilities/server/engine/applications/application_runtime_views.go` | 原已发布应用、运行归因与成员投影，无第二运行账本；`ApplicationRun`、`ApplicationRunPage`、`ApplicationRuns`、`applicationRunGraph` |
| `capabilities/server/application_runtime.go` | `capabilities/server/engine/applications/application_runtime_views.go` | 原已发布应用、运行归因与成员投影，无第二运行账本；`ApplicationVersion`、`ApplicationData`、`applicationMaintenanceLocked`、`ApplicationRuntimeOf` |
| `capabilities/server/applications.go` | `capabilities/server/engine/applications/application_runtime_views.go` | 原已发布应用、运行归因与成员投影，无第二运行账本；`ApplicationPage`、`ApplicationView`、`Applications`、`pageKey` |
| `capabilities/server/approval_execution.go` | `capabilities/server/engine/automation/automation_approvals.go` | 原审批确切闭包及执行主体，禁止用当前定义替换旧版本；`ApprovalRelease`、`ApprovalRelease` |
| `capabilities/server/asset_registry.go` | `capabilities/server/studio/releases/release_retention.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`runtimeAssetOwner`、`ValidateCodeAsset`、`AssetKinds` |
| `capabilities/server/audit_log.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`auditLog`、`keepLast`、`remember` |
| `capabilities/server/authorization.go` | `capabilities/server/engine/authorization/policy_decisions.go` | 原固定合成顺序/显式deny/类型投影，不造另一个授权器；`ProjectMember`、`ProjectAsset`、`ProjectActions`、`decideProject` |
| `capabilities/server/authorization_decision.go` | `capabilities/server/engine/authorization/policy_decisions.go` | 原固定合成顺序/显式deny/类型投影，不造另一个授权器；`authorizationDetails`、`authorizationWalk`、`savedAuthorizations`、`authorizationPass` |
| `capabilities/server/books.go` | `capabilities/server/engine/business/business_books.go` | 原账簿/编号与迁移，接受前不分配正式状态；`legacyJournalEffects`、`sendBooks` |
| `capabilities/server/breakers.go` | `capabilities/server/engine/integrations/integration_protocols.go` | 原通用目的地熔断、半开与锁外效果协调，不归网关另造；`breaker`、`breakers`、`allow`、`report` |
| `capabilities/server/build_authoring.go` | `capabilities/server/studio/authoring/authoring_compiler.go` | 设计态建议和受控编译，模型经aigateway接口；`DraftActionCapabilities` |
| `capabilities/server/business_constraints.go` | `capabilities/server/engine/business/business_constraints.go` | 原约束、业务版本、类型值和事务见证；`constraintWrite`、`constraintReadPlan`、`readableFieldIssues`、`localizedFieldIssues` |
| `capabilities/server/business_refusal_escalation.go` | `capabilities/server/engine/business/refusal_engine.go` | 保留结构化拒绝事实、来源与升级策略，不靠英文文案重判；`PendingRefusalEscalations` |
| `capabilities/server/business_refusal_facts.go` | `capabilities/server/engine/business/refusal_engine.go` | 保留结构化拒绝事实、来源与升级策略，不靠英文文案重判；`BusinessRefused`、`CaptureBusinessRefusal` |
| `capabilities/server/business_refusals.go` | `capabilities/server/engine/business/refusal_engine.go` | 保留结构化拒绝事实、来源与升级策略，不靠英文文案重判；`BusinessInputChange`、`BusinessRefusalEntry`、`refusalCategory`、`correctionOf` |
| `capabilities/server/business_version.go` | `capabilities/server/engine/business/business_constraints.go` | 原约束、业务版本、类型值和事务见证；`businessField`、`businessQuery` |
| `capabilities/server/capabilities.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`Capabilities`、`capabilities` |
| `capabilities/server/capability_ai_result.go` | `capabilities/server/aigateway/gateway_calls.go` | 原模型准备/执行/收据和有效配置，provider分离见AI变更；`ReadCapabilityAI`、`readCapabilityAI` |
| `capabilities/server/capability_bind.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`bindCapabilityInputs` |
| `capabilities/server/capability_closure.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`capabilityClosure`、`sameRetainedCapability` |
| `capabilities/server/capability_describe.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`DescribeCapability`、`describeCapability` |
| `capabilities/server/capability_invoke.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`capabilityPreparation`、`InvokeCapability`、`prepareCapability` |
| `capabilities/server/chain.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`ChainNode`、`ChainEdge`、`chainRead`、`chainRun` |
| `capabilities/server/changes.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`changes`、`changed`、`operationsChanged` |
| `capabilities/server/code_compiler.go` | `capabilities/server/studio/authoring/authoring_compiler.go` | 设计态建议和受控编译，模型经aigateway接口；`CodeCompiler`、`ContainerCompiler`、`EnvironmentCompiler`、`Compile` |
| `capabilities/server/committed.go` | `capabilities/server/kernel/tenant_submission.go` | 私有提交、定义依据、幂等与未知结果；`acceptedValueReader`、`committed`、`saveAnswer`、`saveInput` |
| `capabilities/server/composite.go` | `capabilities/server/kernel/tenant_projection.go` | 原受控投影与复合请求预算，不新增通用业务决定者；`CompositeEdit`、`CompositeAnswer`、`compositeRequestHash`、`encodeAcceptedComposite` |
| `capabilities/server/compute.go` | `capabilities/server/engine/compute/compute_execution.go` | 原受控制品、ABI、预算、取消与进程隔离；`operationBinding`、`pendingOperationInputs`、`RequestOperation` |
| `capabilities/server/compute_call.go` | `capabilities/server/engine/compute/compute_execution.go` | 原受控制品、ABI、预算、取消与进程隔离；`operationActions`、`decideOperation` |
| `capabilities/server/compute_cancel.go` | `capabilities/server/engine/compute/compute_lifecycle.go` | 原受控制品、ABI、预算、取消与进程隔离；`CancelOperation`、`operationCancellation` |
| `capabilities/server/compute_cancel_registry.go` | `capabilities/server/engine/compute/compute_lifecycle.go` | 原受控制品、ABI、预算、取消与进程隔离；`cancels`、`add`、`remove` |
| `capabilities/server/compute_channel.go` | `capabilities/server/engine/compute/compute_execution.go` | 原受控制品、ABI、预算、取消与进程隔离；`operationInputArtifact`、`preparedOperationInput`、`inputKey`、`sealInput` |
| `capabilities/server/compute_install.go` | `capabilities/server/engine/compute/compute_lifecycle.go` | 原受控制品、ABI、预算、取消与进程隔离；`ValidateInstallOperation`、`validateOperation` |
| `capabilities/server/compute_sdk.go` | `capabilities/server/engine/compute/compute_isolation.go` | 原受控制品、ABI、预算、取消与进程隔离；`ComputeSDKRequest`、`ComputeSDK`、`readCapabilityBody` |
| `capabilities/server/compute_socket.go` | `capabilities/server/engine/compute/compute_isolation.go` | 原受控制品、ABI、预算、取消与进程隔离；`listenComputeSocket`、`CheckComputeSocket` |
| `capabilities/server/compute_work.go` | `capabilities/server/engine/compute/compute_execution.go` | 原受控制品、ABI、预算、取消与进程隔离；`acceptedOperationClaim`、`operationClaimDigest`、`decodeOperationClaim` |
| `capabilities/server/confirmation_reconcile.go` | `capabilities/server/engine/integrations/integration_confirmations.go` | 原dispatch/确认/回读与补偿，未知设备动作不自动重发；`ReconcileConfirmations` |
| `capabilities/server/confirmation_transport.go` | `capabilities/server/engine/integrations/integration_confirmations.go` | 原dispatch/确认/回读与补偿，未知设备动作不自动重发；`confirmationCompletion`、`ConfirmationTransport`、`FreezeConfirmation` |
| `capabilities/server/conflict_refusal.go` | `capabilities/server/engine/business/refusal_engine.go` | 保留结构化拒绝事实、来源与升级策略，不靠英文文案重判；`conflictRefusal` |
| `capabilities/server/conformance.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`CheckReplay`、`snapshot` |
| `capabilities/server/connections.go` | `capabilities/server/engine/integrations/integration_sources.go` | 原来源/连接/游标及失败责任，保留凭据引用；`ConnectionKind`、`ConnectionKindPage`、`connectionKinds`、`routesConnectionKinds` |
| `capabilities/server/connector_roster.go` | `capabilities/server/engine/integrations/integration_sources.go` | 原来源/连接/游标及失败责任，保留凭据引用；`connectorRoster`、`newConnectorRoster`、`register` |
| `capabilities/server/console.go` | `capabilities/server/kernel/control/control_host.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`Seat`、`Console`、`Memberships`、`ConsoleActions` |
| `capabilities/server/console_host.go` | `capabilities/server/kernel/control/control_host.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`Apps`、`App` |
| `capabilities/server/console_identity.go` | `capabilities/server/kernel/control/control_identity.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`MemberByID`、`AgentMembers` |
| `capabilities/server/console_member.go` | `capabilities/server/kernel/control/control_identity.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`currentMember`、`migrateGrants` |
| `capabilities/server/context.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`ContextView`、`FlowSummary`、`host`、`appReader` |
| `capabilities/server/continuous_frames.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`flowFrameStore`、`key`、`Seal` |
| `capabilities/server/credentials.go` | `capabilities/server/kernel/control/control_credentials.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`CredentialBinding`、`CredentialDefinition`、`credentialActions`、`credentialCipher` |
| `capabilities/server/decision_definition.go` | `capabilities/server/kernel/tenant_submission.go` | 私有提交、定义依据、幂等与未知结果；`DecisionDefinition`、`decisionDefinition` |
| `capabilities/server/definition_catalog.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`ownedMetadataKind`、`ownedAssetDefinition` |
| `capabilities/server/definition_page_projection.go` | `capabilities/server/studio/pages/page_safe_bindings.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`pageDefinitionProjection`、`pageDefinitionSources`、`function`、`operation` |
| `capabilities/server/definition_visibility.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`definitionsFrom`、`applicationEntryWithdrawn` |
| `capabilities/server/definitions.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`registerDefinitions`、`uniqueRefs` |
| `capabilities/server/dependencies.go` | `capabilities/server/studio/releases/release_candidates.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`DependencyView`、`ProvidedCapability`、`definitionAsset`、`describeOwnedAsset` |
| `capabilities/server/deploy.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`Deployment`、`snapshotter`、`Flags`、`idpKeyPath` |
| `capabilities/server/deploy_composition.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`providers`、`composeRuntime` |
| `capabilities/server/deploy_identity.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`identityUpstream`、`identityProxyHandler` |
| `capabilities/server/deploy_recovery.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`attachJournal`、`retryTenant` |
| `capabilities/server/dispatch_effects.go` | `capabilities/server/engine/integrations/integration_confirmations.go` | 原dispatch/确认/回读与补偿，未知设备动作不自动重发；`beginDispatchAttempt` |
| `capabilities/server/dispatch_readback.go` | `capabilities/server/engine/integrations/integration_confirmations.go` | 原dispatch/确认/回读与补偿，未知设备动作不自动重发；`ReadDispatches`、`readDispatchStatus` |
| `capabilities/server/dispatch_transport.go` | `capabilities/server/engine/integrations/integration_confirmations.go` | 原dispatch/确认/回读与补偿，未知设备动作不自动重发；`DispatchTransport`、`FreezeDispatch` |
| `capabilities/server/doc.go` | `capabilities/server/kernel/doc.go` | 单一职责/依赖/锁义务文档，名称保持文档惯例例外或合并现有说明；常量/协议声明 |
| `capabilities/server/effects.go` | `capabilities/server/engine/integrations/integration_protocols.go` | 冻结外部意图、版本协议与交付分类；`Endpoint`、`effect`、`settled`、`emit` |
| `capabilities/server/enterprise_links.go` | `capabilities/server/engine/organization/organization_queries.go` | 原组织引用/来源/历史使用检查；UAF按独立清单退出；`EnterpriseLinkChange`、`EnterpriseLinkPreview`、`tenantLinkModel`、`tenantConnection` |
| `capabilities/server/enterprise_usage.go` | `capabilities/server/engine/organization/organization_queries.go` | 原组织引用/来源/历史使用检查；UAF按独立清单退出；`EnterpriseUsageItem`、`EnterpriseUsage`、`enterpriseRecordCurrent`、`enterpriseExternalUsage` |
| `capabilities/server/enterprise_usage_history.go` | `capabilities/server/engine/organization/organization_queries.go` | 原组织引用/来源/历史使用检查；UAF按独立清单退出；`enterpriseReferenceDeclaration`、`enterpriseHistoricalReferencePaths`、`historyNamesEnterprisePaths` |
| `capabilities/server/environment.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`SealedArtifact`、`PromotionResult`、`SealCandidate`、`sealedKey` |
| `capabilities/server/events.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`caused`、`publish`、`planAcceptedEvent` |
| `capabilities/server/facade.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`AppEntry`、`AppInfo`、`Catalog`、`Declarations` |
| `capabilities/server/file_handover.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`FileStoreManifest` |
| `capabilities/server/files.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`Upload`、`SweepUploads`、`Download` |
| `capabilities/server/files_local.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`localFiles`、`NewLocalFiles`、`path` |
| `capabilities/server/filestore.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`FileStore`、`memoryFiles`、`Put`、`Get` |
| `capabilities/server/filestore_inventory.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`FileInventory`、`StoredFile`、`checkFileInventory`、`Len` |
| `capabilities/server/flow_artifact_gc.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`flowArtifactGC`、`beginFlowArtifactPreparation`、`flowArtifactRecoveryComplete` |
| `capabilities/server/flow_binding.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`BindFlow`、`retainedFlowClosure` |
| `capabilities/server/flow_execution.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`flowExecution`、`flowExecutionRuntime`、`FlowExecution`、`retainedExecution` |
| `capabilities/server/flow_reads.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`readQueryFrom`、`readQueryVersionFrom` |
| `capabilities/server/function.go` | `capabilities/server/engine/agents/agent_functions.go` | 原模型准备/执行/收据和有效配置，provider分离见AI变更；`functionBinding`、`RequestFunction`、`planFunction` |
| `capabilities/server/function_conversation.go` | `capabilities/server/engine/agents/agent_functions.go` | 原模型准备/执行/收据和有效配置，provider分离见AI变更；`functionConversation` |
| `capabilities/server/health.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`TenantHealth`、`QueueHealth`、`Health`、`health` |
| `capabilities/server/host.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`Delivery`、`Tenant`、`Audit`、`NewTenant` |
| `capabilities/server/host_composition.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`CompositionRequest`、`HostComposition`、`desiredRuntime`、`compositionView` |
| `capabilities/server/host_control.go` | `capabilities/server/kernel/control/control_host.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`hostControl`、`state`、`setState` |
| `capabilities/server/hostadmin.go` | `capabilities/server/kernel/control/control_host.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`HostTenantView`、`HostOverview`、`hostAuthorization`、`hostRequestAuthorization` |
| `capabilities/server/hostview.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`hostView`、`ProtocolContract`、`ReadProtocolRecords` |
| `capabilities/server/http_edge.go` | `capabilities/server/kernel/transport/transport_http.go` | 原HTTP/认证边缘/资源/诊断，不放业务规则；`edgeWriter`、`edgeStats`、`compressible`、`WriteHeader` |
| `capabilities/server/identity_roles.go` | `capabilities/server/kernel/control/control_identity.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`IdentitySessions`、`IdentityProfile` |
| `capabilities/server/installed.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`Install`、`decodeRowImage` |
| `capabilities/server/interface_queries.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`InterfaceRecord`、`InterfaceRecordPage`、`bindInterfaceQuery`、`checkInterfaceQueryFields` |
| `capabilities/server/journal_bridge.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`resultIdentity`、`OpenJournal`、`OpenFileJournal` |
| `capabilities/server/knowledge.go` | `capabilities/server/engine/knowledge/knowledge_retrieval.go` | 原知识来源/向量prepare-rebuild-activate及当前权限；`Passage`、`chunk`、`keep`、`rebuild` |
| `capabilities/server/knowledge_configuration.go` | `capabilities/server/engine/knowledge/knowledge_index.go` | 原知识来源/向量prepare-rebuild-activate及当前权限；`KnowledgeIndexStatus`、`embeddingNeedsRebuild`、`vectorSpace` |
| `capabilities/server/knowledge_vectors.go` | `capabilities/server/engine/knowledge/knowledge_index.go` | 原知识来源/向量prepare-rebuild-activate及当前权限；`syncVectorSources`、`nearestKnowledge` |
| `capabilities/server/languages.go` | `capabilities/server/kernel/transport/transport_declarations.go` | 原声明语言与权限/定义世代缓存，业务值不翻译；`Language`、`asked` |
| `capabilities/server/lifecycle.go` | `capabilities/server/kernel/control/control_tenants.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`Token`、`TokenView`、`end`、`lifecycleActions` |
| `capabilities/server/link_types.go` | `capabilities/server/engine/business/business_relations.go` | 原关系端点、版本、迁移与保留引用；`ValidateInstallLinkType`、`InstallLinkType` |
| `capabilities/server/live_queries.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`LiveQueryResult`、`LiveQueryFrame`、`liveReadAllowed`、`liveOperationRead` |
| `capabilities/server/mail.go` | `capabilities/server/engine/automation/automation_work.go` | 原任务/工作/通知与投递，监控不增加控制权；`letter`、`mailNotice`、`planMailNotice` |
| `capabilities/server/mcp.go` | `capabilities/server/aigateway/gateway_tools.go` | 协议和工具适配，复用原授权动作与typed结果；`supportedMCPVersion`、`mcp` |
| `capabilities/server/model_catalog.go` | `capabilities/server/aigateway/gateway_model_policy.go` | 原模型配置/预算/断路器；系统配置保持静态；`CatalogModel`、`cachedCatalog`、`ProviderModels`、`catalogModel` |
| `capabilities/server/model_configuration.go` | `capabilities/server/aigateway/gateway_model_policy.go` | 原模型配置/预算/断路器；系统配置保持静态；`ModelChoice`、`ModelSelection`、`modelPurposeSetting`、`resolveModel` |
| `capabilities/server/module_lifecycle.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`ownerActive`、`liveApps` |
| `capabilities/server/narrow.go` | `capabilities/server/engine/authorization/policy_decisions.go` | 原固定合成顺序/显式deny/类型投影，不造另一个授权器；`narrower`、`admits`、`mayRead` |
| `capabilities/server/native_flow.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`nativeFlowAsset`、`flowReleaseAsset` |
| `capabilities/server/notices.go` | `capabilities/server/engine/automation/automation_work.go` | 原任务/工作/通知与投递，监控不增加控制权；`noticeBoard`、`state`、`restore` |
| `capabilities/server/observation_context.go` | `capabilities/server/engine/integrations/integration_observations.go` | 原观测窗口/覆盖/未知与调度，不搬行业规则；`ObservationContext` |
| `capabilities/server/observation_decision.go` | `capabilities/server/engine/integrations/integration_observations.go` | 原观测窗口/覆盖/未知与调度，不搬行业规则；`ObservationDecisionEvidence`、`visibleObservationEvidence`、`observationRequestIdentity` |
| `capabilities/server/observation_schedule.go` | `capabilities/server/engine/integrations/integration_observations.go` | 原观测窗口/覆盖/未知与调度，不搬行业规则；`observationWindowAttempt`、`EvaluateObservations` |
| `capabilities/server/observations.go` | `capabilities/server/engine/integrations/integration_observations.go` | 原观测窗口/覆盖/未知与调度，不搬行业规则；`ObservationInfo`、`ObservationRead`、`Observations`、`observationUnavailable` |
| `capabilities/server/operations.go` | `capabilities/server/engine/automation/automation_work.go` | 原任务/工作/通知与投递，监控不增加控制权；`Task`、`workBody`、`retryOf`、`due` |
| `capabilities/server/owned_relations.go` | `capabilities/server/engine/business/business_relations.go` | 原关系端点、版本、迁移与保留引用；`ownedRelationViolation`、`ownedRelationID`、`PutOwnedRelation` |
| `capabilities/server/package_composition.go` | `capabilities/server/composition/runtime_wiring.go` | 部署/能力选择、维护与Attach只在composition；生命周期执行委托原owner；`packageContract`、`packageModule` |
| `capabilities/server/package_decode.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`decodePackageContent` |
| `capabilities/server/package_drafts.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`PackageDraftRequest`、`PackageDraft`、`routesPackageDrafts`、`packageBlueprint` |
| `capabilities/server/package_preparation.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`PackagePreparation`、`PackageApplication`、`PreparationBlueprint`、`routesPackagePreparation` |
| `capabilities/server/package_removal.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`PackageRemoval`、`packageDependents`、`packageRemoval` |
| `capabilities/server/package_sources.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`PackageSource`、`PackageInspectRequest`、`packageOfferKey`、`routesPackageSources` |
| `capabilities/server/packages.go` | `capabilities/server/studio/packages/package_owner.go` | 原包审阅/身份/保留/安装/草稿，真实制品IO与准备状态不丢；`PackageContribution`、`PackageDescriptor`、`check`、`PackageActions` |
| `capabilities/server/page_bindings.go` | `capabilities/server/studio/pages/page_safe_bindings.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`checkFormInputs` |
| `capabilities/server/page_composite.go` | `capabilities/server/studio/pages/page_safe_bindings.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`PageCompositeSource`、`PageMatrixCell`、`readPageComposite`、`checkCompositeGraph` |
| `capabilities/server/page_compute.go` | `capabilities/server/studio/pages/page_bound_queries.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`pageOperation`、`checkPageOperation` |
| `capabilities/server/page_content.go` | `capabilities/server/studio/pages/page_asset_navigation.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`PageContentDefinition` |
| `capabilities/server/page_exploration.go` | `capabilities/server/studio/pages/page_bound_queries.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`explorationAssetAvailable`、`filterExplorationAssets` |
| `capabilities/server/page_model.go` | `capabilities/server/studio/pages/page_safe_bindings.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`PageModelRecord`、`PageModelRule`、`checkModelQueries`、`modelCriteria` |
| `capabilities/server/page_navigation.go` | `capabilities/server/studio/pages/page_asset_navigation.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`filterPageNavigation`、`checkPageNavigation` |
| `capabilities/server/page_queries.go` | `capabilities/server/studio/pages/page_bound_queries.go` | 保留原查询/动作/字段投影；视觉配置迁Web，见page change；`querySourceDefinition`、`visiblePageInterfaceQuery` |
| `capabilities/server/permissions.go` | `capabilities/server/engine/authorization/policy_decisions.go` | 原固定合成顺序/显式deny/类型投影，不造另一个授权器；`RolePermissions`、`PermissionAction`、`Permissions`、`ExplainOperation` |
| `capabilities/server/pipeline_preview.go` | `capabilities/server/engine/integrations/integration_pipelines.go` | 原管道与回写编排，冻结原输入/结果；`PipelinePreviewRequest`、`PipelinePreview`、`PreviewPipeline` |
| `capabilities/server/pipelines.go` | `capabilities/server/engine/integrations/integration_pipelines.go` | 原管道与回写编排，冻结原输入/结果；`RunPipelines`、`pipelineResult` |
| `capabilities/server/pprof.go` | `capabilities/server/kernel/transport/transport_http.go` | 原HTTP/认证边缘/资源/诊断，不放业务规则；`profileRoutes` |
| `capabilities/server/process_diagnostics.go` | `capabilities/server/engine/automation/automation_host.go` | 原Flow调用/绑定/清理/连续流，不改执行器；`ProcessDiagnostic`、`ProcessDiagnostics` |
| `capabilities/server/profile.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`Profile`、`Account`、`profileActions`、`tenantSettings` |
| `capabilities/server/projection.go` | `capabilities/server/kernel/tenant_projection.go` | 原受控投影与复合请求预算，不新增通用业务决定者；`Projection`、`column`、`ident`、`ProjectionSchema` |
| `capabilities/server/property_types.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`ResolvePropertyType`、`ValidateInstallPropertyType` |
| `capabilities/server/property_values.go` | `capabilities/server/engine/business/business_constraints.go` | 原约束、业务版本、类型值和事务见证；`propertyValues` |
| `capabilities/server/protocol.go` | `capabilities/server/engine/integrations/integration_protocols.go` | 冻结外部意图、版本协议与交付分类；`binding`、`request`、`bind`、`bound` |
| `capabilities/server/quarantine.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`tenantFault`、`quarantine`、`quarantined` |
| `capabilities/server/queries.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`namedQuery`、`queryRead` |
| `capabilities/server/query_capability.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`queryInputSchema`、`queryCapabilitySchemas` |
| `capabilities/server/query_install.go` | `capabilities/server/studio/releases/release_registry.go` | 原定义安装/发现/归属校验和不可变历史，不建第二目录；`checkNamedQuery`、`checkQueryFields` |
| `capabilities/server/quota.go` | `capabilities/server/engine/automation/automation_work.go` | 原工作尝试次数配额，不是模型Token预算；`quota`、`usedMinute`、`over`、`spend` |
| `capabilities/server/reads.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`Input`、`Read` |
| `capabilities/server/record_archive_links.go` | `capabilities/server/kernel/records/record_history_index.go` | 原历史与引用/唯一索引保留；`validateArchiveLinkLocked`、`checkArchiveWriteLocked` |
| `capabilities/server/record_history.go` | `capabilities/server/kernel/records/record_history_index.go` | 原历史与引用/唯一索引保留；`attachHistory` |
| `capabilities/server/record_links.go` | `capabilities/server/kernel/records/record_history_index.go` | 原历史与引用/唯一索引保留；`validateUniqueLinkLocked`、`validateChangedLinkConstraintsLocked` |
| `capabilities/server/record_read_scope.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`tenantLane`、`Lock`、`lockContext` |
| `capabilities/server/record_reference_index.go` | `capabilities/server/kernel/records/record_history_index.go` | 原历史与引用/唯一索引保留；`referenceIndex`、`referenceBucket` |
| `capabilities/server/record_sets.go` | `capabilities/server/kernel/records/record_collections.go` | 获权集合、聚合、排序和完整性预算；`recordSetBinding`、`ValidateRecordSet` |
| `capabilities/server/record_sql_candidates.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`sqlRecordFilters` |
| `capabilities/server/record_unique_index.go` | `capabilities/server/kernel/records/record_history_index.go` | 原历史与引用/唯一索引保留；`uniqueKey`、`uniqueIndex` |
| `capabilities/server/records.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`recordStore`、`entityType`、`Changed`、`held` |
| `capabilities/server/records_access.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`viewOf`、`Entities` |
| `capabilities/server/records_current.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`currentRows`、`view`、`Get` |
| `capabilities/server/records_detail.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`RecordView`、`RecordDelivery`、`recordOf`、`recordDeliveries` |
| `capabilities/server/records_enterprise.go` | `capabilities/server/kernel/records/record_enterprise_fields.go` | 记录字段引用检查，组织事实来自原owner typed角色；`EnterpriseReference`、`EnterpriseReferenceGroup`、`enterpriseReferencesRead` |
| `capabilities/server/records_query.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`RecordPage`、`find`、`matchingQuery` |
| `capabilities/server/records_write.go` | `capabilities/server/kernel/records/record_access.go` | 固定视图、私有覆盖、行字段/来源授权与查询写入；`declare`、`of` |
| `capabilities/server/relation_migration.go` | `capabilities/server/engine/business/business_relations.go` | 原关系端点、版本、迁移与保留引用；`ownedRelationImage`、`appendOwnedRelationNeighbors`、`relationImage` |
| `capabilities/server/release_bindings.go` | `capabilities/server/studio/releases/release_candidates.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`candidateWithBindings` |
| `capabilities/server/release_candidate.go` | `capabilities/server/studio/releases/release_candidates.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleasePreview`、`ReleasePreviewRequest`、`ReleaseCandidate`、`releaseCandidateLocked` |
| `capabilities/server/release_changes.go` | `capabilities/server/studio/releases/release_review_checks.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleaseDraftChanges`、`SavedDraftChanges` |
| `capabilities/server/release_evaluation.go` | `capabilities/server/studio/releases/release_review_checks.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleaseEvaluationRequest`、`ReleaseEvaluationStarted`、`EvaluateRelease`、`evalEqualRaw` |
| `capabilities/server/release_extensions.go` | `capabilities/server/studio/releases/release_installation.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleaseExtensionReview`、`checkExtensionSet`、`releaseExtensionReview` |
| `capabilities/server/release_install.go` | `capabilities/server/studio/releases/release_installation.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`installationPredecessor`、`installationPlan` |
| `capabilities/server/release_prepare.go` | `capabilities/server/studio/releases/release_installation.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ownerDraftRequest`、`releaseBootstrapLocked`、`assetDraftPayload` |
| `capabilities/server/release_review.go` | `capabilities/server/studio/releases/release_review_checks.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleaseSummary`、`PromotionTarget`、`promotionsOf`、`withPromotions` |
| `capabilities/server/release_store.go` | `capabilities/server/studio/releases/release_retention.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`releaseStore`、`ReleaseOrigin`、`request`、`rememberRequest` |
| `capabilities/server/release_upgrade.go` | `capabilities/server/studio/releases/release_installation.go` | 确切候选、pinned闭包、前驱、安装/迁移与保留版本，接受由kernel协调；`ReleaseFieldAddition`、`ReleaseUpgradePlan`、`optionalStoredAddition`、`releaseUpgradePlanLocked` |
| `capabilities/server/replay.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`Replay`、`replayAcceptedResult` |
| `capabilities/server/routes.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`routes`、`public`、`handle` |
| `capabilities/server/routes_ai.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`routesAI` |
| `capabilities/server/routes_applications.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`PageRequest`、`routesApplications` |
| `capabilities/server/routes_build.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`decodeReleaseBody`、`routesBuild` |
| `capabilities/server/routes_core.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`routesCore`、`namedReadInputs` |
| `capabilities/server/routes_discovery.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`DispatchTarget`、`routesDiscovery`、`DispatchTargets` |
| `capabilities/server/routes_integration.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`routesIntegration` |
| `capabilities/server/routes_observations.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`routesObservations`、`readObservationRequest` |
| `capabilities/server/routes_records.go` | `capabilities/server/composition/http_routes.go` | 仅合并静态路由接线；请求语义移回四职责owner，按原鉴权/读取锁包装；`enterpriseApp`、`routesRecords` |
| `capabilities/server/runtime.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`runtime`、`CheckEnterpriseUsage`、`CheckEnterpriseTenantLink` |
| `capabilities/server/runtime_capabilities.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`RuntimeCapabilities`、`AuthorizationEngine`、`AuthorizationEngine` |
| `capabilities/server/runtime_providers.go` | `capabilities/server/kernel/tenant_coordinator.go` | 唯一租户协调与原运行能力；具体owner仅在composition接线；`RuleProvider`、`AuthorizationProvider`、`DefaultRuntimeProviders`、`Selection` |
| `capabilities/server/runtime_snapshot.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`runtimeCheckpoint`、`restoreRuntimeSnapshot` |
| `capabilities/server/runtime_state.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`storageDeltaBuilder`、`acceptedStateReader`、`acceptedStorageDelta`、`value` |
| `capabilities/server/sequence.go` | `capabilities/server/engine/business/business_books.go` | 原账簿/编号与迁移，接受前不分配正式状态；`Next` |
| `capabilities/server/sequence_migration.go` | `capabilities/server/engine/business/business_books.go` | 原账簿/编号与迁移，接受前不分配正式状态；`sequenceWaterline` |
| `capabilities/server/sequences.go` | `capabilities/server/engine/business/business_books.go` | 原账簿/编号与迁移，接受前不分配正式状态；`sequences`、`take`、`clone` |
| `capabilities/server/server.go` | `capabilities/server/kernel/transport/transport_http.go` | 原HTTP/认证边缘/资源/诊断，不放业务规则；`Host`、`Tokens`、`Now` |
| `capabilities/server/settings.go` | `capabilities/server/kernel/control/control_settings.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`settingValues`、`get`、`set` |
| `capabilities/server/simulate.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`Simulation`、`SimulatedFlow`、`Simulate` |
| `capabilities/server/simulate_candidate.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`CandidateSimulationRequest`、`SimulationStep`、`SimulateCandidate`、`runCandidateSimulation` |
| `capabilities/server/simulate_compute.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`pendingComputeEffects`、`settleComputeFixture` |
| `capabilities/server/simulate_function.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`configureFunctionFixtureModel`、`pendingFunctionEffects` |
| `capabilities/server/simulate_samples.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`SimulationSample`、`simulationEnvironment`、`newSampleOwner`、`AcceptedLedger` |
| `capabilities/server/simulate_trace.go` | `capabilities/server/studio/trials/trial_runtime.go` | 原隔离试用与原规则、权限/样本/外部意图，不造模拟执行器；`TraceEnabled`、`RecordDecisionTrace` |
| `capabilities/server/snapshot.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`tenantState`、`snapshotCompatibility`、`snapshotKey`、`runtimeSnapshotKey` |
| `capabilities/server/sources.go` | `capabilities/server/engine/integrations/integration_sources.go` | 原来源/连接/游标及失败责任，保留凭据引用；`integrationPreparation`、`PullSources`、`prepareIntegration` |
| `capabilities/server/sources_profiles.go` | `capabilities/server/engine/integrations/integration_sources.go` | 原来源/连接/游标及失败责任，保留凭据引用；`readOData`、`odataLiteral` |
| `capabilities/server/staged_decision.go` | `capabilities/server/kernel/tenant_submission.go` | 私有提交、定义依据、幂等与未知结果；`stagedDecision`、`stagedEffectPanic`、`HoldChanges`、`InputRefused` |
| `capabilities/server/status_reads.go` | `capabilities/server/kernel/tenant_reads.go` | 协调读取/变化监听及事件续接；业务回调归原owner；`statusReadSchedule`、`statusReadAttempt`、`retain`、`next` |
| `capabilities/server/storage_provider.go` | `capabilities/server/kernel/tenant_recovery.go` | 原格式、前驱、检查点和旧输入/新结果各自恢复；`JournalProvider`、`MemoryStorageModule`、`storageModule` |
| `capabilities/server/submission_schema.go` | `capabilities/server/kernel/tenant_submission.go` | 私有提交、定义依据、幂等与未知结果；`currentSubmissionSchema`、`actionSchemaVersion` |
| `capabilities/server/submit.go` | `capabilities/server/kernel/tenant_submission.go` | 私有提交、定义依据、幂等与未知结果；`unknown`、`Submit` |
| `capabilities/server/system_definitions.go` | `capabilities/server/studio/releases/release_system_history.go` | 原系统候选冻结/安装/恢复；新写入口审批后冻结；`systemDefinitionProjection`、`systemDefinitionPeers`、`systemDefinitionRefusal`、`FreezeSystemDefinitions` |
| `capabilities/server/telemetry.go` | `capabilities/server/kernel/tenant_diagnostics.go` | 隔离健康/审计与可观测事实，不产生业务决定；`begin`、`current` |
| `capabilities/server/tenant_registry.go` | `capabilities/server/kernel/control/control_tenants.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`tenantRegistry`、`newTenantRegistry`、`list` |
| `capabilities/server/tenants.go` | `capabilities/server/kernel/control/control_tenants.go` | 静态可信身份/租户/凭据/配置与治理，原接受归属不变；`TenantSpec`、`TenantTemplate`、`load`、`get` |
| `capabilities/server/transaction_iterations.go` | `capabilities/server/engine/business/business_constraints.go` | 原约束、业务版本、类型值和事务见证；`CaptureTxnIteration` |
| `capabilities/server/transaction_transport.go` | `capabilities/server/engine/business/business_constraints.go` | 原约束、业务版本、类型值和事务见证；`txnCalls`、`TxnTransport`、`TakeTxnStep` |
| `capabilities/server/transcripts.go` | `capabilities/server/aigateway/gateway_transcripts.go` | 原获权对话/转录来源；`transcriptDays`、`transcribe` |
| `capabilities/server/transfer.go` | `capabilities/server/engine/integrations/integration_protocols.go` | 冻结外部意图、版本协议与交付分类；`ImportRow`、`Import`、`probeDiagnosed` |
| `capabilities/server/translator.go` | `capabilities/server/kernel/transport/transport_declarations.go` | 原声明语言与权限/定义世代缓存，业务值不翻译；`translator`、`dictionaryKey`、`appManifest`、`installApplications` |
| `capabilities/server/uploads.go` | `capabilities/server/engine/resources/resource_files.go` | 原文件字节/上传/托管/交接，业务记录权限仍独立；`uploads`、`add`、`olderThan` |
| `capabilities/server/view_cache.go` | `capabilities/server/kernel/transport/transport_declarations.go` | 原声明语言与权限/定义世代缓存，业务值不翻译；`viewCache`、`manifestEntry`、`rights`、`definitionsFingerprint` |
| `capabilities/server/view_json.go` | `capabilities/server/kernel/transport/transport_declarations.go` | 原声明语言与权限/定义世代缓存，业务值不翻译；`viewBody`、`viewEntry`、`compressed`、`cachedView` |
| `capabilities/server/wasm.go` | `capabilities/server/engine/compute/compute_isolation.go` | 原受控制品、ABI、预算、取消与进程隔离；`WasmRequest`、`WasmDataInput`、`NewWasmEngine`、`Close` |
| `capabilities/server/web_files.go` | `capabilities/server/kernel/transport/transport_http.go` | 原HTTP/认证边缘/资源/诊断，不放业务规则；`webAsset`、`webFiles`、`hashedName`、`loadWeb` |
| `capabilities/server/words.go` | `capabilities/server/kernel/transport/transport_declarations.go` | 原声明语言与权限/定义世代缓存，业务值不翻译；`tenantWords`、`Word`、`set`、`clone` |
| `capabilities/server/work_board.go` | `capabilities/server/engine/automation/automation_work.go` | 原任务/工作/通知与投递，监控不增加控制权；`workBoard`、`newWorkBoard`、`addJob` |
| `capabilities/server/writebacks.go` | `capabilities/server/engine/integrations/integration_pipelines.go` | 原管道与回写编排，冻结原输入/结果；`IntegrationEffect`、`integrationEffects`、`writebackEffects` |

## 5. platform非page 88个文件

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/platform/action_input_schema.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ActionInputSchema`、`inputFieldSchema` |
| `capabilities/server/platform/actions.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Action`、`Approval`、`DefineRole`、`UsePolicies` |
| `capabilities/server/platform/agent.go` | `capabilities/server/platform/ai.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Agent`、`Case` |
| `capabilities/server/platform/agent_asset.go` | `capabilities/server/platform/dependencies.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AgentReleaseDescriptor`、`Check`、`Dependencies` |
| `capabilities/server/platform/ai.go` | `capabilities/server/platform/ai.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AIVendor`、`AIProvider`、`Name` |
| `capabilities/server/platform/alert.go` | `capabilities/server/platform/work.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AlertRule`、`Check` |
| `capabilities/server/platform/app.go` | `capabilities/server/platform/app.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Member`、`Grant`、`Active`、`RolesIn` |
| `capabilities/server/platform/application_artifact.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ApplicationProfile`、`ApplicationArtifact`、`UnmarshalJSON`、`Check` |
| `capabilities/server/platform/application_concepts.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AppConcept`、`CheckConcepts` |
| `capabilities/server/platform/application_filters.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CheckFilterSchema` |
| `capabilities/server/platform/application_header.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ApplicationHeader`、`ApplicationHeaderItem`、`CheckHeader`、`VisibleHeader` |
| `capabilities/server/platform/application_variables.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`IsWritable`、`CheckVariables` |
| `capabilities/server/platform/asset_lifecycle.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AssetEnvironmentRequirement`、`AssetReleaseContext`、`NewAssetRegistry`、`Register` |
| `capabilities/server/platform/authorization.go` | `capabilities/server/platform/app.go` | 成员/请求/来源授权公共角色；纯授权算法由原platform/authz提供，不复制；`AuthzRequest`、`AuthzDecision`、`Authorize` |
| `capabilities/server/platform/binding.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Binding`、`Predicate`、`Check`、`Resolve` |
| `capabilities/server/platform/block.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CapabilityDescriptor`、`BlockPort`、`ControlBlocks` |
| `capabilities/server/platform/business_version.go` | `capabilities/server/platform/entity.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Versioning`、`VersionPinBinding`、`withinBusinessPeriod`、`ActiveOn` |
| `capabilities/server/platform/composition.go` | `capabilities/server/platform/composition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CapabilityContract`、`CapabilityModule`、`Check`、`Check` |
| `capabilities/server/platform/confirmation.go` | `capabilities/server/platform/protocol.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ConfirmationTransport`、`ConfirmationRuntime`、`ConfirmationCompletion`、`Check` |
| `capabilities/server/platform/constraint.go` | `capabilities/server/platform/entity.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Constraint`、`UnmarshalJSON`、`ConstraintSpec` |
| `capabilities/server/platform/credential.go` | `capabilities/server/platform/credential.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CredentialBinding`、`CredentialDefinition` |
| `capabilities/server/platform/decimal_value.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`DecimalValue`、`ParseDecimal`、`Check` |
| `capabilities/server/platform/decision_trace.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`DecisionTrace`、`DecisionTraceRecorder`、`TraceEnabled`、`Trace` |
| `capabilities/server/platform/definition.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`AssetKind`、`NamedQuery`、`String`、`Check` |
| `capabilities/server/platform/dependencies.go` | `capabilities/server/platform/dependencies.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Dependencies`、`Capability`、`Check`、`MarshalJSON` |
| `capabilities/server/platform/dependency_graph.go` | `capabilities/server/platform/dependencies.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`DependencyGraph`、`NewDependencyGraph`、`NewReleaseDependencyGraph` |
| `capabilities/server/platform/digest.go` | `capabilities/server/platform/dependencies.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ValidDigest` |
| `capabilities/server/platform/dispatch.go` | `capabilities/server/platform/protocol.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`DispatchCommand`、`Dispatch`、`SafeRetry`、`Check` |
| `capabilities/server/platform/effects.go` | `capabilities/server/platform/protocol.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`EffectKind`、`Effect`、`Emit` |
| `capabilities/server/platform/entity.go` | `capabilities/server/platform/entity.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Record`、`Stamp`、`refType`、`transactionDestination` |
| `capabilities/server/platform/evaluation_set.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`EvaluationPolicy`、`EvaluationCase`、`Check`、`CheckEvaluationPolicy` |
| `capabilities/server/platform/extension.go` | `capabilities/server/platform/entity.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ExtensionSource`、`Check`、`CheckExtension` |
| `capabilities/server/platform/fact_archive.go` | `capabilities/server/platform/record_image.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`packedFactArchive`、`CompactFacts`、`Records` |
| `capabilities/server/platform/files.go` | `capabilities/server/platform/files.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`File`、`ImageRegion`、`MaxFileSize` |
| `capabilities/server/platform/finance.go` | `capabilities/server/platform/transaction.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Journal`、`JournalLine` |
| `capabilities/server/platform/flow.go` | `capabilities/server/platform/flow.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Flow`、`Continuous`、`Set`、`DataOf` |
| `capabilities/server/platform/flow_retry.go` | `capabilities/server/platform/flow.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`StreamPreparationRetry`、`Check`、`Tries` |
| `capabilities/server/platform/flow_runtime.go` | `capabilities/server/platform/flow.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`FlowInstance`、`FlowOutput`、`UnmarshalJSON`、`FlowBatchKey` |
| `capabilities/server/platform/function.go` | `capabilities/server/platform/ai.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`FunctionDefinitions`、`AIFunction`、`RequestFunction`、`Check` |
| `capabilities/server/platform/input.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CheckInputs`、`InputIssues` |
| `capabilities/server/platform/interface.go` | `capabilities/server/platform/entity.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Interface`、`InterfaceField`、`Check`、`Implements` |
| `capabilities/server/platform/invocation.go` | `capabilities/server/platform/operations.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`CapabilityInvocation`、`CapabilityResult` |
| `capabilities/server/platform/kernel.go` | `capabilities/server/platform/ledger.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`KernelFactory`、`SchemaRegistry`、`Kernel`、`ForkLog` |
| `capabilities/server/platform/knowledge.go` | `capabilities/server/platform/ai.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`KnowledgeDocument`、`KnowledgeTerm` |
| `capabilities/server/platform/languages.go` | `capabilities/server/platform/app.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Languages`、`LoadLanguages`、`CheckLanguages` |
| `capabilities/server/platform/ledger.go` | `capabilities/server/platform/ledger.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Ledger`、`NewLedger`、`NewLedgerWithKernel` |
| `capabilities/server/platform/link_type.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`LinkTraversal`、`LinkType`、`Check`、`Contract` |
| `capabilities/server/platform/model_call.go` | `capabilities/server/platform/ai.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Message`、`Tool`、`Error` |
| `capabilities/server/platform/model_configuration.go` | `capabilities/server/platform/model_configuration.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ModelCapabilities`、`ModelOptions`、`Override`、`Resolve` |
| `capabilities/server/platform/named_query.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Check`、`Dependencies` |
| `capabilities/server/platform/number_value.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`NumberValue`、`NumberLiteral`、`CheckAggregateScalar` |
| `capabilities/server/platform/observation_context.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ObservationContext`、`ObservationContextRuntime`、`observationRuleFacts` |
| `capabilities/server/platform/observation_evidence.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ObservationSampleSource`、`ObservationSample` |
| `capabilities/server/platform/observation_intervals.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ObservationIntervals`、`ObservationStatePoint`、`UnmarshalJSON`、`intervalRuleSchema` |
| `capabilities/server/platform/observation_reconcile.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`IntervalIdentity`、`IntervalChange`、`intervalOverlap`、`ReconcileObservationIntervals` |
| `capabilities/server/platform/observation_source.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ObservationDefinition`、`ObservationDecision`、`CheckObservationLatest`、`CheckObservationCoverage` |
| `capabilities/server/platform/operation.go` | `capabilities/server/platform/operations.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ValueSchema`、`OperationBinding`、`Check`、`check` |
| `capabilities/server/platform/operations.go` | `capabilities/server/platform/operations.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Job`、`Retry`、`After`、`Accepts` |
| `capabilities/server/platform/org.go` | `capabilities/server/platform/org.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Date`、`Period`、`Works`、`After` |
| `capabilities/server/platform/owned_relation.go` | `capabilities/server/platform/transaction.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`TxnLink`、`OwnedRelationRuntime`、`OwnedRelationType`、`RelationEntity` |
| `capabilities/server/platform/property_type.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`PropertyType`、`Check`、`CheckField` |
| `capabilities/server/platform/proposal.go` | `capabilities/server/platform/definition.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ProposalPolicy`、`ProposalAdoption`、`Check`、`Check` |
| `capabilities/server/platform/protocol.go` | `capabilities/server/platform/protocol.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Protocol`、`ProtocolEvent`、`CheckProtocolActions`、`CheckProtocol` |
| `capabilities/server/platform/query_set.go` | `capabilities/server/platform/transaction.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RecordSetPredicate`、`QuerySet`、`CheckSet` |
| `capabilities/server/platform/reads.go` | `capabilities/server/platform/record_image.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ReadQuery`、`ReadQueryVersion` |
| `capabilities/server/platform/record_image.go` | `capabilities/server/platform/record_image.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RecordImageFormat`、`checkRecordImageFormats`、`decodeRecordImage` |
| `capabilities/server/platform/record_path.go` | `capabilities/server/platform/record_image.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RecordPathField` |
| `capabilities/server/platform/record_set.go` | `capabilities/server/platform/record_image.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RecordSet`、`RecordSetSort`、`ruleRecordSet`、`LowerRecordSetPredicate` |
| `capabilities/server/platform/refusal.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Refusal`、`BusinessRefusalFact`、`UnmarshalJSON`、`RefuseBusiness` |
| `capabilities/server/platform/refusal_escalation.go` | `capabilities/server/platform/actions.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RefusalEscalation`、`RefusalEscalationRequest`、`UnmarshalJSON`、`Check` |
| `capabilities/server/platform/relations.go` | `capabilities/server/platform/protocol.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RecordComment`、`RecordFollow`、`FollowID`、`Links` |
| `capabilities/server/platform/resources.go` | `capabilities/server/platform/app.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Resources`、`ResourceState`、`State`、`Begin` |
| `capabilities/server/platform/revision.go` | `capabilities/server/platform/revision.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ReleaseAsset`、`FlowReleaseDescriptor`、`PageReleaseAsset`、`ApplicationReleaseAsset` |
| `capabilities/server/platform/rule_criteria.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RuleCriterion`、`RuleCriteria`、`EvaluateRule`、`Check` |
| `capabilities/server/platform/rule_scope.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RuleReadSpec`、`RuleCollection`、`RuleCollectionSubjectField`、`RuleFieldKind` |
| `capabilities/server/platform/rule_table.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`RuleTablePlan`、`RuleTableRow`、`Decide`、`DecideSchemas` |
| `capabilities/server/platform/rules.go` | `capabilities/server/platform/rules.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Expr`、`ExprLiteral`、`Rules`、`CompileExpr` |
| `capabilities/server/platform/sample_set.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`SampleRecords`、`SampleSet`、`Check`、`Dependencies` |
| `capabilities/server/platform/schema_version.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`SchemaVersion`、`Check`、`SchemaContentVersion` |
| `capabilities/server/platform/sequence.go` | `capabilities/server/platform/transaction.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Sequence`、`Check`、`Format` |
| `capabilities/server/platform/simulation_owner.go` | `capabilities/server/platform/snapshot.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`SimulationFactory`、`SimulationIdentityFactory` |
| `capabilities/server/platform/snapshot.go` | `capabilities/server/platform/snapshot.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Snapshotter`、`SnapshotFormatOwner`、`Snapshot`、`Restore` |
| `capabilities/server/platform/system_definitions.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`SystemDefinitionHost`、`AttachedSystemDefinitions`、`UnmarshalJSON` |
| `capabilities/server/platform/system_source.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`SystemSource`、`SystemSourceFile` |
| `capabilities/server/platform/test_plan.go` | `capabilities/server/platform/asset_lifecycle.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`TestPlanDescriptor`、`TestStep`、`Check`、`CheckSimulationOrganization` |
| `capabilities/server/platform/transaction.go` | `capabilities/server/platform/transaction.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`BusinessTxn`、`TxnStep`、`UnmarshalJSON`、`CaptureTxnIteration` |
| `capabilities/server/platform/work.go` | `capabilities/server/platform/work.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`Assignment`、`Assign` |
| `capabilities/server/platform/work_runtime.go` | `capabilities/server/platform/work.go` | 单一公共API的真实类型/角色/纯契约合并；不移除协议或复制运行状态；`ApprovalOwner`、`ApprovalRetirementOwner`、`CheckApprovalRetirement` |

## 6. platform页面78个文件（详细责任引用页面附表）

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/platform/page_ai.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageAI`、`AIVariables`、`checkAI` |
| `capabilities/server/platform/page_alert.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageAlertBanner`、`checkAlertBanner` |
| `capabilities/server/platform/page_boolean_input.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkBooleanInput` |
| `capabilities/server/platform/page_button_group.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageButton`、`checkButtonGroup` |
| `capabilities/server/platform/page_choice_input.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageChoiceInput`、`checkChoiceInput` |
| `capabilities/server/platform/page_collaboration.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`collaborationWidget`、`checkCollaboration` |
| `capabilities/server/platform/page_collection_analysis.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageAnalysisStep`、`PageCollectionAnalysis`、`AnalysisVariables`、`checkCollectionAnalysis` |
| `capabilities/server/platform/page_collection_builder.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageCollectionBuilder`、`collectionBuilderInput`、`checkCollectionBuilder` |
| `capabilities/server/platform/page_collection_input.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageCollectionInput`、`CollectionInputObject`、`collectionInputObject` |
| `capabilities/server/platform/page_composite.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`ModelViewBinding`、`PageComposite`、`CompositeLimits`、`CompositePortRoles` |
| `capabilities/server/platform/page_compute_resource.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageComputeResource`、`checkComputeResource`、`ComputeResources` |
| `capabilities/server/platform/page_content.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageContentVersion`、`CheckPageContentVersion` |
| `capabilities/server/platform/page_context_views.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageBreadcrumb`、`PageAvatarStack`、`contextView`、`ContextViewVariables` |
| `capabilities/server/platform/page_date_input.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`CivilDateValid`、`TimestampOffsetValid` |
| `capabilities/server/platform/page_detail_presentation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageDetailPresentation`、`checkDetailPresentation`、`CheckDetailBarcode` |
| `capabilities/server/platform/page_device.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkDevice` |
| `capabilities/server/platform/page_document.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageUnusedWidget`、`PageDocument`、`Check`、`Visible` |
| `capabilities/server/platform/page_embedding.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageEmbedding`、`Navigation`、`EmbeddingVariables` |
| `capabilities/server/platform/page_events.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageOverlayPresentation`、`PageOverlay`、`Navigation`、`Returns` |
| `capabilities/server/platform/page_exploration.go` | `capabilities/server/platform/page_queries.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageResourceList`、`PageAssetDirectoryItem`、`explorationWidget`、`ExplorationVariables` |
| `capabilities/server/platform/page_external_frame.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageExternalFrame`、`ValidPageExternalFrame`、`checkExternalFrame` |
| `capabilities/server/platform/page_facets.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkFacets`、`CheckFacetSchema` |
| `capabilities/server/platform/page_gauge.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageGauge`、`checkGauge` |
| `capabilities/server/platform/page_heatmap.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkHeatmap`、`CheckHeatmap` |
| `capabilities/server/platform/page_histogram.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageHistogram`、`checkHistogram`、`CheckHistogram` |
| `capabilities/server/platform/page_inline_action.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`CheckInlineAction` |
| `capabilities/server/platform/page_input.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkInputPresentation` |
| `capabilities/server/platform/page_interface.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageInterface`、`PagePort`、`checkInterface`、`RecordVariableObject` |
| `capabilities/server/platform/page_kanban.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`CheckKanban`、`CheckKanban` |
| `capabilities/server/platform/page_layout_size.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkLayoutSize` |
| `capabilities/server/platform/page_leaderboard.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageLeaderboard`、`Sort`、`checkLeaderboard` |
| `capabilities/server/platform/page_loop.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageLoop`、`loopOwners`、`overlayOwners` |
| `capabilities/server/platform/page_metric_presentation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageMetricAnnotation`、`PageMetricPresentation`、`checkMetricPresentation`、`CheckMetricPresentation` |
| `capabilities/server/platform/page_model.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`FieldRef`、`ModelRelation`、`Segments`、`Key` |
| `capabilities/server/platform/page_notice.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageNotice`、`checkNotice` |
| `capabilities/server/platform/page_observation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageObservationSignal`、`PageObservationMetadata`、`hasObservationConfiguration`、`HasRecordOutput` |
| `capabilities/server/platform/page_ontology_interface.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`frozenPageInterfaceQuery`、`WindowVariableInterface` |
| `capabilities/server/platform/page_progress.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkProgress` |
| `capabilities/server/platform/page_property.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PropertyValueType`、`CheckPropertySchema` |
| `capabilities/server/platform/page_queries.go` | `capabilities/server/platform/page_queries.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageQuery`、`PageQuerySet`、`Values`、`Variables` |
| `capabilities/server/platform/page_query_inventory.go` | `capabilities/server/platform/page_queries.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`ActiveQueryPlans` |
| `capabilities/server/platform/page_query_sets.go` | `capabilities/server/platform/page_queries.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`CheckQuerySetConditions`、`MissingSetInput` |
| `capabilities/server/platform/page_range.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRangeInput`、`checkRangeInput` |
| `capabilities/server/platform/page_record_calendar.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordCalendar`、`ValidCalendarMonth`、`checkRecordCalendar` |
| `capabilities/server/platform/page_record_card.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordCard`、`checkRecordCard`、`CheckRecordCard` |
| `capabilities/server/platform/page_record_chart.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordChart`、`checkRecordChart`、`CheckRecordChart` |
| `capabilities/server/platform/page_record_comparison.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordComparison`、`checkRecordComparison`、`CheckRecordComparison` |
| `capabilities/server/platform/page_record_events.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageEventTone`、`PageRecordEvents`、`checkRecordEvents`、`CheckRecordEvents` |
| `capabilities/server/platform/page_record_gantt.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordGantt`、`ValidGanttRange`、`checkRecordGantt` |
| `capabilities/server/platform/page_record_links.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordLink`、`checkRecordLinks`、`CheckRecordLinks` |
| `capabilities/server/platform/page_record_list.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordList`、`checkRecordList`、`CheckRecordList` |
| `capabilities/server/platform/page_record_picker.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordPicker`、`checkRecordPicker`、`CheckRecordPicker` |
| `capabilities/server/platform/page_record_scatter.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordScatter`、`checkScatter`、`CheckScatter` |
| `capabilities/server/platform/page_record_view.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordView`、`checkRecordView` |
| `capabilities/server/platform/page_record_work.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageActionParameter`、`PageActionTable`、`checkRecordWork`、`ActionParameterCompatible` |
| `capabilities/server/platform/page_region_presentation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRegionPresentation`、`checkRegionPresentation` |
| `capabilities/server/platform/page_rules.go` | `capabilities/server/platform/page_rules.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`BindRuleTypes` |
| `capabilities/server/platform/page_separator.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageSeparator`、`checkSeparator` |
| `capabilities/server/platform/page_shared_record_context.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`sharedContextRecord`、`sharedTelemetryRecord` |
| `capabilities/server/platform/page_shared_record_output.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkSharedRecordOutput` |
| `capabilities/server/platform/page_shared_record_set.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkSharedRecordSetOutput` |
| `capabilities/server/platform/page_slots.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkWidgetSlots`、`checkSlotParent` |
| `capabilities/server/platform/page_spacer.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageSpacer`、`checkSpacer` |
| `capabilities/server/platform/page_sparkline.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageSparkline`、`checkSparkline`、`CheckSparkline` |
| `capabilities/server/platform/page_spatial.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageRecordMap`、`PageSceneMapping`、`Check`、`finite` |
| `capabilities/server/platform/page_status_tracker.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageStatusTracker`、`checkStatusTracker`、`CheckStatusTracker` |
| `capabilities/server/platform/page_summary.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkSummary`、`CheckSummary` |
| `capabilities/server/platform/page_table_edit.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkTableEdit`、`CheckTableEdit` |
| `capabilities/server/platform/page_table_presentation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageTableColumn`、`PageTablePresentation`、`checkTablePresentation`、`CheckTablePresentation` |
| `capabilities/server/platform/page_terms.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkTerms`、`CheckTerms` |
| `capabilities/server/platform/page_timeline.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`CheckTimeline` |
| `capabilities/server/platform/page_titles.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkTitles` |
| `capabilities/server/platform/page_treemap.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`checkTreemap` |
| `capabilities/server/platform/page_unused.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`ownedChildren`、`checkUnusedWidgets` |
| `capabilities/server/platform/page_variables.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`PageVariable`、`PageResourceSource`、`pageLiteralType`、`CheckVariables` |
| `capabilities/server/platform/page_widget_validation.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`configured`、`checkWidgetConfiguration` |
| `capabilities/server/platform/page_widgets.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`pageModelRole`、`pageModelPort`、`widgetSlot`、`PageUIManifest` |
| `capabilities/server/platform/page_work_views.go` | `capabilities/server/platform/page_model.go` | 安全契约保留，视觉迁Web；逐项责任见archive/2026-10-28-decouple-page-presentation-contract/design.md附表A；`workFeed`、`checkWorkViews` |

## 7. apps/build 73个文件

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/apps/build/actions.go` | `capabilities/server/studio/build/build_actions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`creator`、`checkProcess`、`checkActionCondition` |
| `capabilities/server/apps/build/activation.go` | `capabilities/server/studio/build/build_artifacts.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Draft`、`prepareReleasePublications`、`generatedReleasePage` |
| `capabilities/server/apps/build/agent.go` | `capabilities/server/studio/build/build_agents.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Agent`、`AgentCase`、`agentEntity`、`agentDraftEntity` |
| `capabilities/server/apps/build/agent_release.go` | `capabilities/server/studio/build/build_agents.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`agentReleaseAsset`、`decodeAgentAsset` |
| `capabilities/server/apps/build/alert.go` | `capabilities/server/studio/build/build_constraints.go` | 原声明/编译/发布同一Build owner内聚，保留实际告警或扩展语义；`alertEntity`、`checkAlert` |
| `capabilities/server/apps/build/alert_release.go` | `capabilities/server/studio/build/build_constraints.go` | 原声明/编译/发布同一Build owner内聚，保留实际告警或扩展语义；`alertReleaseAsset`、`sameRuleAsset` |
| `capabilities/server/apps/build/application.go` | `capabilities/server/studio/build/build_applications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Application`、`applicationEntity`、`hand` |
| `capabilities/server/apps/build/asset_lifecycle.go` | `capabilities/server/studio/build/build_artifacts.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`AssetKinds`、`ResolvePinned` |
| `capabilities/server/apps/build/authoring.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`SavedObjectActions` |
| `capabilities/server/apps/build/blueprint.go` | `capabilities/server/studio/build/build_blueprints.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CompleteBlueprintAssets`、`RootDrafts` |
| `capabilities/server/apps/build/build.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Build`、`ReleaseProfile`、`AttachBuild`、`New` |
| `capabilities/server/apps/build/business_version.go` | `capabilities/server/studio/build/build_versions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`businessVersionInstallOrder`、`checkVersioning` |
| `capabilities/server/apps/build/code.go` | `capabilities/server/studio/build/build_compute.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Code`、`Definition`、`sourceHash` |
| `capabilities/server/apps/build/confirmation.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`confirmationPartyBindings`、`confirmationBindings`、`confirmationEntity`、`confirmationActions` |
| `capabilities/server/apps/build/connection.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`connectionEntity`、`connectionActions` |
| `capabilities/server/apps/build/constraints.go` | `capabilities/server/studio/build/build_constraints.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`checkConstraints` |
| `capabilities/server/apps/build/creates.go` | `capabilities/server/studio/build/build_objects.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`checkCreates`、`create` |
| `capabilities/server/apps/build/dataset.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`datasetEntity`、`datasetVersionEntity` |
| `capabilities/server/apps/build/definition_history.go` | `capabilities/server/studio/build/build_artifacts.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`definitionFamily`、`definitionFamilyOf`、`setDefinitionFamily` |
| `capabilities/server/apps/build/dispatch.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`dispatchBindings`、`dispatchEntity`、`dispatchActions` |
| `capabilities/server/apps/build/draft_changes.go` | `capabilities/server/studio/build/build_publications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`SavedDraftChanges`、`draftComparison` |
| `capabilities/server/apps/build/evaluation.go` | `capabilities/server/studio/build/build_evaluations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`evaluationEntity`、`evaluationActions` |
| `capabilities/server/apps/build/evaluation_release.go` | `capabilities/server/studio/build/build_evaluations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`testPlanAsset`、`testPlanKind` |
| `capabilities/server/apps/build/extension.go` | `capabilities/server/studio/build/build_objects.go` | 原声明/编译/发布同一Build owner内聚，保留实际告警或扩展语义；`captureExtensionSource`、`checkExtensionDependents` |
| `capabilities/server/apps/build/formula.go` | `capabilities/server/studio/build/build_formulas.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`formulaExpr`、`checkFormulas` |
| `capabilities/server/apps/build/function.go` | `capabilities/server/studio/build/build_functions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`functionEntity`、`checkFunction` |
| `capabilities/server/apps/build/function_call.go` | `capabilities/server/studio/build/build_functions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`functionCallEntity`、`functionCallActions` |
| `capabilities/server/apps/build/function_release.go` | `capabilities/server/studio/build/build_functions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`functionReleaseAsset`、`FunctionReleaseAsset` |
| `capabilities/server/apps/build/host_roles.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`NewSimulationApp`、`CandidateActions` |
| `capabilities/server/apps/build/integration_release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`integrationType`、`integrationKindOf` |
| `capabilities/server/apps/build/joint_release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`draftInventories`、`recordKind`、`lookupEntity` |
| `capabilities/server/apps/build/journal.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_rule_tables.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`checkJournal`、`numberingOf` |
| `capabilities/server/apps/build/link_type.go` | `capabilities/server/studio/build/build_links.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`LinkType`、`Definition`、`linkTypeEntity` |
| `capabilities/server/apps/build/link_type_release.go` | `capabilities/server/studio/build/build_links.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`linkTypeReleaseAsset`、`LinkTypeReleaseAsset` |
| `capabilities/server/apps/build/match.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`matchEntity`、`publishMatch` |
| `capabilities/server/apps/build/objects.go` | `capabilities/server/studio/build/build_objects.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`objectEntity`、`conditionLookup` |
| `capabilities/server/apps/build/observation.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`observationEntity`、`observationDefinition` |
| `capabilities/server/apps/build/page.go` | `capabilities/server/studio/build/build_pages.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Page`、`InlineEdit`、`pageEntity`、`pageDraftEntity` |
| `capabilities/server/apps/build/page_content.go` | `capabilities/server/studio/build/build_pages.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`retainPagePublication`、`PageContent` |
| `capabilities/server/apps/build/pipeline.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`pipelineEntity`、`pipelineActions` |
| `capabilities/server/apps/build/posts.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_rule_tables.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`BalanceID`、`checkPosts` |
| `capabilities/server/apps/build/process.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`processEntity`、`publishProcess` |
| `capabilities/server/apps/build/process_runtime.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`flowOf` |
| `capabilities/server/apps/build/property_fields.go` | `capabilities/server/studio/build/build_properties.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`propertyBindings`、`checkPropertyFields` |
| `capabilities/server/apps/build/property_type.go` | `capabilities/server/studio/build/build_properties.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`PropertyType`、`Definition`、`propertyTypeEntity` |
| `capabilities/server/apps/build/property_type_release.go` | `capabilities/server/studio/build/build_properties.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`propertyTypeReleaseAsset`、`PropertyTypeReleaseAsset` |
| `capabilities/server/apps/build/proposal.go` | `capabilities/server/studio/build/build_blueprints.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`proposalConsumption`、`checkActionAdoption`、`prepareProposal` |
| `capabilities/server/apps/build/protocol.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`protocolReadName`、`objectProvisions` |
| `capabilities/server/apps/build/public.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`JointDraftAssets`、`Observation` |
| `capabilities/server/apps/build/publications.go` | `capabilities/server/studio/build/build_publications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`AcceptedPublicationSchemas`、`publicationImage` |
| `capabilities/server/apps/build/query.go` | `capabilities/server/studio/build/build_queries.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Query`、`Definition`、`queryEntity` |
| `capabilities/server/apps/build/query_release.go` | `capabilities/server/studio/build/build_queries.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`queryReleaseAsset`、`QueryReleaseAsset` |
| `capabilities/server/apps/build/record_sets.go` | `capabilities/server/studio/build/build_queries.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`checkRecordSets` |
| `capabilities/server/apps/build/release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ReleaseAssets`、`FlowReleaseAsset` |
| `capabilities/server/apps/build/release_execution.go` | `capabilities/server/studio/build/build_publications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`releaseExecution`、`Manifest`、`retainedCodeObject` |
| `capabilities/server/apps/build/rules.go` | `capabilities/server/studio/build/build_formulas.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ruleLookup`、`ruleSpec` |
| `capabilities/server/apps/build/sample_set.go` | `capabilities/server/studio/build/build_tests.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`SampleSetRecord`、`descriptor`、`sampleSetEntity` |
| `capabilities/server/apps/build/schema_version.go` | `capabilities/server/studio/build/build_versions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`captureObjectSchema`、`objectSchemaBody` |
| `capabilities/server/apps/build/source.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`sourceEntity`、`sourceActions` |
| `capabilities/server/apps/build/system_definitions.go` | `capabilities/server/studio/build/build_publications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`checkSystemDefinitionWrite` |
| `capabilities/server/apps/build/table.go` | `capabilities/server/studio/build/build_compute.go`；`capabilities/server/engine/business/business_rule_tables.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`tableEntity`、`publishTable` |
| `capabilities/server/apps/build/table_release.go` | `capabilities/server/studio/build/build_compute.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`tableReleaseAsset`、`tableFromAsset` |
| `capabilities/server/apps/build/testplan.go` | `capabilities/server/studio/build/build_tests.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`testPlanEntity`、`checkTestPlanFields` |
| `capabilities/server/apps/build/transaction.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`decodeTxnValues`、`transact` |
| `capabilities/server/apps/build/transaction_check.go` | `capabilities/server/studio/build/build_transactions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`txnWrite`、`walkTxnSteps`、`walkBusinessTxn` |
| `capabilities/server/apps/build/transaction_intervals.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`intervalFields`、`checkTxnIntervals` |
| `capabilities/server/apps/build/transaction_legacy.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`CanonicalObject`、`legacyTxnExpr` |
| `capabilities/server/apps/build/transaction_range.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`withTxnRange`、`checkTxnRange` |
| `capabilities/server/apps/build/transaction_refusal.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`txnRefusalResult`、`checkRefusalDeclarations`、`businessRefusalCode` |
| `capabilities/server/apps/build/transaction_rows.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`txnIterationSource`、`txnRecordRows` |
| `capabilities/server/apps/build/transaction_steps.go` | `capabilities/server/studio/build/build_transactions.go`；`capabilities/server/engine/business/business_transactions.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`checkTypedTxnStep`、`txnMath` |
| `capabilities/server/apps/build/trial.go` | `capabilities/server/studio/build/build_publications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ApplicationDrafts`、`applicationDrafts` |
| `capabilities/server/apps/build/writeback.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`writebackEntity`、`writebackActions` |

## 8. internal/construction 42个文件

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/internal/construction/actions.go` | `capabilities/server/studio/build/build_actions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`State`、`Action`、`Field`、`ValueConditionValue` |
| `capabilities/server/internal/construction/activation.go` | `capabilities/server/studio/build/build_artifacts.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ReleasePublication` |
| `capabilities/server/internal/construction/alert.go` | `capabilities/server/studio/build/build_constraints.go` | 原声明/编译/发布同一Build owner内聚，保留实际告警或扩展语义；`Alert`、`Definition`、`Matches` |
| `capabilities/server/internal/construction/application.go` | `capabilities/server/studio/build/build_applications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Group` |
| `capabilities/server/internal/construction/application_projection.go` | `capabilities/server/studio/build/build_applications.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ApplicationProjection`、`ProjectApplication` |
| `capabilities/server/internal/construction/build.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Object`、`Field`、`TypeOf`、`ValueChoices` |
| `capabilities/server/internal/construction/code.go` | `capabilities/server/studio/build/build_compute.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；常量/协议声明 |
| `capabilities/server/internal/construction/code_declarations.go` | `capabilities/server/studio/build/build_blueprints.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CodeDeclarations`、`Check`、`AgentDescriptor` |
| `capabilities/server/internal/construction/connection.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Connection`、`ConnectionCheck`、`Resolve` |
| `capabilities/server/internal/construction/dataset.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Dataset`、`DatasetField`、`ValueTypeOf`、`VersionID` |
| `capabilities/server/internal/construction/evaluation.go` | `capabilities/server/studio/build/build_evaluations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Evaluation`、`EvaluationAttempt` |
| `capabilities/server/internal/construction/formula.go` | `capabilities/server/studio/build/build_formulas.go`；`capabilities/server/internal/rules/formula_lowering.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`ValueFormulaNode`、`ValueFormulaParser`、`ValueParseFormula`、`Skip` |
| `capabilities/server/internal/construction/function.go` | `capabilities/server/studio/build/build_functions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Function`、`Definition` |
| `capabilities/server/internal/construction/function_call.go` | `capabilities/server/studio/build/build_functions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`FunctionRun` |
| `capabilities/server/internal/construction/integration_release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ConnectionDefinition`、`SourceDefinition`、`ValueIntegrationKind`、`ValueDecodeIntegration` |
| `capabilities/server/internal/construction/joint_assets.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`JointDraftAssets` |
| `capabilities/server/internal/construction/joint_release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`JointDraftRef`、`DraftChange` |
| `capabilities/server/internal/construction/journal.go` | `capabilities/server/studio/build/build_transactions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`JournalPost`、`JournalPostLine`、`Reversal`、`Entry` |
| `capabilities/server/internal/construction/link_type.go` | `capabilities/server/studio/build/build_links.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；常量/协议声明 |
| `capabilities/server/internal/construction/manifest.go` | `capabilities/server/studio/build/build_owner.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CheckManifest`、`CheckSubscriptions` |
| `capabilities/server/internal/construction/marking.go` | `capabilities/server/studio/build/build_constraints.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`MarkingRank`、`HigherMarking` |
| `capabilities/server/internal/construction/match.go` | `capabilities/server/studio/build/build_integrations.go`；`capabilities/server/engine/integrations/integration_transforms.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`Match`、`MatchKey`、`KeyOf`、`ValueNormalize` |
| `capabilities/server/internal/construction/object_check.go` | `capabilities/server/studio/build/build_objects.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CheckEnterpriseUse`、`CheckFields` |
| `capabilities/server/internal/construction/object_shape.go` | `capabilities/server/studio/build/build_objects.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CompileObjectShape`、`goType` |
| `capabilities/server/internal/construction/observation.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Observation` |
| `capabilities/server/internal/construction/page.go` | `capabilities/server/studio/build/build_pages.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；常量/协议声明 |
| `capabilities/server/internal/construction/pipeline.go` | `capabilities/server/studio/build/build_integrations.go`；`capabilities/server/engine/integrations/integration_transforms.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`Pipeline`、`Step`、`Due`、`Execute` |
| `capabilities/server/internal/construction/pipeline_enterprise.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`EnterpriseTarget`、`Check`、`Value` |
| `capabilities/server/internal/construction/posts.go` | `capabilities/server/studio/build/build_transactions.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Post`、`ValueNumber` |
| `capabilities/server/internal/construction/process.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Process`、`ProcessLane` |
| `capabilities/server/internal/construction/process_check.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ProcessLookup`、`CheckProcess`、`processPaths` |
| `capabilities/server/internal/construction/process_compile.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ProcessExecution`、`CompileProcess`、`predicateSubject` |
| `capabilities/server/internal/construction/process_decode.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`UnmarshalJSON` |
| `capabilities/server/internal/construction/process_scopes.go` | `capabilities/server/studio/build/build_processes.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CheckProcessScopes`、`predicateBindings` |
| `capabilities/server/internal/construction/property_type.go` | `capabilities/server/studio/build/build_properties.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；常量/协议声明 |
| `capabilities/server/internal/construction/publication.go` | `capabilities/server/studio/build/build_artifacts.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`ReadPublication` |
| `capabilities/server/internal/construction/query.go` | `capabilities/server/studio/build/build_queries.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；常量/协议声明 |
| `capabilities/server/internal/construction/release.go` | `capabilities/server/studio/build/build_release.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`CompatibleObjectExpansion` |
| `capabilities/server/internal/construction/source.go` | `capabilities/server/studio/build/build_integrations.go`；`capabilities/server/engine/integrations/integration_sources.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`Source`、`SourceField`、`Due`、`Decode` |
| `capabilities/server/internal/construction/table.go` | `capabilities/server/studio/build/build_compute.go`；`capabilities/server/engine/business/business_rule_tables.go` | 同一声明/编译owner保留，真正运行/纯运算归原engine；共享平台契约，不复制执行或去掉安全投影；`Table`、`TableColumn`、`ValueColumnSchema`、`Definition` |
| `capabilities/server/internal/construction/testplan.go` | `capabilities/server/studio/build/build_tests.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`TestPlan`、`EvaluationPolicy` |
| `capabilities/server/internal/construction/writeback.go` | `capabilities/server/studio/build/build_integrations.go` | 原Build声明、编译/校验与发布同一owner直接共享类型；删除中间转换及旧路径；`Writeback`、`WritebackAns`、`Schema`、`Request` |

## 9. internal/host 15个文件

| 现有精确路径 | 合并/保留目标 | 删除前必须保留的职责及核对入口 |
|---|---|---|
| `capabilities/server/internal/host/agents.go` | `capabilities/server/platform/ai.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`AgentObservation`、`DynamicAgentDefinitions` |
| `capabilities/server/internal/host/blueprint.go` | `capabilities/server/platform/asset_lifecycle.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`BlueprintCatalog` |
| `capabilities/server/internal/host/books.go` | `capabilities/server/platform/transaction.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`BooksChecks` |
| `capabilities/server/internal/host/build_definitions.go` | `capabilities/server/platform/asset_lifecycle.go`；`capabilities/server/platform/definition.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`ConstructionDrafts`、`ConstructionDraftReader` |
| `capabilities/server/internal/host/confirmation.go` | `capabilities/server/platform/protocol.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`ConfirmationHost` |
| `capabilities/server/internal/host/consumer.go` | `capabilities/server/platform/app.go`；`capabilities/server/platform/definition.go`；`capabilities/server/platform/files.go`；`capabilities/server/platform/ai.go`；`capabilities/server/platform/protocol.go`；`capabilities/server/composition/runtime_wiring.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`BuildHost`、`RecordSetHost`、`Attach` |
| `capabilities/server/internal/host/credentials.go` | `capabilities/server/platform/credential.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`Credentials`、`CredentialState` |
| `capabilities/server/internal/host/dispatch.go` | `capabilities/server/platform/protocol.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`DispatchHost` |
| `capabilities/server/internal/host/enterprise.go` | `capabilities/server/platform/org.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`EnterpriseQueries` |
| `capabilities/server/internal/host/flow.go` | `capabilities/server/platform/flow.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`FlowHost`、`AttachedFlow` |
| `capabilities/server/internal/host/host.go` | `capabilities/server/platform/app.go`；`capabilities/server/platform/flow.go`；`capabilities/server/platform/work.go`；`capabilities/server/platform/org.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`Host`、`FlowBinding` |
| `capabilities/server/internal/host/identity.go` | `capabilities/server/platform/app.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`CompositionReady`、`IdentityDirectory` |
| `capabilities/server/internal/host/system.go` | `capabilities/server/platform/snapshot.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`SystemBuildRequest`、`SystemBuildResult` |
| `capabilities/server/internal/host/transaction.go` | `capabilities/server/platform/transaction.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`TransactionHost` |
| `capabilities/server/internal/host/work.go` | `capabilities/server/platform/work.go` | 保留实际消费者的窄typed角色；删聚合Host/纯转发，所有Attach分派改归composition；`WorkHost`、`AttachedWork` |

## 10. 其余120个生产文件（保持同一分母）

| 现有精确路径 | 合并/保留目标 | 处理及责任 |
|---|---|---|
| `capabilities/server/apps/ai/accepted_state.go` | `capabilities/server/apps/ai/accepted_state.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/ai/ai.go` | `capabilities/server/apps/ai/ai.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/core/books.go` | `capabilities/server/apps/core/books.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/core/core.go` | `capabilities/server/apps/core/core.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/enterprise/accepted_state.go` | `capabilities/server/engine/organization/organization_state.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/enterprise.go` | `capabilities/server/engine/organization/organization_owner.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/model.go` | `capabilities/server/engine/organization/organization_model.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/query.go` | `capabilities/server/engine/organization/organization_queries.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/relationship_decisions.go` | `capabilities/server/engine/organization/organization_relations.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/sdk.go` | `capabilities/server/engine/organization/organization_queries.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/enterprise/view_reads.go` | `capabilities/server/engine/organization/organization_queries.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/apps/files/files.go` | `capabilities/server/apps/files/files.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/files/regions.go` | `capabilities/server/apps/files/regions.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/flow/continuous.go` | `capabilities/server/engine/automation/automation_continuous.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_artifacts.go` | `capabilities/server/engine/automation/automation_continuous_lifecycle.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_dead_letters.go` | `capabilities/server/engine/automation/automation_continuous_lifecycle.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_kernel.go` | `capabilities/server/engine/automation/automation_continuous.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_operators.go` | `capabilities/server/engine/automation/automation_continuous.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_prepared.go` | `capabilities/server/engine/automation/automation_continuous_lifecycle.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_retention.go` | `capabilities/server/engine/automation/automation_continuous_lifecycle.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_window.go` | `capabilities/server/engine/automation/automation_continuous.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/continuous_workers.go` | `capabilities/server/engine/automation/automation_continuous.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/engine.go` | `capabilities/server/engine/automation/automation_engine.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/flow.go` | `capabilities/server/engine/automation/automation_owner.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/release.go` | `capabilities/server/engine/automation/automation_owner.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/flow/schedule.go` | `capabilities/server/engine/automation/automation_engine.go` | 原Flow owner搬迁合并，定义/在途/持续处理语义不变 |
| `capabilities/server/apps/knowledge/knowledge.go` | `capabilities/server/apps/knowledge/knowledge.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/relations/relations.go` | `capabilities/server/apps/relations/relations.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/system/definitions.go` | `capabilities/server/apps/system/definitions.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/system/system.go` | `capabilities/server/apps/system/system.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/work/policy.go` | `capabilities/server/apps/work/policy.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/work/redirect.go` | `capabilities/server/apps/work/redirect.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/work/refusal.go` | `capabilities/server/apps/work/refusal.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/work/retirement.go` | `capabilities/server/apps/work/retirement.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/apps/work/work.go` | `capabilities/server/apps/work/work.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/api-types/main.go` | `capabilities/server/cmd/api-types/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/check-boundaries/main.go` | `capabilities/server/cmd/check-boundaries/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/code-builder/main.go` | `capabilities/server/cmd/code-builder/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/install-bundle/main.go` | `capabilities/server/cmd/install-bundle/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/platform-server/main.go` | `capabilities/server/cmd/platform-server/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/platform-updater/main.go` | `capabilities/server/cmd/platform-updater/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/platform-updater/serve.go` | `capabilities/server/cmd/platform-updater/serve.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/rule-browser/main.go` | `capabilities/server/cmd/rule-browser/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/storage-files/main.go` | `capabilities/server/cmd/storage-files/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/uaf-gen/main.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/cmd/wasm-worker/main.go` | `capabilities/server/cmd/wasm-worker/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/cmd/webhook-sink/main.go` | `capabilities/server/cmd/webhook-sink/main.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/composition/tenant.go` | `capabilities/server/composition/tenant.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/idp/local.go` | `capabilities/server/idp/local.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/idp/oidc.go` | `capabilities/server/idp/oidc.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/acceptance/json.go` | `capabilities/server/kernel/acceptedformat/format_envelope.go` | 原接受封套与字节预算归kernel，不新建codec |
| `capabilities/server/internal/acceptance/result/batch.go` | `capabilities/server/kernel/acceptedformat/format_batch.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/change_plan.go` | `capabilities/server/kernel/acceptedformat/format_evidence.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/common.go` | `capabilities/server/kernel/acceptedformat/format_evidence.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/effect.go` | `capabilities/server/kernel/acceptedformat/format_effect.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/effect_transition.go` | `capabilities/server/kernel/acceptedformat/format_effect_transition.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/evidence.go` | `capabilities/server/kernel/acceptedformat/format_evidence.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/input.go` | `capabilities/server/kernel/acceptedformat/format_input.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/record.go` | `capabilities/server/kernel/acceptedformat/format_record.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/refusal.go` | `capabilities/server/kernel/acceptedformat/format_refusal.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/release.go` | `capabilities/server/kernel/acceptedformat/format_release.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/usage.go` | `capabilities/server/kernel/acceptedformat/format_usage.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/acceptance/result/validation.go` | `capabilities/server/kernel/acceptedformat/format_evidence.go` | 原版本化接受格式/七结果族保留；并文件不合并语义或重写历史 |
| `capabilities/server/internal/agents/json.go` | `capabilities/server/internal/agents/json.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/agents/prompt.go` | `capabilities/server/internal/agents/prompt.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/aistate/state.go` | `capabilities/server/internal/aistate/state.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/enterprise/discovery.go` | `capabilities/server/engine/organization/organization_queries.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/legacy_views.go` | `capabilities/server/engine/organization/organization_state.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/lifecycle.go` | `capabilities/server/engine/organization/organization_model.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/model.go` | `capabilities/server/engine/organization/organization_model.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/profile.go` | `capabilities/server/engine/organization/organization_owner.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/relations.go` | `capabilities/server/engine/organization/organization_relations.go` | 同一enterprise权威内聚，必要生命周期/端点/属性与历史保留，见自举清单 |
| `capabilities/server/internal/enterprise/uaf/data.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/internal/enterprise/uaf/releases.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/internal/enterprise/uaf/uaf.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/internal/enterprise/uafgen/generate.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/internal/enterprise/uafgen/profile.go` | `外部可选工具（精确路径见platform-self-hosting/design.md）` | 完整UAF/生成器外移；只计6个生产Go路径退出，不把生成JSON/XMI当手写LOC |
| `capabilities/server/internal/records/access.go` | `capabilities/server/internal/records/access.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/fields.go` | `capabilities/server/internal/records/fields.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/history.go` | `capabilities/server/internal/records/history.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/index.go` | `capabilities/server/internal/records/index.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/query.go` | `capabilities/server/internal/records/query.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/recent.go` | `capabilities/server/internal/records/recent.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/references.go` | `capabilities/server/internal/records/references.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/row.go` | `capabilities/server/internal/records/row.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/rows.go` | `capabilities/server/internal/records/rows.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/unique.go` | `capabilities/server/internal/records/unique.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/validation.go` | `capabilities/server/internal/records/validation.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/values.go` | `capabilities/server/internal/records/values.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/records/write.go` | `capabilities/server/internal/records/write.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/aggregate.go` | `capabilities/server/internal/rules/aggregate.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/compare.go` | `capabilities/server/internal/rules/compare.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/compile.go` | `capabilities/server/internal/rules/compile.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/eval.go` | `capabilities/server/internal/rules/eval.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/model.go` | `capabilities/server/internal/rules/model.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/page.go` | `capabilities/server/internal/rules/page.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/value.go` | `capabilities/server/internal/rules/value.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/rules/window.go` | `capabilities/server/internal/rules/window.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/sink/sink.go` | `capabilities/server/internal/sink/sink.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/systembuild/source.go` | `capabilities/server/internal/systembuild/source.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/systembuild/systembuild.go` | `capabilities/server/internal/systembuild/systembuild.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/internal/systemscope/systemscope.go` | `capabilities/server/internal/systemscope/systemscope.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/file.go` | `capabilities/server/journal/file.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/journal.go` | `capabilities/server/journal/journal.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/memory.go` | `capabilities/server/journal/memory.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres.go` | `capabilities/server/journal/postgres.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_checkpoint.go` | `capabilities/server/journal/postgres_checkpoint.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_read_queries.go` | `capabilities/server/journal/postgres_read_queries.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_read_view.go` | `capabilities/server/journal/postgres_read_view.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_snapshot_state.go` | `capabilities/server/journal/postgres_snapshot_state.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_state.go` | `capabilities/server/journal/postgres_state.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_state_upgrade.go` | `capabilities/server/journal/postgres_state_upgrade.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_unique.go` | `capabilities/server/journal/postgres_unique.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_value_selection.go` | `capabilities/server/journal/postgres_value_selection.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/postgres_vector.go` | `capabilities/server/journal/postgres_vector.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/read_position.go` | `capabilities/server/journal/read_position.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/state.go` | `capabilities/server/journal/state.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/state_replacement.go` | `capabilities/server/journal/state_replacement.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/journal/vector.go` | `capabilities/server/journal/vector.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |
| `capabilities/server/platform/authz/authz.go` | `capabilities/server/platform/authz/authz.go` | 现有独立owner/纯基础包或隔离命令保留；本轮不为配额移动或删除 |

## 11. 特别不能被并文件抹掉的边界

- 接受23→3只减少20个运行协调文件路径。七类结果及原host/agent/work等专属格式继续由版本化codec判定；callback行、批次、控制状态、通知、唯一键、发布安装、SQL delta、未知回执和旧输入都要在新结构中有明确位置。
- `internal/host`有用角色迁入平台已有角色文件，不是物理删除协议。聚合Host和Attach分派不进入公共超级接口；初始装配、隔离试用与私有fork的重接线均由composition提供的typed工厂完成，kernel不类型断言具体应用再调用Attach。
- studio/build内聚相同草稿声明、编译和发布owner；真正的运行变换/事务执行仍归engine。逻辑发布描述必须剥离本地地址、凭据和运行状态的投影继续存在，不能为“零转换”把敏感或可变字段封进制品。
- 表中基础命令main.go、原独立owner与state.go等真实不同职责仍需守门基线约束；不声称82组全部无价值。迁移新文件优先用职责名称，清除原重复项时同步缩减baseline，不扩白名单允许同名镜像。
- 测试文件不计入679→339，但测试必须随owner迁移并保持断言；安全/历史测试不能因为旧实现退出就直接删除。生成物统一由原工具重生；生产数量、源码LOC、生成体量和工具外移分别报告。
