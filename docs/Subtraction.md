# 做减法清单（由人执行）

ADR-0070–0074 落地后，下面这些路径已被 Connection / Source / Dataset / Pipeline / Writeback / Matching rule 取代。沙箱不能编译独立 Go 模块（`apps/*`、`solutions/*`），所以删除由人在本地执行；每项给出"删什么"与"随手要改什么"。按顺序做，每做完一项跑 `scripts/verify.sh go` 与 `scripts/verify.sh web`。

## 1. `apps/erpadapter` + `web/packages/erpadapter`（ADR-0070 §减法、ADR-0072 §减法）

被取代：自配凭据的 ERP 轮询 → `build.connection` + `build.source`(odata/json)；`erpadapter/confirmation` 效果 → `build.writeback`（连接即端点、裁决 change id 即幂等键）。

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
