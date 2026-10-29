# Design

## Context

本变更由原“目录化整理”改为负责人明确要求的四战役结构主线。负责人已明确授权实现四战役；按各change真实任务状态推进。当前意图仍为客户构建业务应用的平台；只消除非必要复杂性，不借减脂删除真正的规则、权限、持久接受和历史责任。完整源文件去向及目标包预算见 [file-consolidation](file-consolidation.md)。

### 核实的基线

| 范围 | 非测试Go文件 | 真实责任 |
|---|---:|---|
| `capabilities/server` 根包 | 263 | 租户协调、结果应用、记录、授权、工作/Flow、集成、AI、发布、HTTP与静态维护混在同包 |
| `platform` | 166 | 公共声明、Caller/角色、Ledger与纯契约；其中78个page文件还混入视觉配置 |
| `apps/build` | 73 | 原草稿、编译、规则/事务、发布与部分执行，不是73个转发壳 |
| `internal/construction` | 42 | 声明类型、确定性编译/变换、兼容与投影；包含真正运行/纯算法 |
| `internal/host` | 15 | 实际消费者窄角色、构建证据及Attach分派；接口不是可删协议 |
| 其他包 | 120 | 既有owner、格式/records/rules纯基础包、journal、idp及隔离命令 |
| 总计 | 679 | 5,124,183字节（修改守门前HEAD `457202df`），同口径计所有build-tag生产文件 |

Build/construction的精确同名交集为29组，五个重点位置合计66组；全仓守门库存为82组/195路径。`code.go/page.go/query.go/property_type.go/link_type.go`在construction主要是常量承载，可并回owner；`formula.go`是旧语法lowering，`pipeline.go`还有真实纯变换，`process.go`与原Flow是作者/执行两类表示，`integration_release.go`剥离可变环境和凭据。不能把所有Struct A→B转换一概删除。

`accepted_*.go`实测23个生产文件，各自并非同一种结果：含Agent、Work、HostControl、Usage、callback行、控制状态、发布映像与SQL计划。当前原`internal/acceptance/result`持版本化格式，`internal/records`持历史/索引/固定行映像；重排必须复用它们的语义而不是再建统一账本。

### 原规划中已失效的陈述

当前`deploy/local/compose.yaml`只有单一platform宿主入口，默认8495；外置worker/builder仍按Unix socket和各自权限运行。gateway不在当前核心树，不能再将“8490/8495双宿主、gateway、compute三服务”列为默认交付目标；当前cmd/README的双实例表述也是文档债务。`cmd`现有11个main包，平台updater是维护监督工具，不因一个目录就算一个业务服务。旧sink收编、govern归位、依赖字节还原和同包拆分已有实施记录，但不能替代今天的目录边界或运行验收。

## Goals / Non-Goals

**Goals:** 原职责与类型只有一个owner；退役construction/host目录；保持单一平台公共导入面；消除根包杂糅及机械调度；每package≤25并以受守门的递减基线收尾（收尾时生产文件355、根包168，原≤339未达成）；报告真实净LOC，保留行为/权限/历史与恢复。

**Non-Goals:** 本轮不迁移业务或更改运行数据、不调整依赖版本、不合并受控执行隔离进程、不继续全量源码自举、不重建enterprise权威、不通过重命名或删除测试伪造净删除、不另建远程服务/总线/万能执行器。

## Decisions

### 1. 四职责采用星形装配，不形成四个上帝包

```text
cmd/platform-server → composition
                        ├→ kernel      接受、持久化、恢复、固定读取
                        ├→ engine      业务运行、Flow/Work、规则和授权
                        ├→ studio      草稿、编译、候选、发布、试用
                        └→ aigateway   provider、工具协议、阶段与预算
四职责 ─→ platform 公共类型/窄角色 ─→ contract及既有纯基础契约
legacy apps ─→ platform
```

四职责彼此禁止直接导入，亦不得导入旧root、composition或apps具体实现；各自可以使用本职责子包、platform及已有纯基础包。composition是唯一知道所有具体实现的接线处，负责初始装配、维护重建、试用及私有fork的typed重接线。命令按实际执行责任可导入composition或明确的执行owner；不能被业务层反向依赖。

