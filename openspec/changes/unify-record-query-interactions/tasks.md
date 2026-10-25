# Tasks

## 1. 单一查询状态

- [ ] 1.1 让受控 window、PageQueries/ApplicationQueries 的现有 QueryView 承载允许的枚举筛选；删除 RecordList 的无效本地筛选旁路，增加含固定父条件的窗口筛选与固定命名查询不可改场景回归，断言真正发出的原查询内容。
- [ ] 1.2 在 ListState 和保存/恢复投影中保留枚举筛选；测试旧视图缺省、多个筛选重开、过期字段提示，确认无静默扩大范围或重写旧历史。
- [ ] 1.3 将筛选计数、导出和 ID 排序接回有效查询，绑定异步结果身份；测试多页、set/traversal、固定排序和旧答复迟到；保留完整本地 DataTable 的排序测试。

## 2. 共有列表操作

- [ ] 2.1 整理 App Records 已有导入、保存视图与标准编辑接线，供声明式 table/list-detail 复用；明确 Page.actions=[] 与 Section.actions 显式[]禁写、省略区段限制继承、inlineEdit省略仅在原profile及标准patch检查内派生。测试旧profile、空allow-list、额外必填、审批、只读预览及无权状态，不复制 CSV 或业务校验逻辑，不重写旧发布。
- [ ] 2.2 范围变化接入 task-context change 的原暂存保护；覆盖筛选/分页时保留或明确放弃，并验证基线修订、逐行拒绝和原请求不被替换。

## 3. 集成验证

- [ ] 3.1 统一运行受影响 UI Records、App query-plans/plugin/Session 与保存视图测试及 `make check-web`；在既有 `record-sets` 或 `model-driven-ui` 路线验证筛选→保存→重开→导出为同一范围，按实际执行分别报告。

## Workflow follow-up

- 实施完成后同步 experience/workspace 并归档；引用缓存、预览模式和暂存 owner 分别由对应 change 维护，不新增同类实现。
