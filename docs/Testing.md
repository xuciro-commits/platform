# 测试

验证方法与人工走查入口。环境、账号和服务地址归 [部署说明](../deploy/local/README.md)。

## 检查选择与停止

自动测试集中在契约、业务行为、权限、数据一致性和恢复；浏览器只保留规范入口与关键构建/操作路线。外观、布局、画布拖动、一般交互和体验由截图检查及负责人走查。不为颜色、面板宽高、拖动距离或每种设备/语言复制自动路线。

| 变更 | 适用检查 |
|---|---|
| 文档、队列、AI 规则 | 链接/引用、语义一致性、`git diff --check` |
| 内核规范/向量/实现 | `scripts/verify.sh contract`；受影响的 Rust/TypeScript 边缘检查 |
| Lean 模型/工具链 | `scripts/verify.sh formal`；映射与可信前提见 [Lean README](../contract/lean/README.md) |
| Go 宿主 | `scripts/verify.sh capabilities format`；改变应用 API/组合时加 `composition` |
| 应用/协议 | `scripts/verify.sh composition format`；MES 规则加 `mes`，PMS 桌面/离线链加 `pms` |
| Web 日常编码与同类组件批次 | `scripts/verify.sh web-check`：生成一致性、Catalog、`pnpm check` 的类型、单元测试与构建；开发中按问题选择原 owner 的 `go test` 和单元测试 |
| 成组交付、重要集成或发布节点 | 按影响面选择一条联合浏览器路线；需要全量验收时 `scripts/verify.sh web` 共用一次完整 Playwright，不按每个组件重复 |
| 纯样式/布局/文案 | `pnpm --dir web check`，启动查看并截所改页面；不要求重跑浏览器业务路线 |
| 提交/恢复/激活语义或部署 | 对应持久化/故障检查及 `scripts/verify.sh deploy`；纯发布导航不自动触发 |

通过后只有新代码、失败修复或环境变化才重跑；文档更新不是重跑理由。新增测试先确认它能发现哪种实际回归、哪一层是规范主人；已由服务端/契约证明的规则不在浏览器逐项重测。

## 自动检查的归属

| 被测能力 | 规范证据入口 |
|---|---|
| K1–K9 语义及跨语言边缘 | `contract/spec`、`contract/vectors`、`contract/go`、Rust/TypeScript K5；选定 K4 Lean 模型 |
| 声明/读取/动作/授权与派生来源 | `capabilities/server` 的对应 Go 测试与应用组合探针 |
| 原子提交、版本和恢复 | 对应 accepted-result / release / process / function 测试；需要 PostgreSQL 的用例不能用内存通过代替 |
| 业务与协议 | 各应用/协议测试及 `CheckReplay`，酒店/制造组合 |
| 共享组件行为 | `web/packages/ui`；翻译完整性由 i18n 检查 |
| Catalog 一致性与查询 | `scripts/catalog.mjs check` 已接入 Web：公共导出/示例/翻译/Widget 引用与私有导入；`@platform/catalog` 验证有界查询，Catalog 预览隔离和 Studio 草稿路线只保留核心行为 |
| 代码编译与计算装配 | `compute`/`assembled_compute` 行为检查；真实 Go/TinyGo profile、私有 socket 与 worker 需要部署 README 的运行环境，显式启用后验证，不能用默认跳过当通过 |
| 浏览器主路径 | `web/e2e/tests`：动作/刷新、审批、字段安全、拒绝保留输入与独立业务主管、只读预览、对象/页面/应用编写、编辑器未保存保护、租户动作、流程/函数编写及共同候选保存后刷新续接/激活/操作、两行业原生记录建议与收件箱返回 |
| OIDC 与持久部署 | `deploy/local/rehearse.sh`、`web/e2e/deploy`；在部署边界改变或发布检查点运行 |

