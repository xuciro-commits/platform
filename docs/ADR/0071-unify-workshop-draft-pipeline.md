# ADR-0071 — 统一 Workshop 草稿与编译管线：50+ 碎文件的单管线

<a id="adr-0071"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项）。

## 1. 问题

`web/packages/build/src/workshop/` 有 100+ 文件，其中两类碎片最严重：

### 1.1 Page Editor 的草稿碎片

```
workshop/page-editor/
├── draft.ts              200+ 行 — 草稿状态 + 历史
├── diagnostics.ts        150+ 行 — 错误收集
├── canvas-layout.ts      180+ 行 — 节点增删改
├── compatibility.ts      100+ 行 — 旧格式兼容
├── clipboard.ts          80+ 行  — 复制粘贴
├── context.ts            — 变量上下文
└── 20+ widget 子文件
```

`draft.ts` / `diagnostics.ts` / `canvas-layout.ts` / `compatibility.ts` 各自持一份“当前 PageDocument + Sections”的视图，修改后需 4 处同步。

### 1.2 Module Import 的编译碎片

```
workshop/module-import/
├── application.ts        200+ 行
├── flows.ts              150+ 行
├── compile.ts            882 行 — God file
├── diagnostics.ts        100+ 行
├── ai.ts / catalog.ts / spatial.ts ... 8 个领域子编译器
└── 10 个 default-*.fixture.mjs
```

`compile.ts` 882 行为 God 函数 `compileWorkshop()`，内部 `if (category === "analytics")` 分 8 个领域分支，各自手写 `section.widget = "..."` 的组装。

## 2. 决定

### 2.1 Page Editor 单管线

```
workshop/page-editor/
├── pipeline.ts      // 唯一状态：{document, sections, history, diagnostics}
│                    // 唯一入口：apply(action) → {pipeline, diagnostics}
├── actions.ts       // 增删改节点 / 绑变量 / 设布局 的纯动作
├── queries.ts       // 派生：可用变量、布局预览、错误清单（selector）
└── widgets/         // 各 widget 的 Inspector（保留专用）
```

- `draft.ts` + `diagnostics.ts` + `canvas-layout.ts` + `compatibility.ts` → `pipeline.ts`。
- 校验与预览是派生，不存两份 `PageDocument`。
- `clipboard.ts` 保留（独立 concerns：剪贴板与草稿无关）。

### 2.2 Module Import 单编译器

```
workshop/module-import/
├── compiler.ts      // compile(workshop.json) → {pages, flows, datasets}
│                    // 内部按 widget 类型表驱动：widgetTable[widgetKind].compile(node)
├── widgets.json     // 已有：前端已用它生成 widget 目录，编译复用同一表
└── fixtures/        // 10 个 fixture 移至 fixtures/ 子目录，非根目录平铺
```

- `compile.ts` 882 行的 `if/else` 链改为表驱动：
  ```ts
  const compilers: Record<string, (node, ctx) => Section[]> = {
    analytics: compileAnalytics,
    operations: compileOperations,
    ...
  };
  ```
- `application.ts` / `flows.ts` 的领域逻辑变为 `compilers` 表的表项，而非独立文件。

## 3. 影响

- Page Editor 的 4 个状态副本 → 1 个，无同步 bug。
- `compile.ts` 从 882 行 God 函数 → 表驱动 150 行 + 8 个领域小函数。
- 预计净减 ~800 行重叠状态与分支。

## 4. 删除条件

- 现有 `workshop/page-editor/draft.test.mjs` 与 `compile.test.mjs` 通过。
- `compileWorkshop` 的旧产物与新产物字节一致（fixture 对照）。

## 5. As built

- [x] 决策已接受
- [ ] `pipeline.ts` / `compiler.ts` 单管线建立
