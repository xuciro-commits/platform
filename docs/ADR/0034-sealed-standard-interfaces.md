# ADR-0034 标准接口封存：query / ref / write 三类契约与生成式 SDK

<a id="adr-0034"></a>

状态：有界实现；query/ref/write 共用原企业模型与宿主授权，生成式 SDK 随原声明更新。模型生命周期与来源责任归 ADR-0022，视图与使用呈现归 ADR-0031。

## 背景

- **引用与写入早有半套机器，查询没有**：记录字段的 `ref:"enterprise.element" stereo:"…"`（ADR-0022 D8）、动作载荷的 `Field.Ref/Stereotype`（ADR-0038）、内核决策时的校验（`Ledger.checked`：存在性 + stereotype，重放不再校验）、反向读 `/v1/enterprise-references`（ADR-0031 D4）都在。缺的是**带条件的类型化读**：ElementPicker 至今拉全量 `/v1/enterprise` 再在客户端过滤——模型一大就是反模式；已存的 ref 也没有独立的解析读，只能靠全量模型找回名字。
- **写入目录只活在 Go 里**：`enterprise.*` 的 13 个动作是模型的唯一写入面，但手写 UI、AI、脚本之外的消费者没有一份可 import 的类型——调研里的 Palantir Ontology SDK（从模型生成的类型化客户端）正是这个形态。
- **『建模 → 被消费』没有证据**：模型画得再多，除建模页外没有任何页面读它。MES 的主数据（工单、工艺、工作中心）与模型的场地层级（plant 模式种出的车间/产线/库房/仓位/设备，ADR-0031 已把种子侧收口）同形，却互不知道。

## 决定

### D1 Query 契约 = `GET /v1/enterprise-query`（主机带参路由）

`Route{Answer: EnterpriseQueryResult}`，与 `/v1/enterprise-references` 同族（`rt.handle`，进 OpenAPI，类型随之生成进 host.ts）。**带参读走主机路由而非 app Read**：app 的 `Read(c, name)` 无参数位；先例即 ADR-0031 D4 的反向读。

| 参数 | 语义 |
|---|---|
| `stereotype` | 逗号分隔，命中任一即取（UAF stereotype） |
| `kind` | 精确匹配 `Element.Kind` |
| `q` | 对 id / name / shortName / kind 的大小写不敏感子串 |
| `alive` | 缺省 = 当天，按有效区间读取；`all` = 含已关闭与未来，但不包含已删除元素；显式 `YYYY-MM-DD` 读取该日有效集并保留重开前的停用区间 |
| `limit` | 缺省 200，上限 1000；结果按 id 排序 |

- 投影 DTO `EnterpriseQueryElement`（id/stereotype/name/kind/shortName/legal/from/until/periods/closed）只带消费所需字段——不搬关系、视图、日历。有效期投影包含重开后的历史区间，旧单区间描述仍可读取。
- 授权与反向读同口径：`t.admits(m)`（本租户成员即读；模型为全员可读的既定口径）。
- 应用内实现挂在 `*Enterprise.Query(...)`：主机路由只做参数解析与分发，模型的锁与投影归 enterprise 自己。

### D2 Ref 契约 = 声明 → 校验 → 解析 → 反向，四件套补齐「解析」

1. **声明（同一口径）**：记录字段 struct tag `ref:"enterprise.element" stereo:"…"` 或 `stereos:"…,…"`；动作载荷 `platform.Field{Ref,Stereotype,Stereotypes}`。生成实体动作传播原限制。Build Field/Input 以同名字段保存，并编译到相同宿主声明；企业元素不是普通记录类型或单独资产。
2. **校验（原宿主统一）**：动作和记录新设/改换引用按存在性、有效期及单类/多类限制检查；编辑其他字段不重新拒绝未变化的旧关闭引用。记录路径支持嵌套明细行；按原引用逐条匹配，允许重排和修改其他列，但不为新增的重复明细行放行。旧日志重放不追加当前声明检查，保留原接受行为。
3. **解析（新）**：`GET /v1/enterprise-resolve?ids=a,b,c`（≤100 个）→ 与 query 同投影，**含已关闭元素并标 `closed`**——存下来的引用永远读得出名字（即 ElementPicker 的 kept 规则的读侧表述）；未知名 id 静默跳过。
4. **反向（既有，并入契约文档）**：`GET /v1/enterprise-references?element=…`（ADR-0031 D4）——「谁引用了它」。

消费面规则：动作表单和通用记录选择器都使用原 query+resolve 读取企业引用；新值只能从有效候选选择，旧关闭值解析原标签并保留。查询失败不降级到普通记录列表、不清空草稿，也不自动选择同名人员。`core.person.element`、`core.location.place` 是明确可选关联，不触发主数据迁移或同步。

### D3 Write 契约 = `enterprise.*` 决策动作目录，唯一写入路径

目录即契约：`declarations()` 单源声明 `element.add/edit/close/reopen/reinstate/delete`、`kind.add/edit`、`relationship.add/end/change`、`view.save/delete`、`tenant-link.save/remove`、`slice.import/sync`、`pattern.apply`、`seed`。手写 UI、SDK、AI、脚本一律经原 `decide()`；新增生命周期与来源动作仍由同一模型及原策略检查，不增加第二个写入系统。只读投影与记录字段不构成写入面。

