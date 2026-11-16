---
paths:
  - "web/**"
---

# 前端

- 动手前用技能 `platform-design`；每个界面守 docs/4-interaction §4.1 的十三条不变式，页面先声明原型（§4.3）。
- 新写与重写的界面只用 Astryx 组件与主题令牌（`pnpm --dir web/packages/ui exec astryx component <Name>` 查用法，`astryx template <id>` 取模板）；`@platform/ui` 只放外壳与页面原型。不新增对旧自有组件、Tailwind 类或 Radix 的使用。
- 读取只经 `useReadQuery` 等现有钩子，写入只经 `decide`；不直接 `fetch`。
- 每个可见字符串经 `t()`，同时在所属包的 `i18n.ts` 加 `zh-CN`；术语取自 docs/glossary.md。
- 不改 `packages/api/src/gen`；宿主类型变了就重新生成。
- 改动后按 docs/7-system/verification §7.8 检查；报告改了哪些屏幕，留给负责人走查。
