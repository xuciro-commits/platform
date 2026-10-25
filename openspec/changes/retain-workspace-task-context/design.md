# Design

## Context

审计 F04–F07 的共同问题是销毁条件错误：Workspace `209–215/465–492` 将切应用变成关闭；Grid 的查询 key 清草稿；PageSession key 含整份 me；明细编辑缺 id，数值输入以文案/缺省名生成缓冲身份。原 DraftInputs、草稿 reducer、dirty/busy、ViewTransfers 和可见性 owner 已存在。

## Goals / Non-Goals

**Goals:** 文档和输入按稳定身份存续，展示切换不承担销毁业务任务的责任。

**Non-Goals:** 不增加跨身份草稿恢复、永久运行页缓存、全局草稿账本或业务明细 ID；不保证强制退出后保留尚未持久化的所有编辑器状态。

## Decisions

1. **在 Workspace 原 owner 内保留作用域视图。** Shell chrome 保持一份；每个已访问应用作用域按现有惰性挂载模式保留同一 Dockview 视图实例，未访问的不挂载。切换隐藏原作用域并传播 `ViewVisibility=false`，不 `api.clear()`。Home/Portal 属于唯一 shell 作用域。各实例仍使用原路由、布局序列化和 editor state，不抽取草稿再复制回编辑器。相比新建跨编辑器草稿容器，保留原树能同时保留非法文本、撤销和原输入 owner。
2. **任务地址包含作用域。** unsaved、关闭、跨编辑器 ticket 与活动路由回调绑定 `(scope,panel)`；隐藏实例不得更新当前 URL 或夺焦。显式关闭才确认/释放；切主体、退出贡献或撤权立即按原规则退订、清理原读取及不可用视图。现有 popout 仍由原 Dockview 管理。
3. **去掉非身份 key，分开任务与读取世代。** PageSession 和 ApplicationSessionHub 的任务身份使用原宿主/租户/主体、当前 page/application 确切版本与实例；不使用 raw me 或包含全租户全部定义的 source.scope 作为整树销毁键。原 source.scope 仍是读取失效世代：在 PageSessionStore.updateSource 等原 owner 内刷新/遮蔽敏感读取，保留不受影响的当前用户输入及任务位置。`lastSeen` 和无关应用发布不能清当前会话；失去当前文档资格或适用版本变化才按原保护退出/重绑定。不得冻结 me 或继续消费失效读取来修复重挂载。
4. **暂存由原 Grid 持有。** 接入既有 `useUnsavedChanges` 与忙碌注册；查询变更先经保护，选择保留时暂存独立于当前页，显式放弃才清除。去掉 domain/search/offset/error 的强制重挂载，并替换 data effect 按当前页 ID 过滤暂存的逻辑：分页/筛选缺席不等于撤权。打开 baseline、逐行答复与原键保留；真实授权撤销、记录不可读或失效继续由原读取依据处理，停止后续提交并清不可读快照。
5. **字段路径由编辑 owner 传递。** 明细 UI 使用只在本机存在的稳定行键与字段路径；行删除/重排移动自己的输入，不修改业务行形状。NumericInput/JSONTextarea 的无名兜底用组件实例唯一身份，删除 DOM 文案作为缓冲键的推断；有业务路径的调用方必须显式传入。复用 DraftInputs，不增加状态系统。

## Removal and Preservation

删除切应用时无条件 `leaveViews/api.clear`，删除 raw me key、查询变化强制清暂存及文案生成输入身份。保留真正关闭/切身份保护、原布局兼容、原草稿撤销/冲突、K5、原字段解析与服务端裁决。字段路径修复不得弱化非法文本保留测试。

## Dependencies and Ownership

预览模式由 `separate-studio-preview-from-live-runtime` 独占，本 change 只改页面实例身份。列表筛选由 `unify-record-query-interactions` 独占，双方只通过原查询变更和离开保护交互。批量退役接入 busy 的具体调用由交付 change 完成，本 change 不重复修改退役逻辑。

## Risks / Trade-offs

- [更多挂载实例占内存] → 未访问不创建、显式关闭释放、隐藏视图停止订阅/测量；按真实打开范围测量，不能为省内存静默丢草稿。
- [scope 间同名 panel/ticket 冲突] → 用带作用域的原任务地址测试并行与返回，不另建路由体系。
- [撤权和普通刷新混淆] → 分别验证 lastSeen-only 与权限变化；稳定输入不能成为保留无权快照的借口。

## Migration Plan

保持原每应用布局存储键及旧深链解码；旧布局恢复后进入保留视图，不改保存业务。先修字段/实例身份与 grid 保护，再替换作用域销毁路径。更新 Architecture 的会话寿命说明及既有错误预期测试，不迁移历史数据。
