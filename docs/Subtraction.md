# 旧实现的替代与退出依赖

ADR-0070–0075 提供了 Connection / Source / Dataset / Pipeline / Writeback / Matching rule。下面记录旧路径的处置与依赖；制造方案的 `erpadapter` 与调用方已一起删除，现有摄取与回写沿原集成归属。沙箱的独立 Go 模块使用 AGENTS 指定的 `scripts/sandbox-go.sh` 装配后检查；删除与调用方迁移在同一块完成，按实际影响选择 Makefile 检查。这里不是第二份施工队列，当前顺序只归 WorkQueue，整个项目的删除原则见 ADR-0124。

已完成章节中的路径与操作说明记录当时实际删除的内容，不是要求重复执行；未退出实现按 ADR-0114 的等价替代与历史责任判断。

## 1. `apps/erpadapter` + `web/packages/erpadapter`（已完成 2026-10-08）

被取代：自配凭据的 ERP 轮询 → `build.connection` + `build.source`(odata/json)；`erpadapter/confirmation` 效果 → `build.writeback`（连接即端点、裁决 change id 即幂等键）。相关模块、席位与引用已全量清理。

删除：
- `apps/erpadapter/`（整个模块）、`web/packages/erpadapter/`；
- `go.work` 中的 `./apps/erpadapter` 条目；`web/pnpm-workspace` 自动收缩，`web/apps/workspace/package.json` 去掉 `"@pkg/erpadapter"`，`web/apps/workspace/src/host/packages.ts` 去掉 `{ serves: ["erpadapter"] … }` 一行，然后 `corepack pnpm install` 刷新 lockfile；
- `web/apps/catalog/src/gen/catalog.json` 由生成脚本重生成（不手改）。

随手改：
- `solutions/manufacturing/cmd/manufacturing-server/main.go`：删 `"erpadapter"` 导入、`erpadapter.ID: erpadapter.Planner` 席位角色、`manufacturing.Seat("erp", "erp", …Connector)` 席位；`*books` 开关只剩 `erp.New`。
- `solutions/manufacturing/external_test.go`：同上三处 + `NewTenant(tenant, erpadapter.New(tenant), …)` 改为不带 adapter 的组合（这个测试本身在验证"外部席位只能走 adapter"，adapter 没了就删掉该测试）。
- `solutions/manufacturing/manufacturing.go:31` 注释里的 `erpadapter.New` 字样；`apps/mes/server/mes_test.go:340` 注释。
- `capabilities/server/effects.go:45` 的示例改成 `"build/writeback"`。
- `deploy/local/README.md`、ADR-0024/0025 里的提及改为"历史"措辞或删句（ADR 不改决定，只补一句"已由 ADR-0072 取代"）。

验收：探针第 3/5 步改用 `build.writeback`（Testing.md「集成织物 Ⅰ-E」）仍通过；`solutions/manufacturing` 编译、测试通过。

## 2. `apps/erp` 采购链中与 `core` 重叠的部分（ADR-0069 §Ⅱ 退出判据）

**现在不删**。Ⅱ 业务核落地（单据/凭证/账期）后，`apps/erp` 的 purchase-order → goods-receipt → posting 这条链成为第一个被 `core` 配置取代的对象；届时再开清单。

## 3. 其他已确认可删的小项

- `web/packages/build/src/ontology/data-source.tsx` 中数据源的 `object/key/mapping` 直写分支在数据集目标普及后可收（ADR-0071 §减法）：等探针三源都改为 dataset 目标后，删掉"直接写对象"的 profile 分支与 `Source.MapRows` 的对象路径；`applyRows` 只剩管道一个入口（匹配规则随之只有一个调用点）。
- `enterprise.slice.import`（ADR-0068 跨租户联邦）与 `enterprise.slice.sync`（ADR-0073）并列保留；两者形状一致，若联邦场景一年内未启用，收掉 import。

## 4. 构建器旧动作表达（ADR-0100，已替换）

