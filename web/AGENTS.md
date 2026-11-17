# AGENTS

给 AI 编程代理的项目专项说明。

下面的块由 Astryx 生成；`astryx init --agent-docs-path AGENTS.md` 会把它整块重写成英文，手译的中文随之丢失。

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
- 样式先用组件属性，再用令牌（var(--color-*|--spacing-*|--radius-*)）。不写裸十六进制与像素值。这里没有 StyleX/Tailwind 编译器，不直接用 xstyle 或工具类；有些模板用了 StyleX，需要编译器插件，见 `astryx docs styling-overview`。
- 主题：调色板与自定义颜色都走主题，永不覆盖 :root。要做或改一个主题（颜色、字体、圆角、间距、照一张图做出那个观感），优先用实验性主题编辑器，而不是手写主题文件：`astryx experimental theme editor new <name> --like neutral`，然后 `editor set <name> <thing> <value>` 或 `editor how "<你想要什么>"`；用 `astryx theme add <name>` 接上；每次改动后都用 `astryx experimental theme editor show <name> --png` 看一眼，打开它写出的 PNG（不改动应用里的东西）；没有看过图，主题改动就不算完。在代码里，扩展一个导入的主题来定制它；`theme eject <slug>` 只是分叉源码。见 `astryx docs theme`。
- 自查：重读文件；把任何裸 <div>/<span>、导入的 .css/@apply、或写死的 #hex/px，换成组件或令牌。

`astryx help` 列出全部命令。流程之外常用的：
  search "<查什么>"   找任何组件 / 钩子 / 文档 / 模板
  discover <词>   可以接入的集成
  docs <主题>   入门、原则、令牌、主题……（`astryx docs` 列出全部）
  docs cli   命令与 API 参考
  docs authoring   做一个集成包，以及它拥有的每个字段
  upgrade --from <旧版本> --apply   依赖升级之后运行
<!-- ASTRYX:END -->
