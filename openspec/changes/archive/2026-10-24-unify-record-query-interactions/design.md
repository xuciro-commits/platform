# Design

## Context

当前 `RecordList` 的受控查询在 `Records.tsx:297/363`，枚举筛选在 `337/341/479` 另存，保存视图 `454` 遗漏筛选；ID 列 `405` 沿 DataTable 本地排序。App 的 `Records` 有导入/保存/默认编辑，`listing-plugins` 和 `RecordWorkspace` 消费同一列表却没有相同适用操作。见审计 F08/Q01。

## Goals / Non-Goals

**Goals:** 一次用户查询只有一个 owner，声明式与通用列表消费同一原操作。

**Non-Goals:** 不新建查询框架、表格注册器或业务规则；不把固定查询改成任意查询；不重做完整内存表。

## Decisions

1. **受控窗口拥有全部可变查询。** 扩展现有 QueryView/window 的变更协议承载允许的枚举筛选；`PageQueries`、`ApplicationQueries` 用原编译器将其与固定谓词组合并检查类型/预算。不能承载该筛选的固定命名查询隐藏或禁用相应操作并说明原因。`RecordList` 只发送变更，不保留一个“看似选中但不影响结果”的副本。
2. **普通列表也只组装一次有效查询。** 本地模式沿现有 ListState；新增可选枚举筛选字段，旧字节缺省为空。筛选计数从当前有效父条件、搜索和其他筛选获得；计数失败显示不可用，不能伪造全零。异步结果绑定原查询和 source scope，不覆盖后来的范围。
3. **排序和导出跟随有效查询。** ID 列头交回窗口或关闭排序；完整内存 DataTable 继续使用本地排序。不得把声明式窗口的 set/traversal/固定版本丢掉后另跑裸对象导出。
4. **共有操作放在现有 App 适配归属。** 把 `Records` 已有 Transfer、保存视图和标准编辑的动作/字段投影整理为原列表操作组合，供通用列表、list-detail 和可编辑声明式列表消费；UI 只接端口。默认可写动作取当前目录与 Page.actions 的交集：Page.actions=[] 继续禁写；Section.actions 省略表示继承页面限制，显式 [] 禁止区段写入。省略 inlineEdit 仅允许从上述交集及显示字段派生默认标准 patch，必须保留原 profile 门槛和 CheckTableEdit/tableEditableFields 的字段、无审批、无额外必填输入约束；不能把缺配置当权限或跳过 owner 检查。显式 inlineEdit 继续限定原动作/字段。预览不能写，需审批的编辑走原表单，离线缓存不授予权限；默认派生不重写已发布文档。

## Removal and Preservation

删除受控列表的独立 facet 状态/计数旁路、保存时遗漏筛选的投影以及 ID 本地排序。合并 App 层已有操作的接线，不复制 CSV/保存/动作逻辑。保留查询计划、父子选择、schemaVersion、分页上限、原始导出值和历史视图读取。

## Dependencies and Ownership

`retain-workspace-task-context` 独占暂存与未保存保护；本 change 的范围变化必须调用该既有保护，不能再清空 grid key 来解决状态问题。`scope-record-reference-reads` 独占引用呈现。两者与本 change 修改同文件不同职责；集成时保留双方代码，不重复编写 store。

## Risks / Trade-offs

- [固定命名查询不允许任意过滤] → 按原能力明确不提供操作；不扩展后台查询语义来迎合按钮。
- [保存视图引用过期字段] → 保留原条件并提示不可用，不能静默扩大结果。
- [默认操作绕过页面设计限制] → 用当前原动作目录与显式限制的交集，覆盖只读、审批及离线场景。

## Migration Plan

旧视图按原 JSON 读取，新增字段仅在用户保存时产生；已发布 Page 不重写。复用原 API，不引入业务数据迁移；若生成 API 类型发生实际变化，使用原生成命令。先连查询状态，再连原共有操作，一次完成对应包验证。