共享读取的固定次数断言限定在同一成员/定义范围及数据修订。相关浏览器路线使用host.ts的stableReadRevision固定测试段的变更流，涉及后续动态发布时恢复真实流；真实租户变更允许重新读取，不能把它计成同一世代的重复请求。业务动作/刷新路线仍使用真实通知，会话用例验证revision变化后的失效与同步重入合并。

浏览器历史路线号只是检索标识，已删除的次要路线转人工验证，不表示能力删除。固定计划、关系/查询、关联创建和函数夹具的语义由 Go 回归；保留浏览器主路径，避免逐功能重复完整生命周期。

## 截图与人工走查

Playwright 默认仅失败时截图。需要当前主路径截图时，在已构建工作区后选择相关测试运行：

```sh
pnpm --dir web/apps/workspace build
PLATFORM_SCREENSHOTS=1 pnpm --dir web/e2e exec playwright test --grep 'compose a page'
```

工坊与流程导航截图选 `workflow.spec.ts`，原生记录建议的普通/窄屏截图选 `record-work.spec.ts`。截图在 `web/e2e/test-results`。这是供人看的图片，不做像素/尺寸断言，也不构成负责人认可。界面改动只检查受影响页面的普通及窄屏状态；不要求每次重复全部中文、键盘、设备组合。集中验收时负责人实际完成以下任务，反馈形成简短待办，修复后仅复核相关问题。

