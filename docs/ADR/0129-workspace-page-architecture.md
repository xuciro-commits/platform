# ADR-0129 — XXL：工作区、页面定义与编辑交互收敛

状态：接受执行（2026-10-17），归 ADR-0125 XXXL。目标是一个一致的构建/操作体验，不以重新设计组件库替代减复杂度。

## 所有权决定

- `Workspace` 唯一拥有 tab/dock、最近项、favorites、shell-level route 和未保存关闭协调；业务 app 拥有自己的领域导航，不能复制 shell 状态。
- Page UI JSON 与 Go `platform/pageui` 是页面/widget 合约与结构校验权威；运行时 renderer 与 Build Inspector 分别实现渲染/编辑，不重复验证同一合约，不维护第二份 widget ID/生命周期注册表。
- `@platform/app` 提供可授权的数据与页面组合原语；`@platform/ui` 提供交互组件。Build Inspector 编辑定义，运行时渲染定义；两者不得各自解释事务、权限、变量或发布版本。
- 表单草稿在一个原始输入 owner 中；不得同时存在组件局部状态、shell 状态和表单框架的影子版本。

## 当前发现与施工范围

`web/packages/ui/src/shell/Workspace.tsx`（约 900 行）混合布局恢复、导航、dock 生命周期、用户菜单和 command palette；`Records.tsx`（约 870 行）同时承载 record mapping、table/kanban/pivot/chart、历史和详情；Build `editor.tsx` 与 workspace `App.tsx` 是重要 composition roots。先逐责任找重复/耦合，再合并同类交互；不按文件长度机械拆成几十个组件。

1. 明确 workspace 的 route/layout/scope 状态owner，并消除 `App` 与 `Workspace` 对 app ownership 的重复推导。
2. 对 Records 的 list/record detail/history/actions 开启路径梳理，保留公共操作一致性；UI 组件的呈现和调用服务分离但不新建第二套数据客户端。
3. Build Inspector 与 runtime widget 使用同一 manifest/type；Inspector 的特殊字段编辑与普通通用表单使用一个保存/草稿路径；未知版本显式拒绝。
4. 优化热交互时用 React Profiler/浏览器 trace 观察导航、记录表与页面画布；只对可重复的实际重复渲染/重复查询做更改。
5. 统一应用列表、资源详情、引用选择、编辑、执行的打开方式与返回语义；移除被替代入口，保留 shell-level unsaved guard、键盘和窄屏操作。

## 验收

编辑、预览和运行使用同一页面含义；应用切换保留草稿/返回位置；选择、拒绝、加载、空值、授权遮蔽在常用组件保持可理解；高复杂字段可完整编辑。无需手写底层定义完成一个页面任务。验证至少覆盖应用导航、页面保存/放弃、字段表单、表格筛选分页及一个包含 query/form/action 的页面。按用户任务截图/手工观察，不伪造用户体验或性能数字。

组件数、文件数与代码行不是单独成功指标；每次改造须同时删除竞争状态/分派/重复规则，记录 production code 行净变化。