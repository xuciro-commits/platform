---
paths:
  - "web/**"
---

# 前端

- 动手前用技能 `platform-design`；页面先声明原型（docs/4-interaction §4.3），组件先查 docs/4-interaction/patterns.md（M1–M22），工具布局查 workbenches.md。
- 只用 `@platform/ui` 的组件与令牌；需要新组件时先在 patterns.md 登记，再在 ui 包实现，不在应用里另写一份。
- 读取只经 `useReadQuery` 等现有钩子，写入只经 `decide`；不直接 `fetch`。
- 每个可见字符串经 `t()`，同时在所属包的 `i18n.ts` 加 `zh-CN`；术语取自 docs/glossary.md。
- 不改 `packages/kernel/src/gen`；宿主类型变了就重新生成。
- 改动后按 docs/7-system/verification §7.8 检查；报告改了哪些屏幕，留给负责人走查。
