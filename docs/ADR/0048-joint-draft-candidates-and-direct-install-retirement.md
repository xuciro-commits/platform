# ADR-0048：联合草稿候选与生产直接安装退场（提案）

**状态：** 已采纳，2026-10-05（#141）。负责人指示按 §2 的推荐值“一口气干完”，M3 已按 D1–D8 实施中，实际落地与剩余见 §6。[ADR-0047](0047-platform-composition-and-workspaces.md) §10.2 规定 M3 的“必要编译、profile 与权限变更先设计”，本文即该设计；独立发布/审计/项目 ACL 仍按 ADR-0047 §11 专项。
**范围：** ADR-0047 §7.1 的联合草稿候选与生产直接安装退场。不改变 M1/M2 已接受的入口与边界；独立发布/审计/项目 ACL 与 §13 持续 Flow 不在本文。
**设计日期：** 2026-10-05。代码事实基线：`e41aa91c`（M2）与其上的 `main`。

## 1. 背景：今天为什么做不到“新对象＋新页面＋应用”

- 候选预览与封存是**单根**的：`POST /v1/releases/preview {kind,id}` 只把一份草稿代换进 inventory（`Build.DraftReleaseAssets`），`POST /v1/releases/candidates` 保存该闭包，`POST /v1/releases/active` 沿原发布主人提交激活。
- **直接安装**（`build.object.publish`、`build.page.publish` 等）立即改变运行定义，编辑器与资源库都提供它；前端标注“Direct install”。
- 因此页面草稿引用未安装对象、或应用引用尚未发布的页面时，闭包无法形成；多份草稿也不能作为一个候选封存。这不是菜单问题，是候选输入问题。

## 2. 待决决定（建议逐条答复“同意 / 改为 …”）

| 决定 | 推荐 | 理由与边界 |
|---|---|---|
| D1 联合草稿图的输入 | 以**应用为根的显式集合**：应用及其当前草稿的页面、对象、逻辑由构建者在交付台逐一勾选/取消；未勾选的依赖必须是已发布版本，否则阻塞。服务端 `DraftReleaseAssets` 扩展为多草稿代换，保存时按同一集合重算候选 ID，不信任浏览器字节 | 保留“显式选择”与稳定资源身份；不做自动纳入或隐式扩展 |
| D2 闭包与阻塞 | 缺依赖必须**列名阻塞**；共享属性/关系/函数等仍须已发布或一并显式勾选；不支持作为候选根的资产（尚不支持的 profile）如实阻塞，不造空应用 | 与 ADR-0047 §7“不能为了编辑一个应用先把每个依赖逐个发布”一致，但不放松 owner 校验 |
| D3 测试与差异 | 沿用现有评测门（每个 included function 需通过报告）与 added/changed/removed 差异；联合候选额外列出**草稿来源**（哪些记录、各自 revision） | 失败保留草稿与旧定义；预览/生产作用域不混淆 |
| D4 封存与激活 | 沿用原封存与原激活主人及激活指针；失败保留旧版本与诊断；**不新增应用私有激活指针** | ADR-0039/0038 的接受结果与恢复边界不变 |
| D5 生产直接安装退场 | 分两段：**(a)** 先实现并证明联合候选路径（WMS 新对象＋新页面＋应用；完整 Module profile；PMS 原生任务不动）；**(b)** 证明后再移除生产入口——编辑器与资源库不再提供 Direct install，改指向“加入草稿图/形成候选”，并由后端权限/profile 拒绝其 API 旁路 | “仅隐藏按钮不算退场”；不先删按钮破坏现有路线；开发/导入/探针 profile 可保留同一编译、安装与恢复实现，但须显式限定且不继承生产权限；历史 Published、版本解码与恢复保留 |
| D6 Module 与导入 profile | 完整 Module 导入改为生成**联合草稿**（保留其“逐页发布”的现有能力仅限开发 profile）；`WorkshopApplicationImport` 的预发布依赖随之改为候选输入 | 不因“导入成功”跳过交付检查 |
| D7 权限与审计 | M3 不新增角色：预览/封存/激活仍要求 Builder；拒绝原因与主体写原审计。独立发布/审计/运维授权、项目委派按 ADR-0047 §11 专项 | 不把“仅隐藏编辑器”当分权，也不扩大 Builder 数据特权 |
| D8 编译与运行影响 | 本批不改 Worker/编译契约（仍 ADR-0044）；若联合候选需要新的 profile 或编译字段，先出接口与版本再实现，不静默削减预算 | 计算/流程执行主人不变 |

## 3. 被拒绝的选项（不做）

- **前端拼装“联合候选”**：多次单根预览相加、一次只激活一个根——激活语义不符，属伪装，不做。
- **先删直接安装按钮再补候选路径**：违反 ADR-0047 §7.1，不做。
- **平行部署状态机 / 应用私有激活指针 / 第二套发布系统**：重复原发布主人，不做。
- **为通过验收而削减默认 Flow/预算**：不做（与 §13 一致）。

## 4. 若负责人接受

