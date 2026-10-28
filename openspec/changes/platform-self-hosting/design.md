# Design

## Context

负责人已从审计与规划进入直接实施，当前完成状态见tasks。原自举已存在，不能把目标收缩写成它从未实现，也不能把旧勾选任务当作新边界已生效。当前产品意图仍是业务应用构建平台；附件新增“确定性优先、静态控制面、UAF 外移”的方向，本 change 只落实战役四，结构拍平、端侧规则和 AI 网关由 `consolidate-platform-boundaries` 管理。

### 已核对的实现与风险

| 事实 | 当前来源 | 判断 |
|---|---|---|
| 唯一 system 租户与系统角色 | `capabilities/server/tenants.go` 的 `systemSpec`；`composition/tenant.go` 只在 system 装配 `apps/system` | 是真实控制权，不是静态文档 |
| 源码、候选、批准与激活 | `apps/system/system.go` 的 file/blob/change/release/activation，`apps/system/definitions.go`，宿主 `system_definitions.go` | 已实现全量源码及定义自举，首批实施须关闭新写入口 |
| AI 真实写入口 | `system.architect` 工具包含文件读取、保存与提案；其批准/激活被原 owner 拒绝 | 不能靠撤掉导航认为 AI/API 入口已关闭 |
| 实际前端 | `web/packages/govern/src/system.tsx`，原静态注册在 `govern/src/index.tsx` | 旧 design 的 `web/packages/platform/src/system.tsx` 不存在，旧体量快照过时 |
| 构建与监督 | `cmd/platform-updater/main.go`、`serve.go`，`internal/systembuild/{source,systembuild}.go` | 固定构建、制品留存、健康、排空、回滚和未知回执续接均真实存在 |
| 实际恢复物 | `.build/system/{sources,bin,web,releases,builds}` 与 `current.json`；原 system 接受映像、候选字节 | 冻结不能抹去这些来源与未完成责任 |
| 闸门覆盖缺口 | `internal/systemscope/systemscope.go` 的冻结表未覆盖 `tenants.go`、`credentials.go`、`model_configuration.go`、服务 main、自举 owner/构建器及 Web 治理源码 | 已证实的风险；没有证明发生过控制面锁死。源码后缀拒绝秘密文件不等于冻结凭据管理代码 |
| 历史恢复边界 | `runtime/storage`、原 owner 严格映像校验；`enterprise/accepted_state.go` 及 `legacy_views.go` | 新封存结果与旧输入分别承担责任；不能删字段、换 owner 或重算旧决定 |

### UAF 体量与用途（2026-10-27 工作树）

| 范围 | 文件数 | 字节 |
|---|---:|---:|
| `capabilities/server/internal/enterprise/uaf/` | 7 | 1,522,419 |
| `capabilities/server/internal/enterprise/uafgen/` | 3 | 9,339 |
| `capabilities/server/cmd/uaf-gen/` | 1 | 1,809 |
| `docs/standards/uaf/` | 2 | 97,728 |
| 上述外移候选合计 | 13 | 1,631,295 |
| 其中两份 XMI | 2 | 1,298,843 |
| 其中实际嵌入的 JSON | 1 | 212,883 |
| Enterprise owner（不含 UAF/生成器） | 19 | 153,710 |

UAF 数据含 272 个构造型、21 个枚举和 12 个域。`go list -deps ./cmd/platform-server` 确认主程序依赖 `internal/enterprise/uaf`，不依赖 `uafgen`；`encoding/xml` 仍可能由其他能力引入。XMI 只在生成阶段解析，运行读取使用 `sync.Once` 解码的生成 JSON。没有做编译耗时、二进制差值或内存基准，不能将 1.63 MB 仓库体量当作运行占用或性能损失。

UAF 当前参与元素类别及继承、关系端点、属性/枚举/引用校验、关闭/删除时的引用检查、外部切片命名空间及 SDK/建模器发现。它不是无用文件：退出须将轻量组织必需的确定性规则迁回原 owner，并显式停止完整建模的新写与导入。

## Goals / Non-Goals

**Goals:** 动态运营记录与静态控制面之间形成可检查边界；原全量自举先冻结新工作再交接保留责任；UAF 从默认运行、构建和生成依赖退出，原组织权威继续服务有效期与授权范围；删除和合并清单可直接核对路径。

**Non-Goals:** 本轮不关闭业务入口、不移动实现、不触碰运行数据或密钥；不重建 tenant/enterprise 身份；不清理日志和制品；不删 Knowledge、集成、专业可视化及现役应用包；不把台账、Flow 或蒸馏另实现到 Enterprise；不继续完善旧源码自举的缺口。

## Decisions

### 1. 动态面只接受原 owner 的运营读取与追加

