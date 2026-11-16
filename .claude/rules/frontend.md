---
paths:
  - "frontend/**"
  - "web/**"
---

# 前端

- 两条轨道并行到择优（路线图“五、前端重建”）：`frontend/` 从零重建，`web/` 整体替换为 Astryx（W10，由另一会话负责）。互不作为参考代码；在一条轨道里工作时不读、不改另一条。
- `frontend/` 的界面由类型生成（docs/7-system/web “界面运行时”）：契约在 `src/host/`，表现在 `src/pages/`，入口是 `src/apps/<应用>/<入口>.ts` 里的一句声明。不为单个类型写界面；入口缺什么，改表现或契约，让所有类型同时得到。
- 需要一种界面，先找官方的：`pnpm --dir frontend astryx build "<想法>"`、`astryx search "<要什么>"`；找到模板就 `astryx template <id> src/templates/<id>` 落地，只做三件事：把示例数据换成契约，补模板缺的表现，改造完删掉落地的原件。模板的框架、间距与交互不动；找不到才组合官方的块，不从空白写。
- 动手前用技能 `platform-design`，并读 `frontend/AGENTS.md`（Astryx 官方的代理说明）。每个界面守 docs/4-interaction §4.1 的十三条不变式，页面先声明原型（§4.3）。
- 属性先查再用：`astryx component <Name>`；不凭记忆写属性。只用 Astryx 组件与主题令牌；不写裸 `div`，不硬编码颜色与尺寸，不引入第二套组件库或样式方案。
- 读取与写入只经 `src/host/`；别处不直接 `fetch`。
- 每个可见字符串经 `t()`，同时在 `src/i18n.ts` 加 `zh-CN`；术语取自 docs/glossary.md。
- 对照真实数据：`make frontend-host` 起夹具宿主（自带一个示例销售项目：客户与商机），`frontend/.env` 写 `PLATFORM_HOST=http://127.0.0.1:18511`，登录页选夹具身份。
- 改动后运行 `make check-frontend`，逐条按报错改到通过，不改检查脚本；再在浏览器里看一眼。报告改了哪些界面，留给负责人走查。
- 按任务卡做一个入口或补一种表现，用技能 `frontend-migrate`：只动卡片列出的文件，做不下去就停下报告。
