---
paths:
  - "frontend/**"
  - "web/**"
---

# 前端

- 新前端在 `frontend/`，是独立项目；`web/` 是待删除的上一套实现，只修缺陷，不加功能，不作为新前端的参考（它的结构、命名与组件都不沿用）。
- 在 `frontend/` 动手前用技能 `platform-design`，并读 `frontend/AGENTS.md`（Astryx 官方的代理说明）。每个界面守 docs/4-interaction §4.1 的十三条不变式，页面先声明原型（§4.3）。
- 页面从 Astryx 官方模板起步：`pnpm --dir frontend astryx build "<想法>"` 找模板，`astryx template <id> <路径>` 落地，`astryx component <Name>` 查属性；不凭记忆写属性。
- 只用 Astryx 组件与主题令牌；不写裸 `div`，不硬编码颜色与尺寸，不引入第二套组件库或样式方案。
- 读取与写入只经宿主合同（docs/7-system/web R3）；组件里不直接 `fetch`。
- 每个可见字符串经 `t()`，同时在 `src/i18n.ts` 加 `zh-CN`；术语取自 docs/glossary.md。
- 改动后运行 `make check-frontend`，逐条按报错改到通过，不改检查脚本；再在浏览器里看一眼。报告改了哪些界面，留给负责人走查。
- 只做一个入口的任务（路线图 W7 的任务卡）用技能 `frontend-migrate`：只在 `frontend/src/apps/<应用>/` 新建文件并给 `src/i18n.ts` 加行；外壳、路由、页面原型、`src/host/`、依赖与规范不动，做不下去就停下报告。