新保存和供编辑的对象将 `Sets`、`Creates`、`Posts`、`Journal`、`Reverses`、`Conditions`、`Reads` 与 `To/ToInput` 转为 `Action.Transaction` 后移除原字段。`Object.Numbering` 转为 `CreateTransaction` 与命名序列，首次取号读取已有最大号码，接受后只用序列 owner。旧的分散动作编辑器及编号编辑器已删除，状态图也只读写事务。

旧 `Published`、日志与定义字节保持原样，旧发布当前执行在内存编译成同一个事务。原 `take/stored/create/post/numberingOf` 仅保留为历史 raw-input 日志的 `LegacyReplay` 输入适配，入口拒绝普通调用；不能删除它们而破坏已有日志恢复，退出条件是这些旧输入不再有恢复义务。已保存的接受结果只应用原映像。post 与新赋值复用唯一 Expr 引擎；逐步骤写入沿 `Ledger.ApplyTxnRecord` 调用原 `standard`，不另造授权、记录校验或出站队列。

## 5. 四套规则求值（ADR-0101，已替换）

旧公式 `ValueFormulaNode.Eval`、决策表条件 `ValueCondition.Holds`、动作 `take` 的比较分派及页面 TypeScript `operators` 已删除。旧字符串/单元格/三元组/页面算子图只作编译输入，保存为 Expr；数字稿件的格式化与原参考应用的领域规则不算语言求值器。未使用的前端 `decimalArithmetic` 已删除。冻结旧公式的数值语义在同一引擎内受限保留，新 Expr 不可指定兼容模式；退出条件是既有不可变旧公式不再有运行义务，历史字节始终保留。

## 6. 新模型的入口专属校验（ADR-0102，已收敛）

新对象的存储不变式归统一约束门禁，不在表单、CSV、事务、AI 或协议另写一份。CSV 的“空白单元格静默省略”已删除，空白表示清空并由同一门禁报告；预览复用私有决定试算，未增加预览执行器。字段输入语义保留在原 Required/InputConstraints；原生行业守卫与独立 relations 图分别随 ADR-0113/0114、ADR-0104 退出，旧定义/日志的读取与恢复义务继续保留。

## 7. 独立同权威关系图（ADR-0104，迁移中）

所有 owner 的 ad-hoc 同权威 link / unlink 新写已冻结，未声明关系存储不再保留写入例外；新关系只经声明的所有者事务。旧关系保持按原权限读取，通过显式迁移成为所有者记录，迁移标记让原边退出当前读取，原 owner-state 保留历史证据；不双写、不改历史字节。实际租户迁移仍须执行，原生行业应用的退出归 ADR-0114。跨应用关系、动态、评论和关注仍归 relations。

## 8. 行业私有版本与业务时间（ADR-0103，平台机制已提供）

新构建产物使用普通记录的 Versioning、固定映射和有效期查询，不另造版本对象、最新版查找器或时间模型。企业模型的半开生效期委托平台判断。MES 的 MasterData / DemoMaster 保留为接管前演示数据，其替换与删除归 ADR-0113 / ADR-0114；不把旧种子包装成新的产品主数据路径，也不重写旧发布或历史单据。

## 9. 运行与专用编辑器的重复会话（ADR-0124，已替换）

