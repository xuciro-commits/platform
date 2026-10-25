# Design

## Context

见 proposal 与审计 F02。`ApplicationPage` 的 holder/session 可选，但 `ComposedPage` 用 `!application?.preview` 开启正式模式。Studio 预览开关传 `live=true`，于是新页比已发布成员页获得更多执行能力。既有候选试运行与 `useActionTrial` 有原隔离 owner，本次不复制它们。

## Goals / Non-Goals

**Goals:** 没有应用会话也成立的作者只读边界；四种来源行为一致。

**Non-Goals:** 不实现新的模拟器，不把前端禁用按钮当服务端授权，不将页面预览升级为完整隔离应用执行。

## Decisions

1. **执行模式归页面运行入口。** Studio 明确给 ComposedPage 的作者预览模式；默认正式模式只用于工作区已发布页面入口。沿既有 embedding/binding 上下文向子页和 Widget 下传原模式，父级只读限制不可被子级或缺省值提升。应用会话继续只管理共享变量和实例，不再决定允许正式写入。必要的模式值在原 Page Runtime 上下文维护，不为每个 Widget 增加一个独立授权器。
2. **预览开关只改变呈现。** 设计态选择装饰、预览态交互和尺寸切换都不改变正式写入许可。替换“actions run for real”文案；应用未发布或不含新页时保持可预览，不要求先正式发布才能安全。
3. **所有有副作用端口复用同一模式。** actions、inline/create 表单、行内编辑、附件上传、Work、AI 与能力调用依原 binding.live/mode 同步禁用；异步任务发起与完成都核对原模式和 owner 生命周期。纯规则 WASM、获权读取及本机交互可继续。不能用拦截所有 POST 的办法，因为有些 POST 是原只读查询。
4. **执行验证留在原隔离路径。** 有现成试运行入口时链接到它，支持范围明确展示；正式验收通过已发布应用路由。维持同一个渲染器，不复制预览组件或后台规则。

## Removal and Preservation

删除靠可选 application context 才能阻止正式动作的门控，以及与行为矛盾的提示。保留发布页版本、应用共享变量、Widget 契约、端口校验、查询预算、正式动作权限和原候选试运行。与 task-context change 的交点仅是同一文件中的不同职责，先固定模式再调整会话 key。

## Risks / Trade-offs

- [过去有人用预览操作正式数据] → 新行为明确将其导向已发布应用，不能保留隐蔽开关恢复不一致路径。
- [少数 Widget 绕过 live] → 针对副作用端口列出调用点，组件级 spy 检查实际 host/client 调用；不能只测 disabled 样式。
- [嵌入与异步回调逃逸] → 覆盖多层嵌入、切页、关闭后回调，不以静态布尔表达式测试替代渲染级验证。

## Migration Plan

只修改作者入口和原模式传播，不重写 Page 文档或发布字节。正式 Runtime 用例作为正对照；作者预览四象限均须阻止正式副作用后才完成实施。隔离试运行的历史及正式数据保持原 owner。