平台API禁止导入这四个concrete职责、composition、apps及退役construction/host。`platform/rules.go`目前使用纯`internal/rules`的类型及默认实现；保留该无I/O基础依赖，不机械搬到engine后造成 `platform→engine→platform` 环。engine选择/实现规则能力、studio经同一platform规则角色编译，实际语义仍唯一；公共API不能把具体engine程序或宿主私有类型暴露出去。

目录分成小而有职责的子包，全部≤25文件。kernel不是替换`contract/`的K1–K9解释器，aigateway不是新的身份/配置权威，studio不在运行期直接决定业务，engine不持有接受存储旁路。

### 2. 接受23→3，合并机械协调而保留结果族

| 目标 | 原路径 | 保留责任 |
|---|---|---|
| `kernel/acceptance_pipeline.go` | agent/effect/host_control/input/release/requests/usage/work 8文件 | 原族专属输入、授权/前驱、任务世代、锁外回调、发布接受和未知回执 |
| `kernel/acceptance_records.go` | approval/batch/callback_rows/connectors/control/intents/notices/observation/plan/rows/state/unique 12文件 | 行/历史/状态/收据、SQL delta、批次私有提升、控制与发布安装映像及观察/通知 |
| `kernel/acceptance_codec.go` | refusal/result/store 3文件 | 原请求/结果身份、封存预算、持久接受、原字节回执和应用标记 |

`internal/acceptance/result`中的七结果族及其他runtime专属格式仍有各自有类型的解码、摘要与验证；在目标`kernel/acceptedformat`保持显式版本分派。可以表驱动编排共同的准备→持久→应用步骤，不能把业务输入降成无类型JSON、重跑原规则/授权或用一条万能“decide”取代不同owner。

锁与状态只迁归一次：kernel持租户单写lane与已安装代际，私有结果收齐后才持久；ops类工作/状态仍按原owner锁保护，不能因合并函数改变mu→opsMu/streamMu义务或在已有lane回调重拿锁。记录固定SQL视图、接受事务和原结果应用完成的顺序不变。

每类必须核对callback行、嵌套批次、唯一键、通知、HostControl、Agent/Work世代、Usage前驱、发布安装和SQL delta。旧提交/旧输入保持原兼容责任，历史字节不重写；合并不是接受格式版本演进。

### 3. Build与construction只保留一个设计owner

73+42文件内聚至`studio/build`的23个职责文件，逐文件表明确到目标。类型、校验和确定性编译同一owner直接共享，不保留旧目录、兼容别名或重复记录。纯formula lowering并入既有rules基础模块；真正的pipeline/source/matching变换、规则表与事务执行归engine，studio仍负责其声明/编译/发布。

草稿与不可变发布描述的语义差异保留：编译会解析版本、规范类型、冻结依赖并剥离本地地址、凭据和运行观察。只消除同义的重复类型搬运，不把这些安全投影删除。Process→Flow、页面安全绑定及运行字段投影等编译关系使用同一现有算法，不以“同名”要求无编译。

原published影像、pinned版本、联合首次发布、后改草稿与候选隔离、兼容扩展和受控试用必须继续工作。Build拥有的运行回调改由公共typed能力连接原engine，不导入engine实现；编译和业务执行不互为调用依赖。

### 4. host目录退出不代表协议退出

15文件有用角色合入platform已有文件：Agent→ai、Build/Blueprint/资产→definition及asset_lifecycle、Flow→flow、Work→work、凭据→credential、组织→org、账簿/事务→transaction、传输/确认→protocol、成员与Caller→app。仅聚合Host、纯转发和Attach分派退出，不把它们复制为新的全权限平台接口。

公共角色不能引用studio.Build、kernel.Tenant或internal.construction的私有类型；必要的草稿/发布协议值由原公共API声明，方法保持原权限/状态语义。composition提供适用于初始实例、试用与私有fork的接线工厂；kernel调用已注入的角色，不对具体应用做Attach类型断言。角色缺失或循环应在组合检查明确拒绝，不能宽化到any或直接传整Tenant。

### 5. 用真实基线控制减脂，不能用配额掩盖债务

守门使用root=263/platform=166/Build=73/construction=42的临时递减配额；其余package≤25。重名82组195路径、37个视觉AST声明（22文件）及2个退役目录均冻结存量，不准扩散；HEAD中已有baseline只能缩减，删除代码时同步剪去对应条目，禁止自动“更新基线”接纳新债务。

