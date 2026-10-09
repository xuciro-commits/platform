# ADR-0094 标准接口封存：query / ref / write 三类契约与生成式 SDK

状态：已落地（2026-10-09）；回应 Round-3 子块④：「设计已定：**query / ref / write 三类契约**（写 = 现有 `enterprise.*` 决策动作集，读 = 类型化查询与 `enterprise.element` 解析）+ **从元模型生成的 typed SDK**（api-types 同款管线）→ **MES 主数据下拉作为第一个消费者**（mes_reads），打通『建模 → 被消费』的闭环。」

## 背景

- **引用与写入早有半套机器，查询没有**：记录字段的 `ref:"enterprise.element" stereo:"…"`（ADR-0067 D8）、动作载荷的 `Field.Ref/Stereotype`（ADR-0028 D5）、内核决策时的校验（`Ledger.checked`：存在性 + stereotype，重放不再校验）、反向读 `/v1/enterprise-references`（ADR-0085 D4）都在。缺的是**带条件的类型化读**：ElementPicker 至今拉全量 `/v1/enterprise` 再在客户端过滤——模型一大就是反模式；已存的 ref 也没有独立的解析读，只能靠全量模型找回名字。
- **写入目录只活在 Go 里**：`enterprise.*` 的 13 个动作是模型的唯一写入面，但手写 UI、AI、脚本之外的消费者没有一份可 import 的类型——调研里的 Palantir Ontology SDK（从模型生成的类型化客户端）正是这个形态。
- **『建模 → 被消费』没有证据**：模型画得再多，除建模页外没有任何页面读它。MES 的主数据（工单、工艺、工作中心）与模型的场地层级（plant 模式种出的车间/产线/库房/仓位/设备，ADR-0093 已把种子侧收口）同形，却互不知道。

## 决定

### D1 Query 契约 = `GET /v1/enterprise-query`（主机带参路由）

`Route{Answer: EnterpriseQueryResult}`，与 `/v1/enterprise-references` 同族（`rt.handle`，进 OpenAPI，类型随之生成进 host.ts）。**带参读走主机路由而非 app Read**：app 的 `Read(c, name)` 无参数位；先例即 ADR-0085 D4 的反向读。

| 参数 | 语义 |
|---|---|
| `stereotype` | 逗号分隔，命中任一即取（UAF stereotype） |
| `kind` | 精确匹配 `Element.Kind` |
| `q` | 对 id / name / shortName / kind 的大小写不敏感子串 |
| `alive` | 缺省 = 当天（`from ≤ day < until`）；`all` = 不过滤（含已关闭与未来）；显式 `YYYY-MM-DD` = 当天存活集 |
| `limit` | 缺省 200，上限 1000；结果按 id 排序 |

- 投影 DTO `EnterpriseQueryElement`（id/stereotype/name/kind/shortName/legal/from/until/closed）只带消费所需字段——不搬关系、视图、日历。
- 授权与反向读同口径：`t.admits(m)`（本租户成员即读；模型为全员可读的既定口径）。
- 应用内实现挂在 `*Enterprise.Query(...)`：主机路由只做参数解析与分发，模型的锁与投影归 enterprise 自己。

### D2 Ref 契约 = 声明 → 校验 → 解析 → 反向，四件套补齐「解析」

1. **声明（既有，一处口径）**：记录字段 struct tag `ref:"enterprise.element" stereo:"…"`；动作载荷 `platform.Field{Ref, Stereotype}`。生成的实体动作从 struct tag 传播两者——同源，不另立声明方式。
2. **校验（既有，内核统一）**：决策时 `Ledger.checked` 先查 `Readable(element/<id>)`（不存在 → INVALID_ARGUMENT），再查 `rt.Element` 的存活与 stereotype（不符 → 带话的 INVALID_ARGUMENT）；重放不再校验（K6）。记录字段另经 `checkEnterpriseReferences`（记录写入路径同规则）。
3. **解析（新）**：`GET /v1/enterprise-resolve?ids=a,b,c`（≤100 个）→ 与 query 同投影，**含已关闭元素并标 `closed`**——存下来的引用永远读得出名字（即 ElementPicker 的 kept 规则的读侧表述）；未知名 id 静默跳过。
4. **反向（既有，并入契约文档）**：`GET /v1/enterprise-references?element=…`（ADR-0085 D4）——「谁引用了它」。

消费面规则：动作表单的 ref 字段由 ElementPicker 呈现（本块改走 query+resolve）；记录内联编辑的 `RecordLookup` 不动（enterprise.element 的记录型内联编辑不是本块对象）。