1. 把 M3 纳入 WorkQueue 当前行，按 §2 D1–D8 实施：接口与状态规范 → Go 多根代换与测试 → 交付台 UI（草稿来源、差异、封存、评测、激活）→ 直接安装退场 a/b → ADR §14 与 WorkQueue 收尾。
2. 验收按 ADR-0047 §10.2 M3 行：新对象＋新页面＋应用及已支持完整 Module profile 可在不预先安装依赖下交付；失败和旧数据/在途版本保持；随后阻止生产直接安装旁路。
3. 检查按 WorkQueue 行：编码中只查类型/构建通过性；整块完成后一次 `web-check`、Go 检查与联合路线走查；无持久规则改变不跑全量部署演练。

## 5. 关系

本稿只扩展**候选集合及其封存/激活输入**，不改变 ADR-0026/0038/0039/0044 的执行、接受结果、恢复、发布与编译主人；ADR-0047 的入口、组织与其他批次的边界不变。

## 6. 实际落地（2026-10-05）

- **D1/D2 联合草稿图**：`Build.DraftReleaseAssetsMulti` 用一份共享 inventory 代换多份对象/页面/应用/流程草稿（`jointEntities` 让未安装的对象在页面与流程里可命名），简单类型仍走各自 owner 的单草稿路径；缺依赖在预览阶段**列名阻塞**（`ReleasePreview.Diagnostic`），引用校验接受“已安装、已保存草稿、或他应用已声明”的对象，其余如实拒绝。
- **D1/D3 候选输入与服务端重算**：`POST /v1/releases/preview`、`POST /v1/releases/candidates` 接受 1–32 份显式勾选（单一 kind/id 与 drafts 互斥）；`POST /v1/releases/drafts/referenced` 由服务端给出所选记录草稿尚未安装的依赖草稿，供交付台勾选。保存时在同一租户锁下按同一集合重算候选 ID，陈旧/外来 ID 被拒绝；预览回显 `drafts` 作为联合候选的草稿来源，差异仍为 added/changed/removed。
- **D2 先证明可安装**：多草稿预览先重建“当前已安装”映像（含全部 prior 与改名/删除闭包），再走**与激活同一套私有安装**的干跑（`releaseInstallationsLocked`，不提交任何状态）；不可安装即 Diagnostic，绝不封存。
- **D4 封存与激活**：沿用原封存与原激活主人及激活指针，不新增应用私有指针；失败保留旧定义、草稿与在途版本。
- **D5b 直接安装退场**：构建者声明 `build/releaseProfile`（`production`/`development`，默认 `development` = 开发/导入/探针）。`production` 时 owner 在 `Submit` 拒绝九类直接安装 schema（`ERROR_CODE_POLICY_DENIED`，消息点名候选路径），回放/恢复与历史 Published 一律不受影响；`GET /v1/release-profile` 让编辑面在 production 不再提供 Direct install，改为指向发布评审。编辑器与资源库按钮仅在 development 显示。
- **D7 权限**：预览/封存/激活仍要求 Builder，未新增角色。
- **D8**：未改 Worker/编译契约。
- **D6 第二批（2026-10-05，本批）**：Module 导入的预发布依赖改为候选输入。导入对话框可把页面绑定到**已保存未安装**的对象草稿（`SemanticObjectSelect` 的 `drafts` 选项、`ModuleImportDialog` 的合并实体集），导入完成后把所用对象草稿作为依赖报给应用编辑器（`importDependencies` 纯函数），`Review application release` 以 `drafts=object:<id>,…` 打开交付台并**预选**这些草稿，页面与对象作为一个候选交付；开发/导入 profile 的逐页发布保持不变。
- **证据**：Go 测试 `TestJointDraftsDeliverNewObjectPageAndApplication`（新对象＋新页面＋应用的联合交付、来源、陈旧 ID 拒绝、激活后可见）、`TestProductionProfileRefusesDirectInstallAndKeepsDelivery`（development 直装仍在、production 拒绝且草稿未变、联合候选在 production 仍交付、回放一致）；e2e `joint-draft-release.spec.ts`（两个行业，UI 走联合交付，并证明单独页面被拒）。
- **D6 完成**：完整 Module 导入改生成联合草稿（第一批）与 `WorkshopApplicationImport` 预发布依赖改候选输入（第二批）均已落地，见上。
- **M4 固定持久环境证据（2026-10-05，本批）**：本沙箱用 `pgserver`（PyPI 包，PostgreSQL 16.2，Unix socket）建立真实日志；`hospitality-server -database …` 在真实 PostgreSQL 上写入对象/记录后停止（快照落库）并重启（`restored hotel-a from the snapshot at N, then replayed 0 entries`），定义与记录原样可读、可继续写入；`capabilities/server` 全套测试带 `PLATFORM_TEST_DATABASE` 在真实 PostgreSQL 上全绿（含联合草稿、production 拒绝、回放恢复）。为此修正两处测试侧问题：追加前必须先读日志（与部署一致），测试决策时间取日志精度（微秒）。
- **剩余**：运维界面走查（从失败业务任务定位原运行/原因与获权恢复动作）；`deploy/local/rehearse.sh` 末尾的 delivery profile 断言（本沙箱无 Docker，未实际执行；轻量 profile 见 [ADR-0049](0049-delivery-and-lightweight-profiles.md)）。
