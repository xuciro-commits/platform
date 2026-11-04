---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

平台基础阶段 P1–P5 已完成；当前只保留未完成项。

## 当前批次

### 全栈收敛（2026-11-04）

按本次全栈架构与产品体验审计的根因分批推进。每条完成并经过对应验收后打勾；旧实现随替换路径同批删除。发布四族与旧页面格式的保留理由仍写在 `docs/7-system/README.md`。

**P0：正确性与安全网**

- [x] C0 运行并记录 `make verify` 基线；补 SQL 模式、viewer/discoverer、项目委派和恢复验收。
- [x] C1 资源创建必须携带项目；删除普通资源的隐式 `workspace` 归属；`Application.Resources` 只表示组成。
- [x] C2 迁移现有 `Resource.Project=workspace` 且只被一个项目引用的资源；保留显式 `build.resource.move` 作为纠正路径。
- [x] C3 增加项目委派构建者验收：创建对象/页面/动作后，资源配置、Files、项目页和项目权限一致。
- [x] C4 动作定义只经 ActionType 资源编辑；对象保存不再接受日常 `actions` 写入，旧内嵌动作只走一次显式迁移。
- [x] C5 删除对象生命周期画布对 `process.actions` 的直接写模型，改为编辑 ActionType 或其显式编辑契约。
- [x] C6 Files 成为所有成员可见的外壳入口；viewer/discoverer 不再依赖 builder 包加载；删除重复 `projects/files` view 与未注册的 `ProjectsList`。
- [x] C7 RID 成为资源路由的第一身份；旧原生 ID URL 只做重定向；对象统一到规范对象 URL。
- [x] C8 修复 MCP 提案确认链接到实际 run 视图；明确 A2A 的问答/执行语义并删除无效 `act` 承诺。

**P1：领域与服务收敛**

- [x] C9 把本体元数据、类型检查和动作类型从 `build` authoring 抽到 `services/ontology`；`Build` 只保留 authoring 编排。
- [x] C10 把对象投影、合并、ObjectSet 和对象分节读取收口到 `services/objects`；host 只留 HTTP 与装配。
- [x] C11 把连接、源、监听器、观测源、数据集、管道、调度、回写、匹配迁到 `services/data`。
- [x] C12 把函数 authoring、自动化定义、页面和应用 authoring 分别迁到 `functions`、`automate`、`apps`，删除 Build 中对应 owner。
- [x] C13 建立统一应用登记：后端 Manifest ID、前端 `defineApp` ID、标题、角色、资源和路由同源；清理 `Builder/Files`、`Settings/Control Panel` 等冲突。
- [x] C14 新增租户可创建、可发布、可安装的接口资源；应用 Manifest 接口作为兼容来源并入同一注册。
- [x] C15 建立 Product → Contents → Candidate → Version → Installation/Upgrade 主链；Marketplace 只消费产品版本。
- [x] C16 对象详情改为按分区读取；Page Model 的过滤、安全、排序、分页和字段投影编译进 ObjectSet，禁止先拉 10,000 行再内存处理。
- [x] C17 收敛 Search：一个结果合同覆盖命令、当前应用、应用、资源、对象、成员、知识；Search 页与 Command Palette 共用 provider。
- [x] C18 明确 `work.view`（个人保存视图）与 `build.query`（可发布对象集资源）的边界、命名和权限；保留两种作用域，删除竞争语义。
- [x] C19 默认对象视图实现 M21；完整页与 Inspector 共用同一视图；`RecordPage` 退回非对象记录。
- [x] C20 资源页分区回到侧栏；`ContentTabs` 只服务编辑器局部模式；删除 Control Panel、Dataset、Observation 和记录页的资源级页内二级 tabs。
- [x] C21 Home 对所有可能审阅者显示提案；AIP Assist 使用当前页面上下文；Applications 业务组按产品或项目分组；补 Help 固定入口。

**P2：清理与一致性**

- [x] C22 Control Panel view 增加统一 `for(host)`/route guard；直接 URL 不注册无权限页面。
- [x] C23 发布资产类型、前端 route、AssetKinds、publication、installation 和 release kind 收口到同一资源元数据源；四族发布实现仅在出现具体行为 bug 时重写。
- [x] C24 明确 TestPlan 与 EvaluationSet 是 variant 还是独立类型，并按 Agent Studio/DevOps 的 owner 分别路由。
- [x] C25 清理遗留死代码、重复导航、陈旧测试注释和错误的“无差距”声明；更新架构规范与必要的 `AGENTS.md` 规则。

## 完成记录

发布四族与旧页面格式的保留理由写在 `docs/7-system/README.md`。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “kernel”一词指三处：`capabilities/server/kernel`、`contract/go/kernel`、`web/packages/kernel`。
- Workspace 构建仍有大于 500 kB 的分块与三处无效动态导入告警；先测量实际加载成本，再决定拆包。
- Object Explorer 仍以对象列表和 Search Around 为主，M10 的完整对象集构建器尚未成为业务用户的主探索路径（`1-enterprise`、`4-interaction/workbenches`）。
- 治理资源与容器尚未采用同一路由入口（`7-system/web`）。
- 授权上下文在宿主根包构造，未进入 `services/security`（`6-domain/security`）。
- 少数位置直接编译 Expr，绕过可注入的规则引擎（`6-domain`）。
- `SetBuilder` 的 `domain` 条件行仍由命名查询依赖（`4-interaction/patterns` M10）。