审计、模型用量和工作监控从各自 owner 投影，缺失、受限和失败按原读取结果呈现；不是新建可编辑副本。追加记录绑定原事件、主体及幂等身份，经同一接受结果保存，已有事实不允许编辑/删除。蒸馏样本只引用 AI 网关负责生成、脱敏和授权的原样本，不在此再做配对、调度或存储。

动态页面只能选择经静态注册明确声明为只读/追加的运营动作；不能通过通用 ActionTrigger、直接 schema、源码变更、包贡献或 Agent 工具调用禁区。任务监控的重试、取消、排空仍在静态 owner 入口，不因看见任务行就获得控制权。显示脱敏凭据状态不等于允许动态管理凭据。

### 2. 静态控制面是启动与修复的前提，不是应用定义

禁区涵盖租户开通/停用、IdP/登录、成员与授权管理、凭据托管、模型/provider/默认用途/路由、基础实现选择、健康/隔离/恢复和部署选择。它们继续使用原权威、原权限及原接受结果，UI 在受信静态构建中注册；静态不等于去掉权限或直接写数据库。

实现批次先关闭 system 文件保存、源码/定义提案、批准、构建与新激活动作；同时停止 Agent 工具与 updater watch 接入新工作。没有默认“专家开关”重新打开全量自举。后续如需重新引入，应作为新的明确授权变化审阅。

现有健康、宿主维护与登录路径不依赖动态 Page 定义，实施时必须实证保持这种独立性：构造动态元数据损坏/缺失、system 租户隔离的环境，仍能通过原静态身份、维护和健康入口诊断。共享进程未隔离、数据库不可用、身份服务本身宕机不在此保证内，沿原部署故障模式明确报告。

### 3. 旧自举状态冻结，不销毁、不换权威

保留原 `system` 身份、封存字节、ledger/安装映像解码及只读诊断。已经开始且结果未知的激活，先以原 ID/原键核对；已关闭的新写入口不能把原已持久结果重试当作新动作，也不能悄悄生成另一激活。未开始的提案不再触发构建/激活；已请求但未完成的工作按原证据在静态维护中明确终结，不能只改 UI 状态。

独立部署/监督继续选择原发布、探活、排空和保留回滚前驱；自动消费新动态变更的 watch 路径退出。不能在没有完成实际服务版本、资源、未回报结果和恢复责任交接前删除 updater、`current.json` 或归属方代码。关闭的是动态控制权，不是版本发布与故障维护本身。

### 4. UAF 工具外移，组织 owner 只保留必要语义

目标包为 `capabilities/server/engine/organization/`，移动的是现有 `enterprise` 权威实现，authority ID、记录身份、接受映像和租户组织引用保持原值；不新增 organization 权威或第二目录。目标生产文件为 `organization_owner.go`、`organization_model.go`、`organization_relations.go`、`organization_queries.go`、`organization_state.go`；公开组织事实沿既有 `platform/org.go`，移除跨包别名传话筒。其他三类目录遵循合并变更：kernel 负责接受/持久恢复，studio 负责设计发布，aigateway 负责模型阶段、预算和蒸馏。

保留轻量单位/岗位/任职、指定结构、有界上下级、有效期间、已存引用解析、关闭/重开与来源所有权检查。组织类型与关系端点成为有界平台目录规则，不复制 272 构造型全集；普通业务台账和流转仍交原 Build/Flow owner。

新写不再接受任意 UAF 类别、标准属性、视图或完整切片导入。历史已保存的资源、场所、能力、视图及属性保留原 ID 与字节，通过原 owner 的只读历史解释可辨认；不能全部强转为单位、丢弃未知属性，或把旧来源改成本地。已封存结果只应用原映像；需要原 UAF 解释器重建的旧输入在独立保留的兼容二进制完成原恢复和受检交接前阻止升级，不把外移工具自动挂回普通运行路径。

### 5. 精确文件删除、合并与目录收敛对照

下列是统一实施目标，原组织owner已按表内聚；UAF标准与工具尚未外移。外部导入工具的仓库名 `platform-uaf-importer` 是交付目标名，不在本轮创建仓库；转移须保留原来源文件、校验摘要、许可证及版本。

#### UAF 从核心外移的 13 个文件

