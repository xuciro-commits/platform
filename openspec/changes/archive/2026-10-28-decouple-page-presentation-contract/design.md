# Design

## Context

动机见 [proposal.md](proposal.md)。2026-10-27 对当前源码核对的事实如下，均不是实施完成声明。

| 核对项 | 实际结果与定位 |
|---|---|
| 页面文件 | `capabilities/server/platform/page*.go` 共 163 个：78 个非测试、85 个测试；非测试合计 9273 行。口径仅此目录直接子文件，不包括宿主根目录同名文件、JSON、Web 或生成物。 |
| 页面清单 | `capabilities/server/platform/pageui/widgets.json` 声明 77 项组件、111 个受支持 profile，当前 `platform.page.v2.111`。这些数量是文件盘点，不是冗余或安全证明。 |
| Go 视觉感知 | `page_layout_size.go` 校验宽高、权重及滚动；`page_region_presentation.go` 校验 padding、背景和折叠；`page_spacer.go`、`page_separator.go` 校验视觉属性。`page_widgets.go` 同时 embed 清单、校验 props/defaults/slots、反射 Section 字段，并导出 Web 清单。 |
| 真实安全/语义 | `page_widget_validation.go` 校验端口类型、同 owner、局部作用域、排序和窗口；`page_kanban.go` 还检查原生命周期动作和动态目的状态；`page_record_gantt.go` 还检查可见字段类型和原状态值。`page_spatial.go` 还检查资产/样本引用，`page_collaboration.go` 检查原文件、评论、附件等服务 owner。不能整件当成像素校验删除。 |
| 运行与投影 | `installed.go` 安装校验，`definition_page_projection.go` 过滤获权字段、动作、函数、计算及传递依赖；`page_model.go` 读取确切已发布页或 Builder 预览，再做当前记录、字段及规则授权。`page_queries.go`、`page_navigation.go`、`page_compute.go` 继续承担各自原请求。 |
| Web owner | `web/packages/app/src/widgets/registry.ts` 与 `web/packages/build/src/workshop/page-editor/widgets/registry.ts` 消费同一清单并要求完整注册；只有清单、没有 Renderer/Inspector，不会使组件可运行。`runtime/model-bindings.ts` 已有旧端口正规化，不应再建第二条映射。 |
| 纯规则 | `go list` 显示 `internal/rules` 的 8 个非测试 Go 文件仅导入标准库：bytes、encoding/json、fmt、io、math、math/big、regexp、slices、strconv、strings、time、unicode/utf8；非标准库依赖闭包只有该包自身。源码没有数据库、HTTP、文件读取或隐式 `time.Now`；时间、成员和集合由 Context 显式传入。 |
| Formula 边界 | `apps/build/formula.go` 不纯：`computeRulesOf` 通过 Caller.RuleContext 捕获获权事实并反射写回结果，`validateOf` 还读取 extension 引用记录；`internal/construction/formula.go` 是旧算术语法转换，导入 platform 并降为同一 Expr，并无第二个 evaluator。不能把两个 formula 文件整体编到浏览器。 |
| 已有浏览器 WASM | `cmd/rule-browser/main.go` 在 `GOOS=js GOARCH=wasm` 下导出 Compile/Eval/LowerPage，`scripts/rule-browser.mjs` 生成模块，`runtime/rule-engine.ts` 在浏览器加载；页面变量及作者规则已有消费。它使用宿主同源 rules，而非 JS 规则副本。 |
| 独立制品执行 | `cmd/wasm-worker/main.go` 导入整个 `platformserver` 并启动 Unix socket 服务；`wasm.go` 用 wazero 校验摘要、ABI、内存、期限、输入输出与调用归属。它是 WASIp1 command/data 制品执行隔离边界，不能作为浏览器构建目标或合并为页面运行器。 |
| 本轮实际检查 | `go test ./internal/rules` 通过；`GOOS=js GOARCH=wasm go build -trimpath -o /tmp/platform-rules-audit.wasm ./cmd/rule-browser` 通过；以该新模块运行既有 `runtime/rule-engine.test.mjs`，32 项向量通过。未运行页面迁移、完整恢复或生产负载。 |

附件要求的“55+”不是精确库存；这里给出全量实际文件。Token 倍数没有本仓库测量依据；端侧处理比例、运行成本和后端文件减半只作为目标，不写成现行事实。`time` 标准库导入不等于隐式取时，纯性以显式输入和无 I/O 调用核实。

## Goals / Non-Goals

**Goals:** 后端页面只解释获权数据、原动作、输入和有界会话作用域；Web 自己拥有视觉类型与验证；新旧页面沿原发布、依赖、授权与历史 owner 工作；复用已经存在的纯 Rules/WASM。

**Non-Goals:** 不改变 K1–K9、宿主接受/恢复及业务动作；不废除 77 项现有呈现能力；不允许租户加载任意渲染脚本；不以浏览器结果代替授权或正式计算；不合并隔离 Worker；不将文件组织改成另一套执行器或能力目录；不在本轮落实业务代码。

## Decisions

### 1. 五要素是安全声明与呈现的分界，不是五个无类型袋子

新页面采用格式 3 的 tagged envelope，页面资产身份、作者修订、来源版本、内容摘要和冻结原件继续由原资产机制持有。五个页面要素为：

| 要素 | 后端必须理解的类型化内容 | 不进入不透明配置的内容 |
|---|---|---|
| DataBinding | 对象/字段路径、确切查询/record-set/query-set/link-type/interface 版本、完整性、过滤/排序、分页和聚合预算、文件/观测等原资源引用及它们的有界依赖图 | 查询谓词、所读字段、跨对象关系、来源与样本关联、窗口上限 |
| ActionTrigger | 稳定触发身份、确切 action/flow/function/operation 等原能力引用、参数映射及输入输出签名；按种类调用原 owner，不另解释业务规则 | schema、动作允许集合、审批/Confirmation 限制、未知请求键及原结果身份 |
| InputSchema | 字段/参数类型、必填和约束、页面变量及同一 Expr 的纯联动、输出端口类型 | 数据类型、表达式依赖、缺失/不可读/不完整状态及写入签名 |
| ScopeVisibility | page/application/局部 owner、输入输出与嵌入接口、资源归属/完整性、数据来源、获权投影与最大展开预算；外部资源仍受原文件/URL/CSP边界约束 | 跨 scope 访问、敏感字段使用、动态读取激活、文件凭据和外部能力 |
| WidgetSpec | 后端只接受有界合法 JSON，保留原字节和呈现格式标记；不解释组件 ID、颜色、图标、尺寸或容器结构 | 不得因此取得任何数据/动作/文件权限 |