| 任务 | 操作与结果 |
|---|---|
| 构建与交付 | 构建者在工坊选择对象、编辑字段/状态/权限，组合页面和流程，保存固定计划，测试后审查并保存候选；沿当前支持的发布/激活路径交付，操作员完成记录动作或收件箱任务 |
| 酒店销售 | 建客户/商机，预留住宿或团队房间，确认与拒绝原因可理解；无外部提供方时走人工答复 |
| 制造与 ERP | 生产订单下达、SFC 流转/不合格处置、跨应用确认；ERP 凭证平衡/连续编号、采购审批及收货记账 |
| 人工协同 | 请假提交、审批、驳回/再提交和代办，申请人与审批人看到正确状态；私有字段只对授权角色可见 |
| AI 与知识 | 配置启用模型，运行建议/智能体，查看依据、回答、拒绝、用量与待人工确认事项；检索与引文遵守当前来源权限 |
| 工作区与辅助操作 | 导航/返回上下文、未保存提醒、搜索、文件、评论、导入导出和异常提示；一般布局、窄屏、中文、键盘及画布手感由人判断 |
| 企业模型（ADR-0067） | 控制面板 → Enterprise：空租户先走向导（名称、人数、站点、法人、行业）得到模板；画布拖入 Palette 元素、Link 连两元素只给相容关系、保存视图；树/表切换、截至日期回看；检查器改名/关闭/添加成员/共享开关；成员详情页"Organisation"面板与 `/v1/organization` 投影一致；业务动作里 `ref:"enterprise.element"` 字段出现元素下拉；Patterns 页签选中组织后 Add a Plant/Hotel/Department（改旋钮、看预览大纲）→ 树中出现子树；空模型页"从一个单元开始"也能建模型；画布滚轮缩放/拖动平移/适应、"排布"切换四种布局、Link 模式从元素拖到元素、每类元素有图标 |
| 集成织物 Ⅰ-A/B（ADR-0070） | 构建器 → Ontology → Connections：新建 http/OData/postgres 连接（地址不含密码、密钥名）→ Check 数秒内出"可达/失败"与详情；Data source 选就绪连接后 Profile 随种类收窄（OData 实体集/过滤/增量列、数据库表/WHERE 片段、CSV）；Publish/Pull now 后 Last pull 计数，增量列有值时显示游标，Reset cursor 重拉；对未就绪连接或 `1=1; drop` 式过滤发布被拒 |
| 集成织物 Ⅰ-C/D（ADR-0071） | 构建器 → Ontology → Datasets：新建数据集；Data source 选 "Rows go to: A dataset" 指向它并 Publish/Pull now → 数据集出现 v1、Schema 推断、Rows 预览；再建 Pipeline：输入该数据集，加 filter/cast/compute/aggregate 等步骤与 notnull/range 期望，输出另一数据集或对象 → Publish 后数秒内 Last run 显示行数/写入/隔离（隔离行带原因）；再次 Pull 数据源出 v2 时管道自动再跑，同版本不重跑；输出对象时记录在对象页可见且重复运行不重复建 |
| 集成织物 Ⅰ-E（ADR-0072） | 构建器 → Ontology → Writebacks：新建回写（对象 + create/动作名、就绪的 http/OData 连接、路径 `A_MaterialDocumentHeader`、请求体映射、应答映射 `d.MaterialDocument → docno`）→ Publish；在对象页新建一条记录 → 设置 → 集成 里出现 `connection:<id>` 端点的效果并送达，回写页 Deliveries 计数 +1、最近应答可见，记录的 docno 被填上；把目标地址改成不可达再建一条 → 效果 retrying、回写页显示排队数，恢复后同一键只送达一次；对象类型 → Data 页能从字段点到管道、数据集、数据源、连接 |
| 集成织物 Ⅰ-H（ADR-0073） | 构建器 → Ontology → Datasets：建数据集 `sapom`，粘贴/加载几行 `{objid, stext, parent, costcenter}`；Pipelines：新建管道，输入 `sapom`，Write to = The enterprise model，Source system `sap-om`、Stereotype ActualOrganization、Id 列 objid、Name 列 stext、Kind `=department`、Parent 列 parent、Root = 公司元素 id、Placed in kind = 组织树的关系类别（如 management）→ Publish → Run now；企业模型组织树里出现 `sap-om:*` 单元挂在公司之下，元素属性中有 `source:sap-om.costcenter`；去掉一行再加载并运行 → 该单元在"今天"关闭（Until），其余不动；没有企业模型 admin 角色的发布者运行时，管道 Last run 报错说明需要该角色 |
| 集成织物 Ⅰ-G（ADR-0074） | 构建器 → Ontology → Matching rules：新建规则，对象 = 物料类对象，键 `partno` 归一化 digits，优先来源 `description → sapmaterials` → Publish；两条管道（SAP、MES）都把对象写为输出；先运行 SAP 管道落 `000000123`，再运行 MES 管道落 `M-123`（带 weight）→ 对象列表仍是一条 `000000123`，描述是 SAP 的、重量是 MES 的，MES 管道最近一次运行显示"1 行由匹配规则合并"；Pause 规则后再落 `M-123` → 变成第二条记录 |
| 集成织物 Ⅰ-I（ADR-0075） | 构建器 → Datasets：把 HR 数据集标记为 confidential；Pipelines：建一条写入 Employee 对象的管道 → Publish 被拒并列出未指明读者的字段；对象类型里给这些字段设 Read by = hr 并发布 → 管道可发布、运行；输出到另一数据集的管道运行后该数据集自动变为 confidential，管道标题旁出现标记；把数据集改为 restricted → 列表的 CSV 导出被拒；给一名成员 build 角色 `integrator` → 左栏只有 Integration 分组，能建连接/数据源/管道，看不到对象类型与页面 |

权限、数据、版本及恢复保证不能仅交给截图证明。人工走查负责可用性和任务体验；自动回归负责其规范边界。负责人认可只在相关 ADR 简短记录日期与范围，不为每次工程增量重新验收。

## 发布与演进边界

对象/页面/应用/流程/代码候选支持冻结安装；AI 版本/评测仍归函数 owner。拒绝、追加失败及追加后应用前崩溃保留旧运行或恢复已接受结果；通用升级迁移、退役和客户扩展保留未完成，边界归 ADR-0039。隔离编译/计算不等于候选数据的物理沙箱。

阶段集成按 Platform §10.6 核对两行业完整旅程、权限、隔离、在途版本及恢复；真实模型质量使用实际任务和供应商评测，替身只证明协议和治理。无真实客户或生产数据时标为内部演练。当前能力边界查 ADR，本文不保存旧检查点的“当时尚未完成”清单。