- `ApplicationRuns` 不再嵌入 `FlowInstanceView`、`RunView`、`OperationRunView`、`DispatchProgress`：选中项只显示依据与原详情入口，取消、重试和设备处理只在原正式详情执行，删除打开详情后同时存活的第二份读取和操作状态。
- `automate/workflow-runs.tsx` 的本地 `command()`、`decide/can`、忙碌/错误状态及取消/重试按钮已删除，正式操作复用 `FlowInstanceView` / `useRunControl`；额外 `setInterval` 删除，原 `useRecordInventory` 负责实时更新。历史版本画布、输入/已接受输出、token、步骤定位与运行覆盖保留。
- 应用 New 菜单不再为已使用专用编辑器的十三类资源初始化 `useNewRecord` 与通用对话框状态；原目录决定创建资格，原专用路由承担创建。仍使用通用表单的资源沿原表单，不增加第二套创建器。
- 知识文档和术语的手写新建按钮、`Dialog` / `GeneratedForm`、记录 ID、打开状态与提交回调删除，保留原 `Records` 的唯一声明动作入口；`NewActions` 的独立弹窗状态也删除，列表与菜单共用已有 `useNewRecord`。检索和术语影响说明保留，标准创建的拒绝、纠正和同键确认沿原动作会话。
- `widgets/FunctionAdvice.tsx` 整个删除，旧 `function` 部件改用已有 `AIWidget` / `createAIReader`，不再独立发起、读取、刷新或维护调用结果状态。旧部件种类与版本不变，仍使用页面所选记录；历史按记录、函数和版本筛选，直接打开正式调用详情，计量信息归该详情，采纳仍走原 `ProposalReview` 与审批事务。
- `ActionDialog` 的 `create()`、`request/createLock`、`outcome/refusal/issues/draft` 状态及第二份纠正判断删除；它只保留弹窗、自定义 ID 与子表单忙碌/等待通知。创建、编辑、声明参数和内联动作由原 `DeclaredActionForm` 的一份提交实现负责，原 K5 outbox、输入表单和拒绝呈现保留，未新增提交器或会话框架。
- `PublishMenu` 与 `ResourceControls` 的两份归档会话、对话框和返回计算合为唯一 `ResourceControls`，旧组件及调用退出；告警、决策表和观测源不再同时放两套控件。发布页面的已保存丢弃/归档只在原菜单，新建取消仍丢弃并离开，未提供发布回调的页面保留独立取消与归档。重复返回按钮删除，沿现有应用上下文导航；完整原路由、归档 revision、未保存保护和观测编辑权限保留。

检查沿 Testing 的应用运行任务与构建者验证任务：原正式详情取消、答复丢失后同键重试、编辑器历史查看及正常新建入口已通过；旧操作按钮明确不存在。Web 类型、目录及双语检查通过，不改执行、权限、持久格式或历史版本责任。

## 10. 资产的重复目录读取与安装解码（ADR-0124，已替换）

`Build.integrationInventory` 的七份分页读取、类型转换与归档过滤退出，改用原 `readDefinitionInventoryOf` 和一份归档过滤；500 条分页、1000 条上限和原 Build 读取主体不变。`DecodeAssetInstallation` 的独立七类分配/解引用分支删除，与安装校验、升级校验共用 `integrationInstallationRecord`；目录和解码只在 `integrationRecordType` 维护一次记录类型。严格解码、安装身份、保留版本族、描述与权限检查及保存图像字节继续保留，没有新注册表或历史格式。原 `asset_lifecycle_test.go` 检查七类记录、错误图像、跨页读取、归档过滤与数量上限；其余原安装、升级和恢复测试继续使用同一实现。

原生发布安装的十个重复身份解析分支和 `activation.go` 中仅供它调用的 `publicationRecord` 已删除。安装元数据只读一次，种类与名称使用原 `draftAssetRef`，发布资格使用原 `AcceptedPublicationSchemas`，不再维护第二份白名单。评测/测试计划、决策表和告警的专用内容/版本检查保留；外层记录名称、Object/Flow 前缀与克隆的原图像不变，实际发布、安装、历史恢复仍由原实现负责。

## 11. 构建者自制表格（ADR-0124，已替换）

页面变量底栏和决策表的手写 `<table>`、行列呈现与两项 UI 豁免删除，改用原 `DataTable`。公共表格修正分组表头跨度并容纳原 Input/Select；决策表保留原条件、输出、默认值、增删列行、重排和保存逻辑，不通过排序改变首个命中的优先级。变量底栏保留标签、类型和原 JSON 值。Lineage 的本地标题分派也删除，沿原 `definitionPresentation` 显示，权限与依赖读取不变。

