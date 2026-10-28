# Proposal

## Why

页面呈现已反向进入应用公共 Go API：`platform/page*.go` 实测有 78 个非测试文件、85 个测试文件，混合了尺寸、颜色和组件配置与真实的查询、动作、字段和作用域约束。需要移走前者、保留并收敛后者；浏览器规则 WASM 已存在，应补齐纯公式与客户端联动，而不是另建求值器或把独立制品 Worker 当浏览器编译入口。

## What Changes

- **BREAKING** 新作者页面采用版本化紧凑声明：`DataBinding`、`ActionTrigger`、`InputSchema`、`ScopeVisibility` 及有界不透明 `WidgetSpec`；身份、版本和确切原字节仍属于原页面资产。Go 不识别具体视觉组件、尺寸、颜色及排版树，查询、动作、输入类型、来源、资源及权限约束保持类型化。
- 将 77 项 Widget 清单的视觉部分、111 个既有 profile 的呈现解释及专项视觉检查迁到 Web 原页面 owner；替换后的 Go 页面契约集中到 `page_model.go`、`page_queries.go`、`page_rules.go`。设计附表逐一覆盖全部 163 个现有 Go 页面文件，删除以真实能力迁移和原测试归属完成为条件，不把搬文件当能力退出。
- 现有页面、候选、包及接受历史不重写。旧格式通过版本化纯投影取得安全绑定；未知、无法投影或不受支持内容保留原件并明确不可用，不能把安全字段塞进不透明配置而跳过校验。
- 已交付的应用中英文本地化继续保留：宿主只翻类型化元数据，Web原呈现owner使用同一已发布有效词典展示不透明配置中的声明文字；不改变英文原件或业务值，不另建词典存储或重复覆写/冲突逻辑。
- 复用 `internal/rules`、`cmd/rule-browser` 与现有浏览器 WASM 接口；把旧公式语法降级转换收进同一纯编译器，分离服务端获权事实捕获、结果赋值与纯求值。`cmd/wasm-worker` 继续承担隔离制品执行，不合并进浏览器或改成业务授权来源。
- **BREAKING** 直接 API 的发布检查不再承诺 Go 验证视觉属性；正常作者、导入及渲染入口由同一 Web 清单检查并保留诊断。安全绑定和资源边界仍由宿主独立拒绝。此取舍及旧客户端升级方式须在实施前审阅确认。

## Capabilities

### New Capabilities

无；不增加按本次任务命名的永久模块。

### Modified Capabilities

- `experience/workspace`：页面呈现清单归 Web，安全绑定与不透明呈现分离，原作者及运行入口保留完整能力。
- `experience/professional-visualization`：专业视图的配置解释归前端，原获权数据、文件与资源限制继续由各 owner 执行。
- `platform/application-lifecycle`：紧凑页面及历史投影随原候选冻结，不能将依赖或安全声明降为不透明 JSON。
- `platform/business-models`：公式降级和浏览器联动复用同源纯规则实现，事实读取与正式裁决仍由原宿主承担。

## Impact

涉及 `capabilities/server/platform/page*.go`、`definition.go`、`installed.go`、`definition_page_projection.go`、原页面读取/导航/查询/计算 owner，Build 页面草稿与包/候选读写，以及 Web 运行、作者注册、生成类型和检查工具。完整逐文件映射及实测依据在 `design.md`。

负责人已授权实施，低层UI与Web作者类型已迁移；Go五要素、旧安全投影和后端视觉删除仍未完成，具体状态见tasks。后端文件减半与运行负载端侧化是待实现/测量目标；附件中的 Token 倍数、运行流量比例及模型成本不是本仓库已证实性能。纯 Go 规则测试和同源 WASM 的 32 项既有向量仅证明所测语义，不代表页面迁移或生产性能完成。