`ScopeVisibility` 是当前读取投影和类型/依赖约束，不是授予，也不是将 `visibleWhen` 升级为授权。局部 scope、循环展开和查询激活图与 DOM 树分离：后端按显式 owner/依赖/最大迭代预算校验，不能信任 WidgetSpec 中的“隐藏”或“未使用”来省掉安全预算。`ActionTrigger` 保留“省略动作限制”和“显式空集合禁止动作”的区别。

不选择“全页面 RawMessage 后再由客户端自报有权”：这会失去 dependency closure、字段来源和当前权限。不选择“把 78 文件原样拼成 3 个大文件”：需要先删除视觉类型与视觉分支，剩余代码才按三种职责收敛。

### 2. Web 只有一个呈现清单和验证实现

将原清单迁为 `web/packages/app/src/pageui/widgets.json`，运行注册及作者注册继续消费它；视觉类型生成到同目录 `types.ts`，输入/视觉校验集中于同目录 `contract.ts` 并复用现有 ValueSchema/字段解析与 layout diagnostics。已有 Renderer、Inspector、布局和专业视图继续在原位置实现，不复制一套组件库。

`WidgetSpec` 只引用声明中的稳定 binding/trigger/input/scope ID。运行器从受控 binding handle 取得数据，禁止从 WidgetSpec 临时构造新的记录查询、文件地址、AI、Compute 或动作请求。未知或非法视觉配置在作者/导入检查和运行边界保留原输入并显示明确诊断；不静默删配置。发布 API 只承诺安全声明合法及字节有界，**不再声称 Go 验过像素/颜色或特定组件兼容性**。这是有意调整现行 workspace 与 professional-visualization 的发布情景，不把冲突藏在“重构保持行为”中。

旧 Go 清单中的纯视觉规则迁 Web；实体字段类型、来源、端口签名及资源预算转换成通用 binding/schema/scope 规则，不再按 Widget ID 分派。外部 frame 的 HTTPS 来源、禁止平台输入/消息桥、文件访问和 GLB 外部资源限制必须保留到通用资源策略或原文件 owner；浏览器仍复核 URL/CSP/GLB，不把这类限制当作 padding 删除。`application_header.go` 对原 `ValidPageImageURL` 的调用一并改为复用通用安全资源规则，不能删函数后留下旁路。

当前还有35个 `web/packages/ui/src` 文件导入了 `Api.Page*` 或 `pageUIManifest`，且 `web/packages/kernel/src/widget-authoring.ts` 从Go清单推导视觉配置。不能简单把import改为app而形成 `ui → app → ui` 环：UI原组件改为自己拥有的primitive props，app原适配器将呈现配置与获权binding结果转换成这些props；服务端资源预算作为通用typed数据传递，UI自己保留字节/文件/渲染分配检查。kernel中的纯视觉authoring类型迁入app，kernel仍只导出宿主安全API。完整35个消费者见附表E；这是类型归属调整，不是删除UI的最后一道解析与资源保护。


WidgetSpec 变为不透明后，Go翻译器必须像跳过Languages本身一样跳过其原始字节，只翻类型化安全元数据。标题、说明、help及呈现清单明确标为声明文本的字段由Web原页面owner展示时翻译，不递归改写任意JSON或业务值。词典来自同一已发布Application.Languages及原translator解决覆写/冲突后的只读结果：在原获权页面/发现响应投影当前owner、语言与发布/读取世代对应的有效词典；不从Build草稿取词，不改Page或候选原件。`/v1/words`仍是原管理员配置入口，普通使用者不因本次变化获得该权限。

原translator继续唯一处理租户覆写优先、同owner冲突回退及缓存失效；Web原 `t()` 只消费该已解决快照作声明字段查找/插值，不再次合并Application词典或实现另一冲突算法，不用全局register把不同owner词典相互污染。快照只是原发现投影，不新建语言服务、词典存储或正式账本；切换语言及发布、撤权、主体切换沿原读取/会话世代失效。英文源、引用/状态代码、记录业务文字和原候选字节保持不变，临时显示译文不参与摘要。

### 3. 旧原件保留，普通作者与运行只走新安全契约

原 v1/无 document 与 v2 的 111 个 profile 不能按最新版本解释。新 `page_model.go` 的版本化 decoder 读取原封存资产，使用 `platform/page_compat.json` 的冻结旧字段路径/类型/依赖映射，投影同一个五要素契约；投影是纯转换，不读业务记录、不联网、不运行授权或当前 Widget 视觉检查。该 JSON 只承载旧绑定映射及版本门槛，不复制旧组件 Renderer，不向新普通作者开放旧语法。

兼容表须覆盖旧显式指针、零值结构、空集合、查询激活、Loop/Overlay/slot/unused 的原 scope 和预算语义。仅视觉的旧属性直接交给 Web 原 profile 解释；旧安全字段没有可验证映射则明确不可用，不猜默认、更不能原样放 WidgetSpec 后继续执行。旧 ID、修订、来源版本、PageContentVersion、候选摘要和包摘要继续取原 owner 的原件身份；不能把新类型 marshal 的投影当成旧字节重算身份。

新编辑显式将所选草稿升级为格式 3，并创建新修订/候选；原页面的已发布原件、包、接受结果和历史均不变。精确旧页面读取、审批、嵌入、在途实例及恢复沿现有 retained artifact 与 owner 格式责任；新接受结果恢复只应用保存映像。旧输入日志仍需旧 owner 重建时按 `runtime/storage` 原限制处理，不承诺任意旧二进制回退。兼容映射不是长期新旧普通执行双轨，所有可执行读取投影到同一安全契约。

