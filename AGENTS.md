# AGENTS.md

AI 编程助手从这里开始。`CLAUDE.md` 仅引用本文件；`.agents/skills` 指向 `.claude/skills`，不维护第二份规则。

## 项目与阅读入口

这是面向 FDE 与客户构建者的 AI 业务应用平台：语言中立的内核契约、Go 宿主、Web 工作区和跨行业验证应用。CRM、MES、ERP 是目标探针，PMS、HCM、CSM 是参考应用；Music 归 MSRU。

首次进入项目读 [Intent](docs/Intent.md) 和 [WorkQueue](docs/WorkQueue.md)；续接只读当前队列项及受影响部分，再按需读取 [Platform](docs/Platform.md) 的相关章节、契约与 ADR。开启阶段或改变架构时读 Platform §10；日常增量无需重读整份架构、历史审计或全部 ADR。先看 Git 状态，保留其他工作的改动。

## 批次与停止

- 负责人本次指令决定范围；未指定新优先级时取 WorkQueue 的当前项。只推进一个活动批次。开工说明本批可见结果、能力归属、停止条件和适用检查，写在会话或现有队列行即可。
- 将大工作号拆成可交付的小批次，每批改善一个构建、交付或操作任务。按整条任务路线的最大断点选择下一项；代码行数、提交数与测试数只用于诊断投入，不作完成度或前后端配比目标。年度门禁用于阶段验收，不自动成为每批的前置要求。
- 只扩展阻塞本批任务或破坏其权限、数据、版本保证的必要依赖。其他缺口简短记入原工作项；发现共性问题决定修复归属，不自动改变优先级。
- 达到停止条件、适用检查通过、受影响文档准确后就收尾。后续按队列推进，避免自动追加邻接功能、故障矩阵或视觉打磨；重复测试的条件见 Testing 的“检查选择与停止”。
- 每批结束说明平台哪段任务变得可用、还缺哪段以及下一批。若连续两批只改善同一局部内部机制，先检查队列是否应转向构建、交付或操作中的另一缺口；负责人明确要求的专项工作按其范围继续。

## 核心规则

1. 内核是语言中立的契约；Schema、语义、错误、兼容性与一致性各有归属。Protobuf 不定义业务含义 (ADR-0002)。
2. `contract/` 不含领域词汇；参考应用不能为自己的业务修改内核。摩擦力记入 WorkQueue。
3. 内核变更先改规范与向量，再改 Go 及受影响的 Rust/TypeScript 边缘实现。`v1alpha1` 列出破坏性变更，`v1` 变更须 ADR。
4. 保持代码最简，不保留永久重复路径或临时垫片。生成文件不得手改，包括 `contract/go/gen` 和 `web/packages/kernel/src/gen/host.ts`；宿主 API 类型改变时，在 `capabilities/server` 运行 `go run ./cmd/api-types` 更新后者。
5. 客户端使用 `@platform/ui` 与 `@platform/app`。代码与受控定义共享语义绑定、授权与发布；租户代码扩展只走受控编译、隔离执行及版本化制品路径，设计见 [ADR-0044](docs/ADR/0044-capability-fabric.md)，不直接进入宿主执行。构建体验参考与采纳边界由 Platform §10.1 维护。
6. 文档各司其职：Intent 管宗旨；Platform 管架构与能力摘要；ADR 管决策和实现边界；WorkQueue 管即时顺序与状态；Testing 管验证方法；Apps 管可执行用法。文档只保留长期决策、当前边界、用法与近期待办；实现摘要直接合并更新，不追加检查点日记。过程留在会话与 Git，不新增总结、控制文档或第二队列。
7. 外部建议需对照本仓库证据判断。只将采纳内容放入其归属文档；历史审计、已接受目标、当前实现和未验证结果应能区分。
8. 批次按 `close-out` 收尾：检查方法以 Testing 为准，旧 ADR 的检查日志不自动成为新批次要求；运行适用检查，仅更新本批改变事实的文档。无需为小修复改齐所有文档；实现证据不在能力地图、队列和测试指南中反复复制。未运行的测试或未观察的 UI 不得报为通过。
9. 提交信息用单句祈使句，注明适用的 ADR 和工作号。提交、推送、部署遵循本次任务已有授权。
10. 产品界面与声明文字提供英文原文和简体中文：应用 `i18n/zh-CN.json`，UI `t()` 与包内 `i18n.ts`。业务数据不翻译；仓库技术文档沿用所在文件的语言。
11. 先复用，单一归属，唯一规范路径。UI 变更按当前任务查 Platform Catalog：`node scripts/catalog.mjs search <任务>`，或按需读 owner 摘要；不全量读取目录。UI Kit、`@platform/app`、应用 API、平台应用、宿主各管自己的能力；不足时扩展归属方。应用不自造共享表格、面板、审批、签名、报表、文件或外部调用。`scripts/escapes.sh` 阻止新增逃逸，已知项只减不增。
12. 应用用于证明平台能力。平台基石门禁见 Platform §10.6；行业深度只取当前验证任务必需的最小变更。平台优先是研发方向，不是百分比汇报指标，也不要求每批补齐所有平台能力。

## 代码导航

| 位置 | 归属与入口 |
|---|---|
| `contract/` | K1–K9 规范、向量、生成类型、Go 参考实现；`lean/` 为有界证明工具链 |
| `capabilities/server/platform/` | 应用与协议唯一可导入的应用 API |
| `capabilities/server/` | 宿主运行时；`internal/host` 为宿主内应用接口；`apps/` 为平台应用 |
| `apps/<id>/server/` | 自治业务应用；跨应用通过 `protocols/`，用法见 Apps |
| `solutions/`、`deploy/local/` | 酒店/制造组合宿主、Compose 与恢复演练；地址账号见部署 README |
| `web/packages/ui`、`web/packages/app` | 共享 UI 与前端应用 API |
| `web/packages/<id>`、`web/apps/` | 应用 UI、统一工作区、Platform Catalog 与 PMS 桌面前端 |
| `web/e2e/` | 开发宿主浏览器路线与一次性部署路线 |

## 按需流程与验证

- `architecture-gate`：未决的结构性选择或阶段启动；已接受设计内的增量不重复过门禁。
- `new-app`：新建或扩展业务应用；只取当前批次所需步骤。
- `close-out`：检查、必要文档与交付一起收尾；三个技能的唯一定义在 `.claude/skills/`。

验证统一入口为 `scripts/verify.sh <step>`。检查选择表在 [Testing](docs/Testing.md#检查选择与停止)：`contract`、`formal`、`capabilities`、`composition`、`web`、`pms`、`mes`、`deploy`、`format`；`ci` 为持续集成组合，无参数为全部检查。日常增量按影响面选择，不默认跑全部。样式/布局用启动截图与人工走查；自动回归聚焦核心行为、权限、数据和恢复，不写像素/尺寸断言或复制设备/语言路线。
