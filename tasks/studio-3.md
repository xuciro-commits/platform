# studio-3 条件与表达式编辑器

车道：studio    依赖：负责人定用哪个代码编辑器（新依赖，lead 去问）；studio-1 之后
能改：frontend/src/pages/**、frontend/src/apps/**、frontend/e2e/builder.spec.ts
不碰：kernel/**；表达式只经宿主的 Expr 求值，界面不复算

## 要做到
- `ConditionBuilder`：构建者在字段的 `when` 上逐条选字段、比较与值，存成宿主认的表达式；能切到文本看原文。
- `ExpressionEditor`：`default` 与规则的表达式有补全与宿主给的报错，报错指到位置。
- 发布后的记录页按条件显示或隐藏这个字段。
- trunk：builder 加一步，用条件编辑器写 `when`，发布，记录上条件成立才出现字段。

## 交接
- 无
