---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

阶段 P6 “结构收口”：把已经存在于代码中、但落在错误包里的行为归位到规范定义的服务包，并让验证链可信。按序执行；前一项未通过验收不开始后一项。

- **P6.1 F3 偶发失败的根因。** `web/e2e/F3-dataset-schedule.spec.ts` 在一次全量 e2e 中失败：界面保存“停用”后，后端 `build.schedule` 的 `enabled` 在 15 秒内未变为 false。该次运行与并行编译重叠；之后单独重跑与空闲机器上的全量运行均通过。复现不稳定，因此先定位根因（轮询条件、保存是否真正提交、还是负载下的时序），再修复实现或用例的错误假设；不得只加长超时。验收：在有意施加负载的条件下连续三次通过。
- **P6.2 授权决定唯一化**（[6-domain/security](6-domain/security.md)，[7-system §7.2](7-system/README.md) `services/security`）。授权上下文构造与唯一决定函数迁入 `services/security`；`host/authorization_decision.go` 只保留装配；删除 host 中重复的 `authorize*` 入口与自动化运行时上的 `Authorize`。验收：`TestInvariant` 与 F4 通过；决定函数在代码中只有一个实现；删除路线图“已知漂移”中的对应项。
- **P6.3 ObjectSet 执行下沉到 `services/objects`**（[7-system §7.4](7-system/README.md)，[2-product §2.3](2-product.md) 对象读取）。`host/object_sets.go`、`host/records_query.go`、`host/record_sets.go` 中的计划与执行迁入 `services/objects`，分三个切片做，每切片保持 K1、K4 通过；`services/objects` 不再只有 `merge.go`。
- **P6.4 数据源同步下沉到 `services/data`**（[7-system §7.2](7-system/README.md)，§7.3 外部源）。`host/source*.go` 与 `host/sources*.go`（约 2100 行）的接入与合并逻辑迁入 `services/data`，先做 sources 一个切片。验收：K1、K5 与 F3-source-sync 通过。
- **P6.5 命名收敛**（[7-system §7.7](7-system/README.md)：同一名字不指两样东西）。`flowExecutionRuntime`（`host/automation_execution.go`、`host/approval_execution.go`）改为与其职责一致的名字；Flow 资源已删除，名字不得残留。验收：全仓 `grep -w flowExecutionRuntime` 为空，`make verify` 通过。
- **P6.6 Workspace 首屏测量与收敛**（[7-system/web](7-system/web.md)）。先记录基线：入口静态可达 JS 约 2.39 MB（原始），最大静态 chunk 1.36 MB，数字写入 `7-system/web.md` 现存差距。再把 `@platform/ui` 中只被懒加载模块使用的导出改为子路径导入。验收：静态可达 JS 有前后对比数字；不改变任何页面行为（F1–F10 通过）。
- **P6.7 文案完整性检查变成可执行检查**（[7-system §7.8](7-system/verification.md) 文案行；规范 §4 “能执行的不写成文字”）。先测量各 `i18n.ts` 与 `zh-CN.json` 的缺失键与未使用键，把检查写成脚本并接入 `make verify`；历史缺口清零或列入本条目的“现存差距”。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。以下需要负责人决定，循环不得自行处理。

- **依赖批准清单与实际依赖不一致**（需决定）：[1-enterprise §1.8](1-enterprise.md) 只列出 OpenTelemetry、Playwright、MinIO 客户端、React Flow 为已批准。实际存在但未列入的有：`web/packages/ui` 生产依赖中的 `three`（规范标为“待用”）、`echarts`、`pdfjs-dist`、`zod`、`radix-ui`、`react-hook-form`、`yaml` 等；Go 侧的 `anthropic-sdk-go`、`wazero`、`pgx`、`go-oidc`、`minio-go`。需决定：补录批准，或移除。
- **`services/ontology` 不存在，本体代码在 `services/resources/authoring`**（需决定）：[7-system §7.2](7-system/README.md) 的目标包表列出 `services/ontology`；实际对象类型、属性、值类型、链接类型的代码（约 2.2 万行的 `resources` 中）未独立。需决定：新建该包并迁移，或修改 §7.2 把它并入其他包。
- **生成物入库且高频改动**（需决定）：`web/apps/catalog/src/gen/catalog.json`、`ui.*.json`、`web/packages/kernel/src/gen/host.ts` 的提交次数最多。需决定：改为构建时生成并不入库，或保留入库。
- **`kernel` 一词指三处**：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- 门户的业务组未按产品或项目分组（[3-information §3.3](3-information.md)）。
- 治理资源与容器尚未采用同一路由入口（[7-system/web](7-system/web.md)）。
- 少数位置直接编译 Expr，绕过可注入的规则引擎（[6-domain](6-domain/README.md)）。`cmd/rule-browser` 直接绑定 `ruleexpr.CompileJSON`，需确认它是否属于同一问题。
- `SetBuilder` 的 `domain` 条件行仍由命名查询依赖（[4-interaction/patterns](4-interaction/patterns.md) M10）。