保存 TenantLink 只关联本地组织、来源 tenant、共享 root 和可读 ready HTTP Connection。`POST /v1/enterprise-links/{id}/preview` 在锁外读取固定来源路径，返回 `{link,base,content,connectionRevision,slice,changes}`，不持久化；确认沿 `enterprise.slice.import` 提交这些原值，检查模型与连接未变化且内容摘要相符，不再 fetch。预览与提交复用原关系端点/有效期/循环校验。失败只返回原拒绝，不建立待导入缓存、调度器或跨租户写权限。

`GET /v1/enterprise-usage?element=<id>&until=<date>` 回答 `{canDelete,canClose,hasRestrictedUsage,items}`。使用判定检查当前与保留历史，但细节沿原成员读取；不可见使用只标记存在。`enterpriseUse` 为原对象当前记录上的纯布尔 Expr；未设置时未归档记录均视作当前使用。它随原 Build 草稿/发布保留，不引入业务状态名约定。

### D4 生成式 SDK：`go run ./cmd/api-types` 的第二个产物

`apps/enterprise.SDK()` 机械生成 `web/packages/kernel/src/gen/enterprise-sdk.ts`：

- **联合类型**：`EnterpriseStereotype`（档案 18 条）、`EnterpriseViewpoint`（五视角）——元模型即类型。
- **写入面**：`enterpriseWrites` 常量 + `EnterpriseWrite` 联合 + 每动作一个载荷类型（`EnterpriseElementAddPayload` 等；required 字段必填，`element.add` 的 `stereotype` 字段特化为 `EnterpriseStereotype`，`view.save` 的 `viewpoint` 特化为 `EnterpriseViewpoint`）+ `enterpriseWrite(decide, schema, target, payload)` 类型化包装（`EnterpriseDecide` 为结构最小的三参 decide，任何宿主的 decide 都能赋进来）。
- **读取面**：`queryElements(get, filters)`、`resolveElements(get, ids)`——注入与 `client.get` 同形的 `get`，不依赖 React、不依赖任何具体宿主。
- **管线**：与 host.ts 同一条命令（`cmd/api-types` 加 `-sdk` 路径旗标，默认写同一 gen 目录）；`TestAPIContract` 同时把两份文件钉成 staleness 测试——任何一侧过期即测试失败，提示同一条再生成命令。kernel `index.ts` re-export 整包。

### D5 MES 首个消费者（mes_reads）

1. **`mes.order.release` 载荷 + `place`**（可选，`Ref: enterprise.element`，`Stereotypes: [ActualLocation,ActualOrganization]`）同时支持演示模型中的组织单元与模式中的位置。原 `checked` 校验存在性、有效期和引用族，不新增 MES 私有选择器或引用校验。
2. **`Order.Place` 记录字段**（同 ref tag）随决策持久化：引用落在记录上（ledger 重放保真），反向读立刻可见（`/v1/enterprise-references?element=…`）。
3. **发布工单对话框新增「Place in the model」下拉**：选项来自 SDK 的 `queryElements(client.get, { stereotype: ["ActualLocation", "ActualOrganization"] })`（多 stereotype 是 query 契约的原生能力）——主数据表单里第一次出现企业模型元素，即『建模 → 被消费』的证据。
4. **ElementPicker 改道**：从全量 `/v1/enterprise` 客户端过滤改为 `queryElements`（存活集）+ `resolveElements`（既存值，含已关闭的标注）——同一语义（只可选存活元素、已关闭的既存值保留并标注），数据量从全模型降为投影。

## 后果

- 三类契约各有文档（本 ADR）、有类型（host.ts + enterprise-sdk.ts）、有测试（staleness + 语义测试）：改声明不重新生成 → 测试红；绕过 decide 的写入不存在；无条件全量读只剩画布等真需要整图的消费者。
- 查询/解析的页大小与 id 上限是契约的一部分：大模型下带 `q` 的搜索式选择器（picker 里打字检索）未做——当前按 stereotype 过滤后规模有限，等真实痛点触发。
- **MES 主数据自身仍是内存只读种子**（ADR-0032 行的既有欠账）：本块的消费证据是「发布工单挂到模型场所 + 记录反向可查」，不是主数据 CRUD；主数据落库/编辑归 seed_ref 方向，不因本块冒充已具备。
- `web/packages/build` 的选根下拉、pipeline 里 `enterprise.elements` 的全量过滤等既有全量消费点未改写（候选，等触及再迁，不铺一次性搬运）。
- Core、Build 字段与动作输入现已使用同一企业 query/ref 能力；不在没有需求时搬迁其他全模型读取点。

## As built

生成 SDK 包含现行生命周期、结构改名与来源关联动作；带连字符的 schema 只影响生成类型标识符写法，不改变原动作名。Go 定向检查覆盖 query/resolve 日期区间、源 HTTP 预览与陈旧拒绝、Core 旧引用保留、Build 字段/输入限制和候选依赖；API 生成与双语检查沿原入口。Enterprise 浏览器路线由 Testing 记录实际结果，未运行或未观察的部分不在此声称通过。UAF 后续支持边界仅引用 ADR-0022，不将完整 OCL、XMI 实例互换或跨版本迁移列为已实现。