## 12. 代码规模与本轮可删除空间（ADR-0124）

统计入口为 `make code-size REVISION=<提交>` 与 `make code-size`（Python 3、cloc 2.10）；按 Git 文件清单统计，包含新增文件，生产源码的空白和注释不计，测试、生成物、构建/示例、文档/配置/数据分开。JSON 配置不是代码删减，翻译字典仍作为手写生产源码计入。报告含逐文件数据，保存于 `.build/code-size*.json`，可重新生成。

比较起始提交 `0c715c6461097f35f43a0501cc837dcaca173c1b` 与 `bd1ed00a`，包含该次提交前各并行工作的实际成果，不把总量归给某一个代理。下表以同一统计工具重新计算，报告保存在 `.build/commit-and-prune/original.json` 与 `after.json`。

| 类别 | 修改前代码行 | 修改后代码行 | 变化 |
|---|---:|---:|---:|
| 手写生产源码 | 162,374 | 160,280 | 减少 2,094（1.29%） |
| 测试 | 64,040 | 64,651 | 增加 611 |
| 生成物 | 27,753 | 27,636 | 减少 117，不计生产删减 |
| 构建与示例 | 3,353 | 3,428 | 增加 75，含统计工具 |

生产源码物理行从 179,927 减为 177,824；新增的共有实现已计入。849 行净减少来自同值翻译去重，另外 1,245 行是其他生产源码净减少。未把挪文件、缩进压缩、测试删除或生成文件压缩当成成果。生产文件数从 1,209 增为 1,211：共有版本处理与草稿逻辑放在原所属目录，文件数没有下降。

| 已删除的重复职责 | 现在的维护位置与保留行为 |
|---|---|
| Query / Function / PropertyType / LinkType 四份版本家族维护 | `apps/build/definition_history.go` 维护历史校验、版本读取、候选替换、发布选择与恢复；各类型检查、原字节和 Link 约束仍在原 owner |
| 三处页面组件字段检查与五十项独立调用 | `Page.CheckSectionFields` 与原文档规则循环；安装、权限发现与封存发布使用同一规则 |
| 22 个无行为 Read / Input 方法 | `platform.Reader` / `InputReceiver` 与宿主原入口；有行为的读取、输入及旧日志恢复不变 |
| 六类集成与查询 / 关系 / 共享属性 / AI 函数 / 智能体的独立草稿生命周期 | `session/definition-draft.ts`，十一个编辑器共用保存、修订、读取、丢弃与提交锁；字段、载荷、原动作和发布诊断保留；应用关联失败后重试同一资源 |
| ERP 六处创建表单与提交状态 | 原 `useNewRecord` / `DeclaredActionForm`；ID 前缀、手填编码、单据初始行保留 |
| Dialog / Sheet 的重复生命周期与管理页第二套宿主接口 | 原 OverlayFrame、useHost；焦点、隐藏保留、草稿隔离、操作权限继续由原机制负责 |
| 同值翻译、无人使用的辅助函数、无引用包依赖 | 必依赖的原字典、原时间计算、已有接口；翻译有效映射逐项一致；删除 11 项无源码引用依赖声明 |
| 各 Go 模块重复检查命令 | `verify.sh` 的 `check_module`；仍执行原 vet / test，未削减检查范围 |

相对提交 `8851c787`，十个编辑器的生命周期合并净减 81 生产代码行；规范化事务的目的状态描述修复新增 65 行，修复量单列，合计净减 16 行。测试保留动态动作与独立卡片字段，保存后的断言读取实际 `ModelBinding`，不恢复旧双轨字段。

对应 Go 编译、静态检查、应用边界、Web 类型与 Catalog 检查通过。浏览器分别验证四类定义编辑器的拒绝、修订、陈旧读取和丢弃，六类集成的新建与应用成员重试，AI 函数创建，以及列表/看板独立字段与动态动作导入。陈旧读取测试最初只控制 HTTP，实时记录快照可合法覆盖伪造的旧值，导致错误提示断言间歇失败；现仅在该测试排除当前记录的实时订阅，其余订阅保留，原陈旧读取和修订断言不变。