### 4. 计算继续同源，服务端事实捕获与纯求值分别归属

保留 `internal/rules` 的8个现有生产文件；旧算术语法 lowering 的唯一目标为 `capabilities/server/internal/rules/formula_lowering.go`，只接受语法、字段类型和显式上下文，不导入 platform、Caller、数据库或网络，继续调用原 `compile.go`，不新增 evaluator。独立旧语法文件承担保留格式读取边界；三个旧Formula路径删除后净少两个Go路径，不以新增文件规避收敛。`apps/build/formula.go` 与当前 `apps/build/rules.go` 的排序、编译及运行适配，按总清单共同并入 `capabilities/server/studio/build/build_formulas.go`；事实捕获和反射赋值仍属服务端Build owner。`validateOf` 目前位于formula文件，其对象归属现有文件是 `apps/build/objects.go`，最终内聚至 `capabilities/server/studio/build/build_objects.go`。测试随同责任迁移，不重建已删除的 `apps/build` 或 `internal/construction` 路径。

继续用 `cmd/rule-browser` 构建 Go js/wasm，通过原 `runtime/rule-engine.ts` 提供受信纯预览；扩展正常表单联动时只使用当前获权且有来源的本地快照及未提交输入。模块初始化失败明确说明联动不可用，不偷偷调用云模型、不回填“0”、不将浏览器结论写成正式结果。服务器重新捕获当前获权事实并用选定 RuleEngine 决定；缺失、withheld、不完整集合、精确十进制/货币、日期、预算和诊断继续同一语义。

`cmd/wasm-worker`、`wasm.go`、`compute_socket.go`、`code_compiler.go` 保留原隔离制品责任。浏览器纯规则模块与 WASIp1 command/data ABI 不能互换；不为了文件目标将无网络 Worker 合入宿主，亦不把它导入纯 rules。规则提供方可替换性继续遵守 business-models 的实现代际；浏览器只能声称其已支持的相同语义版本，不冒充任意动态提供方已获验证。

### 5. 与现行规范、其他战役的关系

- 现行 `experience/workspace` 要求 Go 与 Web 共用 Widget 清单并在发布拒绝视觉字段归属，本 change 明确改为 Web 单一视觉清单 + 后端单一安全契约；旧版本解释与安全拒绝保留。
- `experience/professional-visualization` 的专业发布情景同步调整；坐标、原记录、附件、样本、GLB与资源释放等其他需求不降低。
- `platform/application-lifecycle` 禁止将所有资产无类型化；WidgetSpec 只豁免视觉，原依赖闭包、版本、角色与发布权威继续类型化。
- `platform/business-models` 已要求纯编译与同源浏览器语义，目前 rules 已满足 I/O 纯性；本次重点是移走旧语法 lowering 的 platform 依赖和混合适配，不把既有能力写成待新建能力。
- `consolidate-platform-boundaries` 现有“一种接受结果一个文件”与宿主分包方案由战役1修改。本 change 不预设它已完成，也不占用其 engine/studio 目录；宿主根页面读取文件本轮保留原 owner，随后由战役1按统一依赖图物理迁移。不得在两个 change 同时发明页面执行器。
- 根 guard 可针对新增 Go 视觉定义设置递减边界，删除旧文件后同步减少基线；不能禁止兼容 JSON 或前端视觉词汇，也不能仅靠黑名单证明授权和历史正确。

## Risks / Trade-offs

- [直接 API 不再验证视觉合法性] → 正常作者/导入和运行统一检查；原字节及错误定位保留。发布 API 的保证收窄需要负责人审阅本提案后再实施，不能在代码提交时隐瞒。
- [不透明配置藏有安全引用] → 宿主只执行五要素中显式绑定；正常运行器不允许 raw 配置获得新读取/动作；恶意直接 API 仍由原独立授权、记录及文件边界拒绝。
- [旧格式转换漏掉控制或数据边] → 对全部旧 profile 和 77 项注册导出旧安全字段映射，按原页面/包原件比对 ID、闭包、动作允许集合与预算；不合法迁移保留原件并拒绝，不承诺未知格式自动修复。
- [三文件变成新上帝对象] → 只留下契约/查询/规则三个职责；视觉实现留在原前端功能模块，实测迁移后文件数、非测试 LOC、依赖图和语义覆盖。文件数差量不是质量或性能证明。
- [纯 WASM 被当成离线接受或云模型替代率] → 只报告同源向量及具体表单路线；测量实际任务分布后再讨论运行占比，浏览器永不承担最终授权、预留或持久接受。

## Migration Plan

1. 实施先固定旧格式与支持范围，生成安全绑定对照及原件摘要夹具；不要先删 Go 文件。
2. 在既有页面草稿/发布/读取 owner 内引入五要素及旧格式纯投影，原 API 类型按工具生成；Web 切换同一新契约。版本切换失败保持当前发布，不改旧原件。
3. 迁移全部视觉类型、77项注册验证和专业呈现安全消费者；从原后端逐个移除视觉分支，按下表删除源文件。原权限、查询、文件、动作与在途能力不丢失时才删除对应旧普通路径。
4. 按总清单将旧Formula纯lowering迁入同一 `internal/rules/formula_lowering.go`，声明及正式适配收进studio/build，删除三个旧Formula源/测试路径，扩展原浏览器表单联动；原宿主重新执行最终规则。
5. 按 tasks 集中运行 owner、Go/Web、同源向量、一条通用业务页面路线及恢复检查，核对原候选、包和历史摘要不变。主规范同步与归档只在实现与适用检查完成后进行。
6. 新格式被正式写入后，旧二进制不能直接回退；需要仍支持格式3和原已接受后缀的兼容版本。部署、数据卷、密钥和租户配置按原交付规则保留。

## 审阅时确认的取舍与后续细节

本设计已选定“后端不校验视觉内容、Web统一校验”和“历史只投影、不重写”的目标；二者已获负责人实施授权，按任务的真实实现与验证收口。若负责人要求直接 API 也阻止所有视觉错误，应先重新定界（例如受信前端编译工具的发布产物验证），不能在 Go 隐藏第二份组件 schema 或新设网络校验服务。