| 当前精确文件 | 目标（可选外部工具仓库内） | 核心处理 |
|---|---|---|
| `capabilities/server/internal/enterprise/uaf/data.go` | `platform-uaf-importer/uaf/data.go` | 外移后删除 |
| `capabilities/server/internal/enterprise/uaf/uaf.go` | `platform-uaf-importer/uaf/uaf.go` | 外移完整标准模型，核心不保留导入 |
| `capabilities/server/internal/enterprise/uaf/releases.go` | `platform-uaf-importer/uaf/releases.go` | 撤掉核心 JSON embed |
| `capabilities/server/internal/enterprise/uaf/uaf_test.go` | `platform-uaf-importer/uaf/uaf_test.go` | 标准测试随工具走 |
| `capabilities/server/internal/enterprise/uaf/gen/uaf-1.3.json` | `platform-uaf-importer/uaf/gen/uaf-1.3.json` | 原生成字节保留 |
| `capabilities/server/internal/enterprise/uaf/spec/uaf-1.3-profile.xmi` | `platform-uaf-importer/spec/uaf-1.3-profile.xmi` | 原件与摘要保留 |
| `capabilities/server/internal/enterprise/uaf/spec/uaf-1.3-measurements.xmi` | `platform-uaf-importer/spec/uaf-1.3-measurements.xmi` | 原件与摘要保留 |
| `capabilities/server/internal/enterprise/uafgen/generate.go` | `platform-uaf-importer/generate/generate.go` | 生成职责外移 |
| `capabilities/server/internal/enterprise/uafgen/profile.go` | `platform-uaf-importer/generate/profile.go` | XML 解析职责外移 |
| `capabilities/server/internal/enterprise/uafgen/generate_test.go` | `platform-uaf-importer/generate/generate_test.go` | 来源/生成一致性验证随工具走 |
| `capabilities/server/cmd/uaf-gen/main.go` | `platform-uaf-importer/cmd/uaf-gen/main.go` | 核心命令退出 |
| `docs/standards/uaf/1.3/SOURCE.md` | `platform-uaf-importer/docs/1.3/SOURCE.md` | 修正外部仓库内路径，保留来源 |
| `docs/standards/uaf/1.3/uaf-1.3-specification.html` | `platform-uaf-importer/docs/1.3/uaf-1.3-specification.html` | 标准说明外移 |

#### 组织能力内聚的逐文件映射

下表 target 均以 `capabilities/server/engine/organization/` 为根；旧目录仅在全部消费者改接、历史责任验证后删除，不留纯转发壳。

| 当前精确文件 | 目标文件 | 必须保留或退出的真实责任 |
|---|---|---|
| `internal/enterprise/model.go` | `organization_model.go`、`organization_state.go` | 组织/任职/关系身份与有效期；旧 UAF 扩展字段留历史读取 |
| `internal/enterprise/lifecycle.go` | `organization_model.go`、`organization_relations.go`、`organization_state.go` | 关闭/重开、期间覆盖、引用占用保留；完整 UAF 属性重写和新切片导入退出 |
| `internal/enterprise/relations.go` | `organization_relations.go` | 有界目录端点/角色规则保留；全 UAF 契约推导外移 |
| `internal/enterprise/discovery.go` | `organization_queries.go`、`platform/org.go` | 保留目录查询 DTO；标准 metamodel/profile/contracts 发现退出 |
| `internal/enterprise/profile.go` | `organization_owner.go` | 仅保留轻量组织类别的显示声明；五类完整建模视角与全集 palette 退出 |
| `internal/enterprise/legacy_views.go` | `organization_state.go` | 旧 grid/viewpoint 映像解释保留，普通视图写入退出 |
| `internal/enterprise/model_test.go` | `organization_model_test.go`、`organization_relations_test.go` | 原身份/时间/关系与授权事实断言 |
| `internal/enterprise/lifecycle_test.go` | `organization_model_test.go`、`organization_state_test.go` | 原有效期、关闭及旧历史断言 |
| `apps/enterprise/enterprise.go` | `organization_owner.go`、`organization_model.go`、`organization_queries.go` | 原 enterprise 决定、Units/Below/Holders/Calendar；必要属性与端点校验内聚，不能直接删掉 |
| `apps/enterprise/accepted_state.go` | `organization_state.go` | 严格解码、身份重复检查、保存/恢复原 owner 状态 |
| `apps/enterprise/model.go` | 移除纯别名，实际类型归 `organization_model.go` / `platform/org.go` | 不建立新的同名转发层 |
| `apps/enterprise/query.go` | `organization_queries.go` | 有界查询、已关闭引用解析、稳定排序保留 |
| `apps/enterprise/relationship_decisions.go` | `organization_relations.go` | 原期间、端点、结构/任职检查保留；新全量切片及标准关系写入退出 |
| `apps/enterprise/sdk.go` | `organization_queries.go` | 目录语义从原声明产生；UAF palette/标准SDK发现退出 |
| `apps/enterprise/view_reads.go` | `organization_queries.go`、`organization_state.go` | 旧视图仅保留历史读取；新完整建模入口退出 |
| `apps/enterprise/enterprise_test.go` | `organization_model_test.go`、`organization_state_test.go` | 原 owner 决定及恢复证据，不改断言掩盖差异 |
| `apps/enterprise/accepted_state_test.go` | `organization_state_test.go` | 深拷贝、原映像及未知字段/格式拒绝 |
| `apps/enterprise/lifecycle_test.go` | `organization_model_test.go`、`organization_relations_test.go` | 生命周期和有界关系验证 |
| `apps/enterprise/query_test.go` | `organization_queries_test.go` | 当前/历史引用与查询范围验证 |

