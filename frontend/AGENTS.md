# AGENTS

平台前端：只有一个。界面由类型生成。仓库根的 `AGENTS.md` 依然适用；本文件补这个目录特有的部分。

- 动这里之前先读 `docs/frontend.md`，并用技能 `platform-design`（设计打磨可配合 `impeccable`）。结构与行为见 `docs/ux.md`，观感见 `docs/design.md`，视觉令牌与产品真值见仓库根 `DESIGN.md`、`PRODUCT.md` 与 `.impeccable/design.json`。
- 三层：契约在 `src/host/`；表现在 `src/pages/`（每个由一份官方模板改造一次）；入口是 `src/apps/<应用>/<入口>.ts` 里的一句声明。永不为单个类型写页面；改表现或契约，让每个类型都得到。
- 先找官方模板或块（`pnpm astryx build`、`pnpm astryx search`）；只改数据形状和它缺的表现。
- `web/` 是旧前端，冻结。不读，也不改。
- 真实数据：仓库根跑 `make frontend-host`，`.env` 里写 `PLATFORM_HOST=http://127.0.0.1:18511`，然后在登录页选一个夹具身份。
- 跑 `pnpm dev`（http://localhost:8496）；下面的命令行写作 `pnpm astryx <命令>`。
- 每个可见字符串都经 `t()`，并在 `src/i18n.ts` 有简体中文条目。不写代码注释。
- 在仓库根用 `make check-frontend` 核对。它报什么就修什么；永不改检查。
- 下面的块由 Astryx 生成，只用 `pnpm astryx init --agent-docs-path AGENTS.md` 刷新，永不手改。这里的中文是手译，刷新时会整块换回英文。

<!-- ASTRYX:START -->
Astryx v0.7.16 · 174 个组件
命令行：每条命令都写作 `pnpm exec astryx <cmd>`（下文简写为 `astryx ...`）。

初始化（一次，写在应用入口，例如 main.tsx）：缺了这两行，组件没有样式：
  import "@astryxdesign/core/reset.css";
  import "@astryxdesign/core/astryx.css";

流程：每个页面都从模板起步，永不从空白写起：
1. `astryx build "<想法>"`：点出该用哪个模板起步、还缺哪些块。
2. `astryx template <name> <path>`：落地模板；保留它的框架、间距与内边距，只换内容。
3. 缺的块用 `astryx template <Block>`；用一个组件之前先 `astryx component <Name>` 读它的属性。
改已有页面？保留它：跳过第 2 步，在它的各个分节里加块或组件。
交付之前跑 `astryx doctor --built <dir>`；不通过就不交付。

规矩：
- 不写 <div>：布局与间距都由组件负责。
- 改模板的框架之前先读 `astryx docs layout`。
- 密数据用行（Table、List/Item），不用卡片包列表。Badge 只放计数。
- 样式先用组件属性，再用 xstyle 属性 / StyleX 令牌。不写裸十六进制与像素值。
- 主题：调色板与自定义颜色都走主题，永不覆盖 :root。要做或改一个主题（颜色、字体、圆角、间距、照一张图做出那个观感），优先用实验性主题编辑器，而不是手写主题文件：`astryx experimental theme editor new <name> --like neutral`，然后 `editor set <name> <thing> <value>` 或 `editor how "<你想要什么>"`；用 `astryx theme add <name>` 接上；每次改动后都用 `astryx experimental theme editor show <name> --png` 看一眼，打开它写出的 PNG（不改动应用里的东西）；没有看过图，主题改动就不算完。在代码里，扩展一个导入的主题来定制它；`theme eject <slug>` 只是分叉源码。见 `astryx docs theme`。
- 自查：重读文件；把任何 className=、style={{…}}、裸 <div>/<span>、导入的 .css/@apply、或写死的 #hex/px，换成组件或 xstyle 属性加令牌。

`astryx help` 列出全部命令。流程之外常用的：
  search "<查什么>"   找任何组件 / 钩子 / 文档 / 模板
  discover <词>   可以接入的集成
  docs <主题>   入门、原则、令牌、主题……（`astryx docs` 列出全部）
  docs cli   命令与 API 参考
  docs authoring   做一个集成包，以及它拥有的每个字段
  upgrade --from <旧版本> --apply   依赖升级之后运行
<!-- ASTRYX:END -->