可稍后确定且不改变上述方案的细节：格式3首次上线的客户端升级窗口、未识别视觉版本的就地诊断文案、实际性能测量使用的代表性表单。没有流量占比或 Token 成本的现有基线，不能预填目标达到的日期或性能数字。

## 文件删除、合并与目录收敛对照

所有路径均相对仓库根目录；以下是统一实施目标；未完成项不能当作已经删除。目标目录不新建 Go 页面服务包：公开安全契约仍在 `capabilities/server/platform/`，运行读取仍在原宿主 owner，前端呈现在 `web/packages/app/src/pageui/` 与原 widgets/pages/runtime。

- 页面生产文件：78 → 3，保留并重写 `page_model.go`、`page_queries.go`、`page_rules.go`，其余75个在迁移后删除。
- 页面测试文件：85 → 2，安全及兼容测试并入现有 `page_model_test.go`、`page_queries_test.go`；纯视觉和运行测试迁 Web，其他83个旧测试路径删除。
- 因而 `platform/page*.go` 计划163 → 5，净减少158个 Go 文件；新增兼容JSON与Web文件另计，不声称这些文件完全没有复杂度。
- 另删除 `apps/build/formula.go`、`apps/build/formula_test.go`、`internal/construction/formula.go` 三个路径，纯lowering使用总清单唯一新目标 `internal/rules/formula_lowering.go`，声明和正式适配内聚到studio/build；此项与战役1共享同一行映射，同一路径只算一次。

物理迁移路径以 [战役1总清单](../consolidate-platform-boundaries/file-consolidation.md) 的“原精确路径”所在唯一行为准；本表表达页面/Formula责任迁移，未额外列明的旧宿主路径只作审计定位，实施时须按总清单解析到目标，不能重建已退出的apps/build、internal/construction或旧根目录owner。platform页面公共契约最终保留三文件，规则纯实现保留internal/rules，不为四域目录名再复制。

### A. 全部78个页面生产文件

表中 `契约`=`capabilities/server/platform/page_model.go`，`查询`=`capabilities/server/platform/page_queries.go`，`规则`=`capabilities/server/platform/page_rules.go`；完整目标路径在每行写出。视觉目标为 `web/packages/app/src/pageui/contract.ts`、`types.ts` 及原 Renderer/Inspector；旧映射只归 `capabilities/server/platform/page_compat.json`。