#### 同批必须改接的入口与消费者

- `Makefile`：`check-go` 不再依赖 `check-uaf`；`generate-uaf/check-uaf` 随外部工具外移。`cmd/README.md`、Architecture 与主规范在实现完成后同步，不在本轮误写为当前已退出。
- `composition/tenant.go`：装配迁移后的同一 enterprise owner；`enterprise_usage.go`、`enterprise_usage_history.go`、`enterprise_links.go`、`api.go`、`pipelines.go`、`routes_records.go`、`live_queries.go`、`internal/host/enterprise.go` 的类型和读取由战役一共同改接；先逐一核对实际文件/角色，后者随 internal/host 整目录退出，不另立桥接接口。
- `web/packages/govern/src/enterprise/{index.tsx,model.ts,properties.tsx,lifecycle.tsx,canvas.tsx,TenantLinks.tsx,model.test.mjs}`：目录操作与历史解析内聚至既有组织界面；完整 UAF 属性/元模型、画布、视图及切片编辑退出。`web/packages/ui/src/records/RecordLookup.tsx`、`web/packages/app/src/semantic/Selector.tsx`、`web/packages/build/src/ontology/process.tsx` 及生成 `host.ts` / `enterprise-sdk.ts` 的引用协议与当前权限保留，生成物由原工具重新生成，实施前重查调用方。
- `apps/system/{system.go,definitions.go}`、宿主 `system_definitions.go`、`apps/build/system_definitions.go`、`platform/{system_definitions.go,system_source.go}`、`internal/systemscope/`：先拒绝新全量自举，保留原封存/恢复解码，不立即整目录删除。
- `cmd/platform-updater/{main.go,serve.go,updater_test.go}`、`internal/systembuild/{source.go,systembuild.go,source_test.go}`、`internal/host/system.go`：维护/恢复保留部分改归部署静态工具，动态构建订阅退出；只有核清实际调用方和历史后才能删除被替代实现。
- `web/packages/govern/src/{index.tsx,system.tsx,system.test.mjs,host.tsx,credentials.tsx,ai-configuration.tsx}`：完整自举写控件退出，原宿主、凭据、模型治理保持静态注册；`tenants.go`、`idp/`、`credentials.go`、`model_configuration.go`、`host_control.go`、`deploy_recovery.go` 及服务 main 全部属于静态控制面禁区。

## Risks / Trade-offs

- 删除 UAF 会改变现役校验和发现协议 → 先以原 owner 的组织、端点、属性、有效期与历史样本比较接受/拒绝；无法解释的旧输入阻止升级并保留兼容二进制，不默默放行。
- 关闭源码入口但尚有未完成激活 → 首批任务列出实际 pending/unknown 状态，沿原 ID 核对并交接监督责任；不能删除 `current.json` 解除阻塞。
- 静态控制台与动态工作区共享 JavaScript/进程 → 验证不依赖动态元数据，但不夸大为操作系统级隔离；失败必须落到部署维护路径。
- 当前边界检查只能限制继续扩散 → 本轮 guard 基线通过不等于运行时路径已关闭；新边界均保留未完成任务。
- 包含敏感来源的运营/蒸馏内容 → 当前权限与原来源继续过滤，禁止将凭据、思考原文或不可读业务内容复制到普通运营记录。

## Migration Plan

1. 首批实施静态禁区与全量自举新写冻结；提供仍可登录、查看健康、维护、恢复的静态路线，原业务运行不变。
2. 核对并终结/交接既有激活和未知回执，保留原 system 记录、候选、源码、制品及回滚前驱；部署不再自动消费新提案。
3. 按原 owner 迁移轻量组织规则和历史解释，再改消费者、SDK/生成物和界面；将完整 UAF 工具、标准和测试一起外移，默认构建去依赖。
4. 引入有限运营投影与追加声明，按 owner 验证权限、幂等及重放；蒸馏只接 AI 战役已交付的样本能力。
5. 运行针对性检查、真实 PostgreSQL 恢复和元数据故障演练；核对实际版本/资源/数据后，再同步 Architecture/主规范。任一步未通过保留上个兼容部署，不能用动态自举重新打开禁区作为回退。