### D3 Write 契约 = `enterprise.*` 决策动作目录，唯一写入路径

目录即契约：`declarations()`（自 `New()` 抽出，单源）声明的 13 个 schema——`element.add/edit/close`、`kind.add`、`relationship.add/end/change`、`view.save/delete`、`slice.import/sync`、`pattern.apply`、`seed`。手写 UI、SDK、AI、脚本一律经 `decide()` 进入；顺序 = 策略 → `checked`（choices/引用）→ 应用规则（K6），校验代码零新增。只读投影（`ReadModel`）与 `readonly` 记录字段不构成写入面。

### D4 生成式 SDK：`go run ./cmd/api-types` 的第二个产物

`apps/enterprise.SDK()` 机械生成 `web/packages/kernel/src/gen/enterprise-sdk.ts`：

- **联合类型**：`EnterpriseStereotype`（档案 18 条）、`EnterpriseViewpoint`（五视角）——元模型即类型。
- **写入面**：`enterpriseWrites` 常量 + `EnterpriseWrite` 联合 + 每动作一个载荷类型（`EnterpriseElementAddPayload` 等；required 字段必填，`element.add` 的 `stereotype` 字段特化为 `EnterpriseStereotype`，`view.save` 的 `viewpoint` 特化为 `EnterpriseViewpoint`）+ `enterpriseWrite(decide, schema, target, payload)` 类型化包装（`EnterpriseDecide` 为结构最小的三参 decide，任何宿主的 decide 都能赋进来）。
- **读取面**：`queryElements(get, filters)`、`resolveElements(get, ids)`——注入与 `client.get` 同形的 `get`，不依赖 React、不依赖任何具体宿主。
- **管线**：与 host.ts 同一条命令（`cmd/api-types` 加 `-sdk` 路径旗标，默认写同一 gen 目录）；`TestAPIContract` 同时把两份文件钉成 staleness 测试——任何一侧过期即测试失败，提示同一条再生成命令。kernel `index.ts` re-export 整包。

### D5 MES 首个消费者（mes_reads）

1. **`mes.order.release` 载荷 + `place`**（可选，`Ref: enterprise.element`；不声明 `Stereotype`——演示模型把工厂/产线建模为组织单元（ADR-0012 口径），模式嫁接的车间/产线是 ActualLocation，两类都是「场所」，字段因此按引用族收口而非单一 stereotype）→ 内核 `checked` 自动校验存在性（不存在 → INVALID_ARGUMENT），零 MES 侧校验代码。
2. **`Order.Place` 记录字段**（同 ref tag）随决策持久化：引用落在记录上（ledger 重放保真），反向读立刻可见（`/v1/enterprise-references?element=…`）。
3. **发布工单对话框新增「Place in the model」下拉**：选项来自 SDK 的 `queryElements(client.get, { stereotype: ["ActualLocation", "ActualOrganization"] })`（多 stereotype 是 query 契约的原生能力）——主数据表单里第一次出现企业模型元素，即『建模 → 被消费』的证据。
4. **ElementPicker 改道**：从全量 `/v1/enterprise` 客户端过滤改为 `queryElements`（存活集）+ `resolveElements`（既存值，含已关闭的标注）——同一语义（只可选存活元素、已关闭的既存值保留并标注），数据量从全模型降为投影。

## 后果

- 三类契约各有文档（本 ADR）、有类型（host.ts + enterprise-sdk.ts）、有测试（staleness + 语义测试）：改声明不重新生成 → 测试红；绕过 decide 的写入不存在；无条件全量读只剩画布等真需要整图的消费者。
- 查询/解析的页大小与 id 上限是契约的一部分：大模型下带 `q` 的搜索式选择器（picker 里打字检索）未做——当前按 stereotype 过滤后规模有限，等真实痛点触发。
- **MES 主数据自身仍是内存只读种子**（ADR-0088 行的既有欠账）：本块的消费证据是「发布工单挂到模型场所 + 记录反向可查」，不是主数据 CRUD；主数据落库/编辑归 seed_ref 方向，不因本块冒充已具备。
- `web/packages/build` 的选根下拉、pipeline 里 `enterprise.elements` 的全量过滤等既有全量消费点未改写（候选，等触及再迁，不铺一次性搬运）。
- 通用记录内联编辑对 `ref=enterprise.element` 字段仍走 RecordLookup（记录型读法）；表单与动作走 ElementPicker——两种读法并存是有意的，记录型内联编辑若要接企业元素需单独设计。
- 浏览器观感（发布对话框下拉、ElementPicker 行为）**未在浏览器观察**。