| 现有文件 | 操作及完整目标路径 | 删除前必须迁移的真实责任 |
|---|---|---|
| `capabilities/server/platform/page_ai.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原函数/Agent确切绑定、record输入、角色与来源字段；呈现分离 |
| `capabilities/server/platform/page_alert.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_boolean_input.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 状态/端口可写性及同scope检查；控件外观迁Web |
| `capabilities/server/platform/page_button_group.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 稳定control ID和ActionTrigger映射保留；按钮variant/icon不留Go |
| `capabilities/server/platform/page_choice_input.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 选项来自原字段/获权窗口、选值类型和联动归属 |
| `capabilities/server/platform/page_collaboration.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 附件/评论/关注/标注的服务身份、原记录和动作签名 |
| `capabilities/server/platform/page_collection_analysis.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 获权集合、轴与派生来源、排序/窗口以及分析结果类型 |
| `capabilities/server/platform/page_collection_builder.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 输入集合来源、字段类型、局部owner与依赖循环预算 |
| `capabilities/server/platform/page_collection_input.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 嵌入页面的集合输入及原对象类型 |
| `capabilities/server/platform/page_composite.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 多模型绑定、角色字段、规则读取与行/列/值集合预算 |
| `capabilities/server/platform/page_compute_resource.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 受控Operation版本、输入/输出与获权record来源 |
| `capabilities/server/platform/page_content.go` | 迁移后删除 → `capabilities/server/platform/page_model.go` | 原内容身份和确切旧摘要算法；不能marshal新投影冒充原件 |
| `capabilities/server/platform/page_context_views.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | breadcrumb/avatar记录及query来源；图片URL的通用资源安全 |
| `capabilities/server/platform/page_date_input.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原日期/时间类型语义迁InputSchema及internal/rules/value.go；控件offset呈现迁Web |
| `capabilities/server/platform/page_detail_presentation.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 条码读取原ID/字段资格；尺寸/格式迁Web |
| `capabilities/server/platform/page_device.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_document.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 旧格式纯投影、稳定ID、scope/变量/触发绑定；排版树迁Web |
| `capabilities/server/platform/page_embedding.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 子页确切内容版本、接口类型、原scope、闭环与导航权限 |
| `capabilities/server/platform/page_events.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 事件到原动作/导航/overlay及输入输出类型；弹层chrome迁Web |
| `capabilities/server/platform/page_exploration.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 多对象/关系图端点、原query/集合/asset依赖及获权投影 |
| `capabilities/server/platform/page_external_frame.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | HTTPS来源、禁止平台输入与消息桥；高度迁Web，通用资源策略保留 |
| `capabilities/server/platform/page_facets.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 过滤器字段存在性、类型、查询归属及授权 |
| `capabilities/server/platform/page_gauge.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 数值绑定/类型与来源保留；量程颜色/针盘迁Web |
| `capabilities/server/platform/page_heatmap.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 获权group/measure字段、数值类型和有界窗口；色阶迁Web |
| `capabilities/server/platform/page_histogram.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原数值字段及查询预算；分桶显示参数迁Web |
| `capabilities/server/platform/page_inline_action.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原动作签名、参数映射、字段与目标一致性 |
| `capabilities/server/platform/page_input.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_interface.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 页面输入输出、确切导航目标、类型和原owner |
| `capabilities/server/platform/page_kanban.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 生命周期transition、目的状态/参数、原动作及获权字段，不只是视觉拖拽 |
| `capabilities/server/platform/page_layout_size.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_leaderboard.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原sort/limit和字段类型；排名显示迁Web |
| `capabilities/server/platform/page_loop.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 局部作用域、父子集合类型、展开乘法预算和泄漏防护 |
| `capabilities/server/platform/page_metric_presentation.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_model.go` | 保留重写 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | ModelBinding正规化、字段路径/规则类型、依赖与显式安全模型 |
| `capabilities/server/platform/page_notice.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_observation.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原观测/资产/样本引用、查询版本、窗口和信号输出来源 |
| `capabilities/server/platform/page_ontology_interface.go` | 迁移后删除 → `capabilities/server/platform/page_model.go` | 确切interface查询、资源端点和依赖版本 |
| `capabilities/server/platform/page_progress.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_property.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go` | 获权字段类型/路径及原record来源 |
| `capabilities/server/platform/page_queries.go` | 保留重写 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 有界查询图、类型、排序、原版本、当前字段与集合端口 |
| `capabilities/server/platform/page_query_inventory.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 旧profile激活差异及有界依赖图；不靠raw视觉隐藏省预算 |
| `capabilities/server/platform/page_query_sets.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go` | 集合条件、主体输入、完整性和循环约束 |
| `capabilities/server/platform/page_range.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_record_calendar.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 可见日期/标题字段保留；月份/格子迁Web |
| `capabilities/server/platform/page_record_card.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原label字段可读性与标量类型；卡片布局迁Web |
| `capabilities/server/platform/page_record_chart.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原数值/分组字段、固定查询窗口；图型迁Web |
| `capabilities/server/platform/page_record_comparison.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | record-set类型、字段与同owner；比较布局迁Web |
| `capabilities/server/platform/page_record_events.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 事件字段类型/原状态值和时间来源；tone迁Web |
| `capabilities/server/platform/page_record_gantt.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 获权起止/状态字段与原窗口保留；视口日期范围及tone迁Web |
| `capabilities/server/platform/page_record_links.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原引用端点、inverse、获权关系和查询 |
| `capabilities/server/platform/page_record_list.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原label字段/窗口来源；列表显示方式迁Web |
| `capabilities/server/platform/page_record_picker.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 获权有序ID查询、集合端口、选值类型与owner |
| `capabilities/server/platform/page_record_scatter.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原数值x/y和记录来源；点样式迁Web |
| `capabilities/server/platform/page_record_view.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原记录/详情/关联与动作允许集合；tab呈现迁Web |
| `capabilities/server/platform/page_record_work.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原动作参数签名、默认值字段来源、审批/工作查询资格 |
| `capabilities/server/platform/page_region_presentation.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_rules.go` | 保留重写 → `capabilities/server/platform/page_rules.go` | 获权字段/路径类型与同源纯表达式；不另建求值器 |
| `capabilities/server/platform/page_separator.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_shared_record_context.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go` | page/application/overlay/loop资源输入owner |
| `capabilities/server/platform/page_shared_record_output.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go` | 共享record输出的对象/类型/作用域 |
| `capabilities/server/platform/page_shared_record_set.go` | 迁移后删除 → `capabilities/server/platform/page_queries.go` | 共享集合输出的对象/类型/作用域 |
| `capabilities/server/platform/page_slots.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 插槽排版迁Web；原父子scope关系进入显式通用owner图 |
| `capabilities/server/platform/page_spacer.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_sparkline.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 当前窗口有序记录/数值与输出端口；迷你图迁Web |
| `capabilities/server/platform/page_spatial.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 资产/样本引用、当前文件与值来源保留；坐标/轴/渲染配置迁Web |
| `capabilities/server/platform/page_status_tracker.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原状态字段/值及同record来源；步骤外观迁Web |
| `capabilities/server/platform/page_summary.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 获权group/measure字段、类型与聚合来源 |
| `capabilities/server/platform/page_table_edit.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原编辑schema/字段写资格、revision及显式空动作限制 |
| `capabilities/server/platform/page_table_presentation.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 列/字段引用与可编辑白名单保留；排序布局/密度迁Web |
| `capabilities/server/platform/page_terms.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原字段类型/查询计数和集合范围 |
| `capabilities/server/platform/page_timeline.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 时间字段与原事件/记录来源 |
| `capabilities/server/platform/page_titles.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 视觉属性/尺寸/文案校验迁Web；只保留外层字节、类型与预算边界 |
| `capabilities/server/platform/page_treemap.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原group/measure、集合端口与预算；矩形显示迁Web |
| `capabilities/server/platform/page_unused.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原未使用节点保存/恢复及profile差异；安全owner与预算保留 |
| `capabilities/server/platform/page_variables.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 显式状态/资源/规则类型、来源、依赖、完整性与原scope |
| `capabilities/server/platform/page_widget_validation.go` | 迁移后删除 → `capabilities/server/platform/page_rules.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 通用端口、字段类型、同owner及计划约束；不保留按组件分派 |
| `capabilities/server/platform/page_widgets.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 移走embed视觉注册/反射props；安全约束进五要素，旧门槛进冻结映射 |
| `capabilities/server/platform/page_work_views.go` | 迁移后删除 → `capabilities/server/platform/page_model.go`；`web/packages/app/src/pageui/contract.ts` / `web/packages/app/src/pageui/types.ts` | 原Work/Flow/Agent/Runs引用、角色、来源与query |

### B. 全部85个页面测试文件

不按名称直接丢弃测试；同一文件内的视觉用例迁前端，安全、原版本和原件兼容用例并回既有Go owner测试。以下为每个源测试的目标；`contract.test.ts` 负责纯视觉配置，`page_model_test.go` / `page_queries_test.go` 保留宿主不可移交的检查。已有真实 Renderer/Inspector 测试继续原位置维护，不用反复镜像每个实现分支。