SDK、裸模型endpoint和模型请求方法只归provider，原4个裸调用/SDK路径豁免已随provider抽离撤销，不能恢复为迁移债务；纯rules不得导入网络/文件/数据库/部署或实时状态。四职责依赖图同步落实为AST检查，编译标签不豁免扫描。既有超额不是合规证明，未来迁移也不得通过扩大baseline、新增wrapper目录或改名躲过检查。

逐文件映射当前形成215个目标生产路径（含保留工具、journal/idp、纯基础包），平台API25、studio/build23、每包≤25。该215是包含新增阶段/训练职责的设计去重结果，不是已经写出且编译通过的数量；最终实际必须≤339。需要补回安全实现时，在原职责内修正并重新计数，不能为追求215删去正确性。生产净LOC单独报告，不能拿679→215的路径算术当净代码删除证据。

### 6. 三个关联变更按原owner协作

- [页面解耦](../archive/2026-10-28-decouple-page-presentation-contract/design.md)：78个生产page文件→3个公共安全契约；保留原记录/查询/动作安全，把视觉配置交Web。根页面运行owner的新物理路径见本对照表。
- [AI隔离](../archive/2026-10-28-isolate-ai-execution-and-training-data/design.md)：StagePolicy、provider/MCP、预算/降级及SFT/DPO由该变更定义；本表标注原调用文件去向，不复制政策或另建样本Sink。
- [自举收缩与UAF](../archive/2026-10-28-platform-self-hosting/design.md)：先静态禁区与新自举冻结，再UAF外移/原组织owner内聚。原system证据及恢复归属保留，不能为文件目标删未知回执或历史。

三者实施次序与停止条件只归WorkQueue；各change任务分本次责任，不建立另一份队列。实际源码API、generated host.ts、enterprise-sdk和Catalog随相应批次原工具生成，不能手工修生成物掩盖协议差异。

## Risks / Trade-offs

- 单个Tenant方法跨包后无法仅靠移动继续访问私有字段 → 按上述状态owner和typed角色实际拆责任，禁止通过暴露整个Tenant或公共字段绕回原上帝对象。
- 合并文件减少数量却增加理解成本 → 表中每个目标只承接共同owner和真实职责，先核对调用/锁/历史；报告生产LOC和真实依赖，负责人验收优先于文件配额。
- 合并接受族误删专属回调或SQL映像 → 原格式/存储向量及owner测试继续覆盖，真实数据库恢复不能跳过后报通过。
- 把公共API移入concrete包引入循环 → 构建与AST守门共同检查；既有纯基础依赖保留，四职责跨域只能用原公共typed角色。
- 把旧完成记录当当前拓扑 → tasks历史区完整保留但明确过时；只勾选本次真实完成的行为与验证，不以旧完成记录替代剩余迁移。

## Migration Plan

1. 本轮完成规划与守门，锁住存量；运行owner守门测试、全边界及check，不修改业务/数据，不部署源码。
2. 先按自举change保住静态登录/维护并冻结新全量自举；逐批固化公共窄角色与composition工厂，保持原运行能力。
3. 按owner合并Build/construction和host接口，再迁根包运行/设计职责；每块同步原测试、文档与生成物，报告真实净LOC/文件数及依赖，不留同义旧路径。
4. 在明确角色与状态边界后合并接受23→3，沿所有格式/回调/SQL/恢复验证；不能把全库验证拖到最后才发现历史责任丢失。
5. 协调页面、AI及UAF批次，用同一679基线核对最终≤339与每包≤25，执行适用集成、真实PG恢复及代表性浏览器路线。完成后才同步主规范与归档，代码按本次授权更新单一Platform入口并核对实际版本/资源/原数据。

## Historical implementation record

旧12项完成记录继续保存在tasks；已实现sink收编、govern重划、源码同包拆分、依赖还原校验及其当时验证不抹掉。旧双实例/三服务、固定8490/8497目标、等待代理和“一种接受结果一个文件”的收尾方向均不再定义本次目标。当前部署文档中的过时拓扑在对应实施批次修正；本轮不修改Architecture或主规范冒充业务已经迁移。
