# 交接：ADR-0083 多语言租户定义 + 构建器清单分组（负责人七条）

- **基点 B** = main `8d230387`（分支 `94f9128` 同树）
- **冻结 HEAD** = 见本文件所在提交（`git log --oneline -1`）
- 路径：`git diff --name-only 8d230387 HEAD` → 43 个文件 + 本文 + `docs/ADR/0083-words-and-trees.md`。按目录：
  - 宿主 `capabilities/server/`：`words.go`（新）、`translator.go`、`console.go`、`host.go`、`snapshot.go`、`routes_discovery.go`、`i18n/zh-CN.json`、`host_test.go`、`languages_test.go`
  - 生成：`web/packages/kernel/src/gen/host.ts`（`go run ./cmd/api-types` 产物，勿手改）
  - `web/packages/ui/src/`：`components/GroupedList.tsx`（新）、`index.ts`、`i18n/zh-CN.ts`
  - `web/packages/platform/src/`：`words.tsx`（新）、`apps.tsx`、`index.tsx`、`operations.tsx`、`knowledge.tsx`、`i18n.ts`
  - `web/packages/build/src/`：`editor/ResourceList.tsx`（新）、`editor/names.ts`（新）、`projects/membership.ts`（新）、`ontology/ModelWorkbench.tsx`、`ontology/action-type.tsx`、`ontology/process.tsx`、15 个清单页（`RecordList`→`ResourceList` 一行替换）、`i18n.ts`
  - `web/packages/app/src/`：`automation/agents.tsx`、`i18n.ts`

## 七条对应（设计见 ADR-0083）

| 负责人 | 落点 |
|---|---|
| 1 名称不能"EN/中文" | 租户词典（第三层字典）+ 控制面板"语言与用词"页；对象编辑器对混用名字给警告（不阻断、不自动拆） |
| 2 剩余英文 | 字面 `t()` 零缺口；审计表显示动作标题/模块标题；代理信号与记忆标签翻译；能力名由工作区命名 |
| 3 术语表/血缘 | 术语表说明直说三处读者并指向"语言与用词"；血缘不改（从集成定义读，无第二模型） |
| 4 能力矩阵 | 每模块一卡"提供/使用"，不再用定高表格，重叠消失；`AppInfo.title`；软件包页与矩阵分工写进页面说明 |
| 5 动作类型 | 两段：本组织声明的（可编辑、分组）/ 宿主与模块自带的（折叠、只读、分组） |
| 6 平铺 | `GroupedList`：对象工作台左栏、动作类型、15 个清单（树/表格切换，按项目/对象/状态） |
| 7 布局 | 事实列表 + 换行，状态只出现一次 |

## 已验证（沙箱）

- `go build/vet`，`go test ./...` 全绿（含新增 `TestTenantWords`）；`api-types` 重生成后 `TestAPIContract` 通过。
- `tsc` 逐包：ui / app / platform / build 通过；`scripts/escapes.sh` 无新增。
- 未运行：Catalog check（沙箱对 main 本身即 stale，未提交重生成）、e2e、`make verify`。

## 请本地核对（需要浏览器）

1. **语言与用词**（管理员，控制面板 → 语言 → 语言与用词）：来源默认"租户定义（构建器）"，应列出 WMS 等自定义对象/字段/状态/动作/应用/页面的标题；选 zh-CN，给一条英文标题填译文回车 → 切换界面语言后对象工作台/页面/动作菜单读到译文。反向（中文名给英文译文）同理。删除译文（清空回车）后恢复原文。
2. **混用警告**：对象标题改成 "Pallet/托盘" → Problems 出现警告并仍可发布；"WMS 仓库" 不报。
3. **能力矩阵**：标签不再重叠；筛选框可用；中文下能力名为中文。软件包页"内置模块"段只剩标题与能力名。
4. **对象类型**左栏默认按项目分组，未归项目的在"未归入项目"，代码对象在各模块下标"内置"；动作类型页两段；查询/函数/数据集等默认树形、可切回表格（表格仍可排序分页）。
5. 审计表动作列为标题 + 小字 schema。

未在沙箱观察到浏览器表现，以上均为"待本地代理核对"，不写成负责人已验收。