| 现有测试 | 操作与完整目标路径 |
|---|---|
| `capabilities/server/platform/page_aggregate_ports_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_ai_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_alert_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_boolean_input_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_button_group_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_chart_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_choice_input_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_collaboration_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_collection_analysis_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_collection_builder_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_collection_input_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_compute_resource_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_context_views_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_counts_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_date_input_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_detail_presentation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_document_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_embedding_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_events_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_exploration_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_external_frame_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_facets_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_gauge_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_heatmap_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_histogram_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_indexed_choice_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_inline_action_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_input_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_interface_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_kanban_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_layout_size_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_leaderboard_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_link_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_loop_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_metric_presentation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_model_test.go` | 保留并合并适用安全用例 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_nested_loop_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_notice_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_observation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_overlay_filter_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_overlay_presentation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_overlay_resources_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_overlay_scope_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_pivot_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_progress_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_property_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_queries_test.go` | 保留并合并适用安全用例 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_query_inventory_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_query_sets_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_range_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_calendar_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_card_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_chart_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_comparison_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_events_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_gantt_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_links_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_list_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_picker_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_scatter_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_sets_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_view_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_record_work_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_region_presentation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_separator_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_shared_record_context_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_shared_record_output_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_shared_record_set_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_shared_select_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_slots_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_spacer_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_sparkline_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_spatial_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_status_tracker_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_summary_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_table_edit_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_table_presentation_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_tag_counts_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_terms_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_queries_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_timeline_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_titles_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_treemap_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_unused_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_widget_ports_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |
| `capabilities/server/platform/page_work_views_test.go` | 迁移后删除原路径 → `capabilities/server/platform/page_model_test.go`（安全/兼容）；`web/packages/app/src/pageui/contract.test.ts`（仅视觉） |

### C. 清单、消费方、公式和执行工具

| 现有完整路径 | 操作及目标完整路径 | 不得丢失的责任 |
|---|---|---|
| `capabilities/server/platform/pageui/widgets.json` | 迁移为 `web/packages/app/src/pageui/widgets.json`；旧安全路径和profile门槛冻结到 `capabilities/server/platform/page_compat.json` | 77项注册、111个旧profile的解释；不在Go保留视觉清单 |
| `capabilities/server/platform/definition.go` | 保留；页面声明改引用三文件的五要素，删除具体视觉结构的公共Go依赖 | 资产与原Page身份、其他Definition种类不变 |
| `capabilities/server/platform/application_header.go` | 保留；安全资源校验复用 `capabilities/server/platform/page_model.go` 的通用资源策略 | 原图片URL安全责任不能因Page函数删除而消失 |
| `capabilities/server/platform/dependencies.go` | 保留；改消费类型化绑定/trigger/schema/scope | 同一依赖投影、闭包与确切版本；raw WidgetSpec不发明依赖 |
| `capabilities/server/platform/asset_lifecycle.go` | 保留；使用原件内容身份与版本化页面读者 | 精确旧页读取、未改写的内容摘要 |
| `capabilities/server/installed.go` | 保留宿主owner；删除视觉分派，调用通用安全检查 | 安装不扩大权限、签名和集合范围 |
| `capabilities/server/definition_page_projection.go` | 保留宿主owner；按安全绑定ID过滤再给Web呈现引用 | 当前获权字段、动作、来源及传递失效；不改原封存body |
| `capabilities/server/page_model.go` | 保留宿主读取owner，消费通用模型与显式预算 | 当前成员、Builder预览与精确正式页、行字段/规则授权 |
| `capabilities/server/page_queries.go` | 保留原查询owner；消费统一typed DataBinding | 查询确切版本、输入及完整性 |
| `capabilities/server/page_composite.go` | 保留原多模型读取owner，去Widget ID分派 | 多模型和矩阵读取仍走原记录/规则，不能前端拉全集 |
| `capabilities/server/page_navigation.go` | 保留原导航owner；使用类型化输入/输出接口 | 版本、类型、当前可发现入口与原记录身份 |
| `capabilities/server/page_compute.go` | 保留原Compute owner接线；使用ActionTrigger | 固定Operation、输入输出、权限与限额 |
| `capabilities/server/page_content.go` | 保留；读取原冻结页原件 | 当前权限、内容版本、旧字节 |
| `capabilities/server/apps/build/page.go` | 原草稿owner迁 `capabilities/server/studio/build/build_pages.go`；新普通草稿保存格式3 | 可视化/源码一份草稿、修订冲突、原发布入口 |
| `capabilities/server/apps/build/page_content.go` | 原候选/历史owner迁 `capabilities/server/studio/build/build_pages.go` | 原精确页面与候选内容，不用新投影重算旧摘要 |
| `capabilities/server/apps/build/formula.go` | 删除；`formulaExpr`/`checkFormulas`/`formulaOrder`/`computeRulesOf`并入 `capabilities/server/studio/build/build_formulas.go`；`validateOf`内聚 `capabilities/server/studio/build/build_objects.go` | 旧语法/排序、获权事实、正式赋值、extension检查；不是整个文件纯化 |
| `capabilities/server/apps/build/formula_test.go` | 删除；适配测试迁 `capabilities/server/studio/build/build_formulas_test.go`，纯lowering用例并入 `capabilities/server/internal/rules/rules_test.go` | 旧数值解释、公式依赖、冻结读取 |
| `capabilities/server/internal/construction/formula.go` | 删除；纯parser/lowering迁 `capabilities/server/internal/rules/formula_lowering.go`，必要声明/适配内聚 `capabilities/server/studio/build/build_formulas.go`，应用API沿 `capabilities/server/platform/rules.go` | 单一算术lowering，无platform/Caller依赖；与战役1协调不重复实现 |
| `capabilities/server/internal/rules/formula_lowering.go` | 总清单唯一新纯lowering目标；承接 `internal/construction/formula.go` 的旧语法 | 只输出原Expr并复用compile.go；旧格式读取边界，不是另一求值器 |
| `capabilities/server/internal/rules/model.go` | 保留 | 类型、显式时间/成员/集合和缺失状态 |
| `capabilities/server/internal/rules/compile.go` | 保留；旧lowering输出仍交同一compiler | 类型检查、预算及冻结旧语法，不另建compiler |
| `capabilities/server/internal/rules/eval.go` | 保留 | 同一纯求值与错误语义 |
| `capabilities/server/internal/rules/value.go` | 保留；必要的通用日期类型检查在此复用 | 精确数值、缺失/withheld及类型解析，不导入页面视觉 |
| `capabilities/server/internal/rules/compare.go` | 保留 | 单一值比较 |
| `capabilities/server/internal/rules/aggregate.go` | 保留 | 完整性、数值聚合 |
| `capabilities/server/internal/rules/window.go` | 保留 | 原窗口摘要与纯计算 |
| `capabilities/server/internal/rules/page.go` | 保留 | 旧页面表达式lowering到同一Expr，非另一求值器 |
| `capabilities/server/internal/rules/rules_test.go` | 保留并吸收纯formula用例 | 同源语义与旧值转换 |
| `capabilities/server/internal/rules/conformance.json` | 保留并扩展有意义的新增边界向量 | Go/浏览器同一向量源，不把32项现状当全语义完备 |
| `capabilities/server/platform/rules.go` | 保留原唯一应用API | 选定规则引擎和纯lowering适配，不加另一规则实现 |
| `capabilities/server/cmd/rule-browser/main.go` | 保留浏览器构建入口 | 受信同源Compile/Eval/LowerPage及语义版本 |
| `scripts/rule-browser.mjs` | 保留 | 正式构建同源浏览器WASM，不编译服务端Worker |
| `scripts/rule-test.mjs` | 保留 | 用同源浏览器模块跑原向量 |
| `capabilities/server/cmd/wasm-worker/main.go` | 保留独立服务 | Unix socket与WASIp1隔离，不承担客户端预览 |
| `capabilities/server/wasm.go` | 保留独立执行实现 | 摘要/ABI/数据通道/内存/取消/时间/输出上限 |
| `capabilities/server/compute_socket.go` | 保留 | 原受控Worker接线 |
| `capabilities/server/code_compiler.go` | 保留 | 原受控离线编译依赖；不因页面改造合并builder/worker |
| `web/packages/app/src/widgets/registry.ts` | 保留，清单来源改为 `web/packages/app/src/pageui/widgets.json` | 原77项运行注册及真实实现匹配 |
| `web/packages/build/src/workshop/page-editor/widgets/registry.ts` | 保留，继续经 `@platform/app` 消费同一视觉清单 | 原Inspector、binding编辑及草稿会话 |
| `web/packages/app/src/runtime/model-bindings.ts` | 保留并改消费通用绑定；旧端口映射与新格式协调 | 不产生新旧两套普通模型解释 |
| `web/packages/app/src/runtime/PageModel.ts` | 保留并改消费有界binding handle | 不从raw WidgetSpec推导额外请求；原scope和取消 |
| `web/packages/app/src/runtime/PageNavigation.ts` | 保留 | 原实例、嵌入、输入输出及预览隔离 |
| `web/packages/app/src/runtime/rule-engine.ts` | 保留并接入适用表单纯联动 | 同源模块初始化、显式输入、错误和权限状态 |
| `web/packages/app/src/runtime/rule-engine.test.mjs` | 保留 | Go/WASM原一致性向量 |
| `web/packages/app/src/pages/sections.tsx` | 保留 | 原呈现树、选择、作用域、预览及Widget实例生命周期 |
| `web/packages/app/src/pages/layout.ts` | 保留并吸收迁来的纯布局诊断 | 原profile尺寸/布局约束只在Web解释 |
| `web/packages/app/src/pageui/types.ts` | 由清单生成的新Web视觉类型 | 取代Go专用视觉类型；不手写第二份schema |
| `web/packages/app/src/pageui/contract.ts` | 新增的原清单验证实现 | 复用schema/字段解析，不做新执行器或权限目录 |
| `web/packages/app/src/pageui/contract.test.ts` | 新的视觉测试归属文件 | 承接表B中的纯视觉情景，安全仍留Go |
| `capabilities/server/cmd/api-types/main.go` | 保留生成入口，删除Go导出视觉清单步骤 | 只生成安全宿主API；视觉类型由原Catalog生成链处理 |
| `scripts/catalog.mjs` | 保留生成链并接入Web视觉源 | 原Catalog身份/owner与校验，不增加运行注册表 |
| `web/packages/kernel/src/gen/host.ts` | 原工具重新生成 | 仅宿主typed契约；不得手改保留旧Go视觉结构 |
| `web/apps/catalog/src/gen/` | 原工具重新生成 | 新来源路径、原owner和示例一致 |
| `web/e2e/tests/kit.ts` | 改读Web呈现profile来源，原builder/runtime/editor入口保留 | 不手写登录、对象、发布或行业夹具 |

### D. 能力迁移验收矩阵

| 删除族 | 必须仍然可用的能力 | 原拒绝/保留边界 |
|---|---|---|
| 布局、chrome、输入、图表与卡片 | 原77项注册及全部现有props/视觉角色；宽窄布局、插槽、unused、tabs/overlay、撤销/源码 | 未知配置保留；原scope与query预算不得移入raw |
| record/query/collection/model | 对象、集合、interface、派生值、矩阵、关系、过滤、分页、排序、聚合、精确版本 | 当前行字段授权、完整/withheld、固定query及展开上限 |
| 工作、AI、Compute、动作 | 原记录动作、参数映射、审批、Work/Flow和原函数/Operation调用 | 写入仍经接受结果；原版本、动作限制和未知结果键 |
| 空间、文件、附件与协作 | 地图、标注、Scene3D、PDF、历史、评论、附件原关系及资源清理 | 文件和业务记录均获权，样本确切reference，自包含GLB，URL/CSP不放宽 |
| 历史/冻结与纯计算 | 旧页、旧profile、旧包/候选/嵌入和同源Formula/规则联动 | 旧原件摘要不变；服务端最终裁决；无云端或I/O回退 |
| 声明本地化 | 同一已发布应用中英文菜单、typed字段和WidgetSpec声明文字切换 | 词典草稿不影响运行；租户覆写/owner冲突只由原translator处理；业务值、英文原件与候选字节不变 |

### E. 低层UI与kernel视觉类型消费者

此表覆盖审计时 `Api.Page*|PageUI|pageUIManifest` 的全部35个UI文件；全部保留真实能力，不向app建立反向依赖。目标路径为原文件自身；app层 `web/packages/app/src/pageui/types.ts` 与原widgets适配器消费UI公开props并传入有界数据。资源、文件字节和原记录读取限制不得因视觉类型退出kernel而删除。

| 现有完整路径 | 操作和目标完整路径 | 原责任 |
|---|---|---|
| `web/packages/ui/src/charts/CollectionCounts.tsx` | 原位保留重构 → `web/packages/ui/src/charts/CollectionCounts.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/charts/CountMatrix.tsx` | 原位保留重构 → `web/packages/ui/src/charts/CountMatrix.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/charts/spec.ts` | 原位保留重构 → `web/packages/ui/src/charts/spec.ts` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/AssetDirectory.tsx` | 原位保留重构 → `web/packages/ui/src/components/AssetDirectory.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/BreadcrumbTrail.tsx` | 原位保留重构 → `web/packages/ui/src/components/BreadcrumbTrail.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/ButtonGroup.tsx` | 原位保留重构 → `web/packages/ui/src/components/ButtonGroup.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/IndexedChoices.tsx` | 原位保留重构 → `web/packages/ui/src/components/IndexedChoices.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/MultipleChoiceInput.tsx` | 原位保留重构 → `web/packages/ui/src/components/MultipleChoiceInput.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/NotificationList.tsx` | 原位保留重构 → `web/packages/ui/src/components/NotificationList.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/StaticImage.tsx` | 原位保留重构 → `web/packages/ui/src/components/StaticImage.tsx` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/components/TermCounts.tsx` | 原位保留重构 → `web/packages/ui/src/components/TermCounts.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/choice.ts` | 原位保留重构 → `web/packages/ui/src/components/choice.ts` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/external-frame.ts` | 原位保留重构 → `web/packages/ui/src/components/external-frame.ts` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/components/histogram-data.ts` | 原位保留重构 → `web/packages/ui/src/components/histogram-data.ts` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/components/static-image.ts` | 原位保留重构 → `web/packages/ui/src/components/static-image.ts` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/layout/LayoutRegion.tsx` | 原位保留重构 → `web/packages/ui/src/layout/LayoutRegion.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/ApprovalInbox.tsx` | 原位保留重构 → `web/packages/ui/src/records/ApprovalInbox.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/ColumnPresentation.tsx` | 原位保留重构 → `web/packages/ui/src/records/ColumnPresentation.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordActionGrid.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordActionGrid.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordAvatarStack.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordAvatarStack.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordCard.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordCard.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordComparison.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordComparison.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordEvents.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordEvents.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordGantt.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordGantt.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordLeaderboard.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordLeaderboard.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordNeighborhood.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordNeighborhood.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordResourceList.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordResourceList.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordScatter.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordScatter.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/RecordSparkline.tsx` | 原位保留重构 → `web/packages/ui/src/records/RecordSparkline.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/Records.tsx` | 原位保留重构 → `web/packages/ui/src/records/Records.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/SearchAround.tsx` | 原位保留重构 → `web/packages/ui/src/records/SearchAround.tsx` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |
| `web/packages/ui/src/records/attachment-bytes.ts` | 原位保留重构 → `web/packages/ui/src/records/attachment-bytes.ts` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/records/pdf-engine.ts` | 原位保留重构 → `web/packages/ui/src/records/pdf-engine.ts` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/records/useRasterAttachment.ts` | 原位保留重构 → `web/packages/ui/src/records/useRasterAttachment.ts` | 保留原安全URL、文件/图像/预览字节预算及取消/释放；去掉对Go视觉清单的依赖 |
| `web/packages/ui/src/spatial/scene-model.ts` | 原位保留重构 → `web/packages/ui/src/spatial/scene-model.ts` | 保留原primitive props/呈现与获权记录源；通用安全预算由调用方显式传入，不导入app |

| `web/packages/kernel/src/widget-authoring.ts` | 迁移并删除原路径 → `web/packages/app/src/pageui/types.ts` | 以同一Web清单推导视觉配置与端口适配类型，不依赖Go Section视觉结构 |
| `web/packages/kernel/src/widget-authoring.test.ts` | 迁移并删除原路径 → `web/packages/app/src/pageui/contract.test.ts` | 原类型/清单推导覆盖随owner移动 |
| `web/packages/kernel/src/index.ts` | 保留，移除视觉清单及authoring类型导出 | 仍导出通用宿主安全契约，不引入app依赖 |
| `web/packages/build/src/workshop/page-editor/draft.ts` | 保留，视觉类型经原 `@platform/app` 入口消费 | 一份可视化/源码草稿，不复制旧Go Section类型 |

### F. 本地化owner与跨表一致性补充

| 当前审计路径 | 最终责任与物理目标 | 不得重新实现的语义 |
|---|---|---|
| `capabilities/server/translator.go` | 总清单 → `capabilities/server/kernel/transport/transport_declarations.go`；继续提供已发布owner有效词典快照 | 原租户覆写、owner冲突、大小写/模式解释和缓存失效；不递归改WidgetSpec |
| `capabilities/server/languages.go` | 总清单 → `capabilities/server/kernel/transport/transport_declarations.go`；只翻typed声明元数据 | 英文源及业务值不改；原语言选择和管理员词汇维护资格 |
| `capabilities/server/view_cache.go` | 总清单 → `capabilities/server/kernel/transport/transport_declarations.go`；原发布/权限/定义世代失效 | 不新增第二份词典存储或以草稿失效正式词典 |
| `capabilities/server/routes_discovery.go` | 总清单 → `capabilities/server/composition/http_routes.go`；原获权发现响应携带语言结果投影，语义归原owner | 不开放管理员/v1/words给普通成员，不把投影当授予 |
| `web/packages/ui/src/i18n/index.ts` | 原位保留；原t消费当前获权且已解决的owner快照 | 只做声明查找/插值，不重新决定覆写、冲突或跨owner全局注册 |
| `web/packages/app/src/pages/sections.tsx` | 原位保留；WidgetSpec声明字段在原呈现owner取同一发布词典显示 | 不修改RawMessage、候选/页摘要、原业务值或未发布草稿 |
