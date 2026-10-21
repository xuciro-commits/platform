# ADR-0070 — 收敛页面 Widget 校验：270 文件平铺 → 能力分组

<a id="adr-0070"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项）。

## 1. 问题

`capabilities/server/platform/` 有 270 个文件，其中 162 个为 `page_*.go`：

```
page_gauge.go              27 行
page_heatmap.go            45 行
page_separator.go          17 行
page_boolean_input.go      23 行
... 共 80 个实现文件 + 82 个 _test.go
总计 17,050 行
```

每个文件为一个 widget 的两个方法：

- `func (d *PageDocument) checkXxx(s Section) error` — 文档层变量绑定校验
- `func (s Section) CheckXxx(info EntityInfo) error` — 实体层字段存在性校验

校验共享 `Section` 的 50+ 字段（`Fields` / `Actions` / `CollectionVariable` / `Selection` / `Widget` 等），但分散在 80 个文件，无分组。

后果：

1. **同一变更跨多文件**：新增 `Section` 通用字段需检查 80 个校验的 `len(s.Fields)>0` 是否遗漏。
2. **无能力归属**：`page_gauge.go`（展示）与 `page_collection_analysis.go`（数据）平级，无从判断职责范围。
3. **测试碎片化**：每个 widget 1 个测试文件，80 个测试文件中 half 为“widget 错配时拒绝”的相同样板。

## 2. 决定

### 2.1 按能力分组

```
platform/page/              // 新目录，替代 platform/page_*.go 平铺
├── document.go             // PageDocument + Section 的核心
├── layout.go               // nodes / slots / device
├── variables.go            // 变量作用域/类型校验
├── widgets/
│   ├── presentation.go    // gauge, separator, notice, progress, spacer, timeline ...
│   ├── inputs.go          // boolean-input, choice-input, date-input, text-input ...
│   ├── charts.go          // heatmap, histogram, treemap, chart, sparkline ...
│   ├── lists.go           // record-list, record-card, record-picks, kanban ...
│   └── analysis.go        // collection-analysis, filters, facets, pivot ...
├── validation/
│   ├── document_check.go  // Dispatch：按 widget 分发 checkXxx
│   └── bindings.go        // 通用：无 widget 时 business fields 必须为空
└── widget_registry.go     // WidgetID → 元数据（原 page_widgets.go 的 Widgets 映射）
```

- 同类 widget 共一文件：presentation (10 widgets) → `presentation.go` 约 300 行，替代 10 个 27 行小文件+头尾重叠。
- 通用守卫抽为 `validation/bindings.go`：`len(s.Fields)>0 || len(s.Actions)>0 || s.CollectionVariable!=""` 的 80 处重复收敛为 1 个 `noBusinessBindings(s)`。

### 2.2 不改变校验语义

- 每个 widget 的错误文案保留原样（前端依赖文案匹配做提示）。
- 新增 widget 只需在对应组文件加 1 个 `checkXxx`，不新增文件。

### 2.3 前端同步

- `web/packages/app/src/widgets/` 的 40+ widget 文件同步按同分组收敛为 5 组插件文件，已有 `plugins.ts` 注册表。
- 后端分组完成后，前端的 `widgets/` 的重复 `plugin.test.mjs` 可删（后端校验即权威）。

## 3. 影响

- `platform/` 文件数从 270 → ~150（-44% 文件数），但更重要的是“改展示相关只进 `widgets/presentation.go`”的归属清晰度。
- 通用绑定检查从 80 处 → 1 处。
- 预计净减 ~500 行重叠头尾与样板测试。

## 4. 删除条件

- 校验行为一致：`go test ./platform/... -run Page` 全过。
- 平铺文件彻底删除后，`go vet` 无 orphan 方法（`checkXxx` 仍被 `document_check.go` 的 dispatch 调用）。

## 5. As built

- [x] 决策已接受
- [ ] `platform/page/` 目录建立与分组迁移
