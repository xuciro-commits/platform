---
paths:
  - "frontend/**"
---

# 前端

- 只有一个前端：`frontend/`。
- 动手前读 docs/frontend.md，用技能 `platform-design`（设计打磨可配合 `impeccable`）。界面守 docs/ux.md 的十三条不变式、docs/design.md 的“不许出现”与仓库根 `DESIGN.md` / `PRODUCT.md`。
- 界面由类型生成：契约在 `src/host/`，表现在 `src/pages/`，入口是 `src/apps/<应用>/<入口>.ts` 里的一句声明。
  不为单个类型写界面；入口缺什么，改表现或契约，让所有类型同时得到。
- 先找官方的：`pnpm --dir frontend astryx build "<想法>"`、`astryx search "<要什么>"`。落地模板后只做三件事：
  把示例数据换成契约，补模板缺的表现，改造完删掉落地的原件。模板的框架、间距与交互不动。
- 属性先查再用：`astryx component <Name>`。只用 Astryx 组件与主题令牌；不写裸元素，不硬编码颜色与尺寸。
- 读取与写入只经 `src/host/`。每个可见字符串经 `t()`，同时在 `src/i18n.ts` 加中文；术语取自 docs/glossary.md。
- 改完定点核对（按需 `pnpm --dir frontend typecheck`），完整 `make check-frontend` 由 CI 执行；用真实数据在浏览器里看（`make frontend-host`）。
- 汇报改了哪些界面、负责人该看哪个地址；没在浏览器里看过的，要说没看过。