继续核对五处建议后，另有 9 个生产文件从 2,924 减为 2,790 代码行，净减 134：页面端口引用与作用域检查合并 73 行，接受结果直接记录捕获及任务快照合并 32 行，智能体复用原草稿管理再减 29 行。记录枚举保留原锁范围；回调读取指定记录，不再复制写入表和扫描全量。页面保留嵌入输入、观测记录及组件自身权限规则。两份发布测试复用既有 testkit，准备代码另减 34 行，不计生产删减；原业务与恢复断言保留。其他并行的约束与安装改动不计入这 134 行。逐文件前后报告为 .build/prune-followup/before.json 与 after.json。

本块 Go 编译、静态检查、Web 类型及 Catalog 检查通过；相关页面、接受结果、输入、回调、快照、重放与发布恢复测试通过。浏览器检查智能体新建、应用成员加入、五种定义编辑器的拒绝、陈旧读取、丢弃与后续修订。PostgreSQL 专项未配置数据库，未执行，不以普通测试替代。

旧动作新保存已经只保留 BusinessTxn；不能再把旧日志恢复所需的输入适配算成普通双轨。数据源直接映射对象仍是 ADR-0071 明确保留、界面可选择的有效功能，仍经原对象 create/edit 写入；若改由 Dataset/Pipeline 完全替代，须先完成相同配置与运行能力的迁移，不能只迁移测试数据后删除。当前 escapes 清单仍只有两个后端事实日志条目，没有七处前端表格可删除。

这是已完成且有证据的可删除量，不是对全仓剩余空间的上界。15%–25% 对应约 24,356–40,594 行，目前没有证据支持在保留现有功能与保证的同时删除这个数量，不能宣布全仓目标完成。未对实际维护工时、运行容量或未来扩展成本作未经测量的数值承诺。

当前必须保留：明确以 legacy / retained 命名的五份 Go 文件共 628 代码行，仍有旧声明编译、Agent 前驱、旧身份、企业视图和保留版本写入入口；raw-input 的 stored / take / create / post / numberingOf 仍服务旧日志。它们不是第二套普通写入，不能因名称旧而删除。原生行业应用仍须满足 ADR-0114 的等价替代与历史责任。先前未实施的 Section 共有结构已在 §13 的后续结构试做中完成：机械调整字面量不作为阻止结构整理的理由。仅未在源码找到直接文字匹配的 173 个长词条没有删除，动态和服务器消息使用尚不能排除。

## 13. 外部减量建议的核对与试做（ADR-0124）

无仓库访问条件下提出的“脚本精确统计”和可删 40% 不能作为证据。以本轮开始工作树、同一 `make code-size` 口径核对：手写生产源码 160,407 代码行，测试 64,250 行，生成物 27,634 行。第三方的 240,411 总行数没有提交与计数方法，不能与生产源码直接比较。Go/Proto/Lean 和 TypeScript 全部合计为 1,646 个文件、257,474 物理行 / 233,741 代码行，包含测试和生成物，仍不是可删除空间。

