# 做减法清单（由人执行）

ADR-0070–0075 提供了 Connection / Source / Dataset / Pipeline / Writeback / Matching rule。下面列出旧路径的处置；制造方案的 `erpadapter` 与调用方已一起删除，现有外部摄取与回写沿集成织物。沙箱不能编译独立 Go 模块（`apps/*`、`solutions/*`），本地删除须与调用方迁移一起验证。每项完成后按影响面运行 `scripts/verify.sh capabilities composition web`；这里是迁移范围参考，执行顺序仍以 WorkQueue 为准。

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

## 4. 构建器旧动作表达（ADR-0100，已冻结，尚未删除）

`buildapi.Action` 的 `Sets`、`Creates`、`Posts`、`Journal`、`Reverses` 及 `Object.Numbering` 的记录计数编号冻结为既有能力，不再承载新能力。新增多对象变更只用 `Action.Transaction`；编译拒绝将事务与旧变更字段或旧计数编号混用。最小切片接管 create/update/sequence，复用原生成记录动作的授权、字段与记录校验及宿主命名序列；它不走 `stored` 的相关记录复算，也不走 `numberingOf` 的记录计数。

退出路径：旧定义逐一迁移到事务步骤后，删除对应 `actions.go:stored`、`creates.go`、`posts.go` 和旧编号表达；post/journal 迁移仍需各自 owner 的后续步骤支持，本块不实现。旧 `Published`、日志与定义字节保持原样，既有动作继续读取、运行与历史恢复；迁移完成前保留这些义务，不建立新旧双写路径。新赋值源由 ADR-0101 的 Expr 接管，`setSource` 仅保留冻结旧相关创建的输入适配；事务中的旧赋值输入先降低为 Expr，再走同一求值器；逐步骤写入沿 `Ledger.ApplyTxnRecord` 调用原 `standard`，不另造授权或记录校验实现。

## 5. 四套规则求值（ADR-0101，已替换）

旧公式 `ValueFormulaNode.Eval`、决策表条件 `ValueCondition.Holds`、动作 `take` 的比较分派及页面 TypeScript `operators` 已删除。旧字符串/单元格/三元组/页面算子图只作编译输入，保存为 Expr；数字稿件的格式化与原参考应用的领域规则不算语言求值器。未使用的前端 `decimalArithmetic` 已删除。冻结旧公式的数值语义在同一引擎内受限保留，新 Expr 不可指定兼容模式；退出条件是既有不可变旧公式不再有运行义务，历史字节始终保留。

## 6. 新模型的入口专属校验（ADR-0102，已收敛）

新对象的存储不变式归统一约束门禁，不在表单、CSV、事务、AI 或协议另写一份。CSV 的“空白单元格静默省略”已删除，空白表示清空并由同一门禁报告；预览复用私有决定试算，未增加预览执行器。字段输入语义保留在原 Required/InputConstraints；原生行业守卫与独立 relations 图分别随 ADR-0113/0114、ADR-0104 退出，旧定义/日志的读取与恢复义务继续保留。

## 7. 独立同权威关系图（ADR-0104，迁移中）

所有 owner 的 ad-hoc 同权威 link / unlink 新写已冻结，未声明关系存储不再保留写入例外；新关系只经声明的所有者事务。旧关系保持按原权限读取，通过显式迁移成为所有者记录，迁移标记让原边退出当前读取，原 owner-state 保留历史证据；不双写、不改历史字节。实际租户迁移仍须执行，原生行业应用的退出归 ADR-0114。跨应用关系、动态、评论和关注仍归 relations。
