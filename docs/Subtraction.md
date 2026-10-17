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

检查沿 Testing 的应用运行任务与构建者验证任务：原正式详情取消、答复丢失后同键重试、编辑器历史查看及正常新建入口已通过；旧操作按钮明确不存在。Web 类型、目录及双语检查通过，不改执行、权限、持久格式或历史版本责任。

## 10. 集成资产的重复目录读取与安装解码（ADR-0124，已替换）

`Build.integrationInventory` 的七份分页读取、类型转换与归档过滤退出，改用原 `readDefinitionInventoryOf` 和一份归档过滤；500 条分页、1000 条上限和原 Build 读取主体不变。`DecodeAssetInstallation` 的独立七类分配/解引用分支删除，与安装校验、升级校验共用 `integrationInstallationRecord`；目录和解码只在 `integrationRecordType` 维护一次记录类型。严格解码、安装身份、保留版本族、描述与权限检查及保存图像字节继续保留，没有新注册表或历史格式。原 `asset_lifecycle_test.go` 检查七类记录、错误图像、跨页读取、归档过滤与数量上限；其余原安装、升级和恢复测试继续使用同一实现。