| 建议中的数字或判断 | 当前代码证据与处理 |
|---|---|
| page_*.go 的 162 个文件约 1.69 万行都是组件胶水 | 包含 85 个测试文件；生产 77 文件、9,435 物理行 / 8,920 代码行。模型绑定负责数据来源、字段和权限，不能替代地图、场景、审批、浮层、时序等各自的配置语义 |
| module-import 的 8,187 行占 Build 的 61% | 45 文件确为 8,187 物理行，但其中 JSON 数据 5,169 行，测试与样例代码 803 行；手写生产只有 2,215 物理行 / 2,110 代码行。占整个 Build 物理行 32.84%，占 Build 手写生产代码约 12.08% |
| accepted_ 和 Build 可直接砍半 | accepted_ 的 40 文件 / 11,112 物理行包含 19 个测试文件，生产只有 5,282 代码行；各接受结果有不同前驱和应用顺序，只合并同义的严格解码操作，不删除独立状态规则 |
| 旧路径只需隔离成不超过 1,500 行只读解码 | 旧发布定义仍需按原语义执行，旧 raw-input 日志仍需重放。仅解码不能替代这些责任；原普通写入已转同一 BusinessTxn，不以改种子撤销历史保证 |
| 18 个编辑器各 80 行可省 9,500 行，7 处逃逸须清零 | 18×80 只有 1,440；已有 DraftSession、ResourceControls、六集成编辑器共用逻辑。当前清单仅剩 MES / PMS 两个后端事实日志，无前端表格条目，不能通过删清单伪造清零 |
| 测试全面改用 testkit 能省 2.2 万行 | 根包已有 77 文件使用既有租户 / 发布测试工具；只对真实重复的准备代码试做，保留逐项业务和恢复断言，不按测试文件数量设上限 |

`host.ts` 当前 11,096 物理行 / 10,574 代码行，其中 Section 声明 135 行；统一 Go 内部声明而保持公开契约不变，不会使生成物自动减少 40%。删除有效组件配置属于改变契约和功能。

已试做：11 处同义严格 JSON 读取合为 `decodeAcceptedJSON`，每类结果自己的大小、格式、前驱、权限和应用顺序不变，生产净减 64 代码行。三个早期测试改用现有测试工具，删除 29 物理行准备代码，业务、版本和恢复断言保持。对应接受结果与恢复测试、三项业务测试通过；数据库分支没有配置 PostgreSQL，仍按原约定跳过。

后续采用了完整的 Section 结构整理，而非止于辅助函数：

- 前 100 个字段归唯一 `platform.PageWidgetConfig`；平台与构建器 Section 各保留后 33 字段及原类型、字段说明与 choices。现有 131 处字面量在 61 个文件内只作机械包装，字段值和断言不改。
- `descriptor` / `pageDraft` 两处一百字段复制改为直接赋同一配置，原应克隆的切片仍克隆。生产 Go 净减 91 代码行、15,888 字符。
- 前端 `AuthoringSection` 从手抄 133 字段改为引用生成的 `Api.Section`，只声明五种确有不同的构建期引用；预览用原类型展开并只转换这五项，不再手抄 128 个公共字段。正常排版使前端增加 5 代码行，同时减少 7,081 字符与两份字段维护清单；不把字符减少说成行减少。
- 合计本次生产代码净减 150 行（严格解码 64 + Go Section 91 − Web Section 5），不含并行任务。Go 内部结构不改变 JSON 字段次序、旧扁平字节或 host API；未修改生成器以迁就方案。新增测试覆盖全部公共字段的双向转换和扁平 JSON 字节，前端检查全部生成字段的预览保留。

结构块检查完成：platform / Build 全包、宿主页模型与 API 检查、Go 编译 / vet / 边界、Web 类型与 178 项 Build 测试、两行业应用组合检查通过；application-authoring 与 model-driven-ui 的 4 项浏览器用例通过，包含原页面保存迁移、逐跳引用、权限隐藏和不可写单元格解释。host.ts 文件字节与本轮开始完全一致。未执行本地部署或生产容量检查。

第一版泛型因构建器 Width 元数据改变公开枚举已回退；该失败只排除那种做法，不证明结构不能整理。现采用匿名共有配置，类型、metadata和原 JSON 行为分别保留。数万行的删减承诺仍没有证据，但此前把百份字段维护当作只能逐函数小修的问题也不成立。

可参考的是定位重复声明、机械编解码和测试准备代码的方向；不能采纳无证据的 14.49 万行总上限、文件数上限与各区固定删减额度。试做继续使用原归属，先完成整块，再按实际影响验证；实际结果只按前后同口径统计。
