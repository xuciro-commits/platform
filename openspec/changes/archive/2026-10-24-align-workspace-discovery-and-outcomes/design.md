# Design

## Context

审计 F13–F16：Data Connection `for` 接纳 Integrator，但 home 指向管理员 Connectors；usePlatformHost 只返回布尔 metadataReady，App 只等 Opening；动态 import 没有失败分支；Operations 将查询失败的 data 默认为空；ExplainAccess 把 authority 作为 action，并手写答复类型。现有 React Query、useReadQuery 和宿主授权已提供所需事实。

## Goals / Non-Goals

**Goals:** 当前可用任务、读取状态和确切请求能完整到达界面。

**Non-Goals:** 不发明前端权限 DSL、第二发现目录或通用错误框架；不扩大后台读取资格，不重新提交业务来“刷新状态”。

## Decisions

1. **从当前导航选可用首页。** 原 AppUI 贡献对当前 host 产生导航和适用首页，composeApplications 在同一投影取它们。Data Connection 对非治理成员默认获权观测/血缘或原 Integration 入口，管理员保留 Connectors。原禁止深链明确显示拒绝与返回。本人账号、审计和包审阅按原资格保留；修正当前 spec 把所有非管理员一概排除的过宽陈述，不修改原权限。
2. **启动保留原读取状态。** usePlatformHost 返回必需发现的进行中/失败/重试信息，App 区分 bootstrap、声明加载和包资源失败。动态 import 的异常纳入同一启动反馈，已获权可用贡献保留原身份。不能通过强置 metadataReady 或默认空定义“解决”无限等待。
3. **监控消费 UseQueryResult 而非只有 data。** Queue、Delivery、Connector、Effect、Settings、Audit 按现有 query 的 pending/error/data 渲染，成功空集合才显示空态；错误与原重试在就近位置。离线准备沿既有 OfflineReadError，不能把它画成无记录。只整理对应消费点，不另包装一个查询层。
4. **解释使用生成协议。** ActionRefusal 构造生成的 AuthzRequest，并以 `submission.schema.name` 作为 action；主体/租户/目标按原请求。答复使用生成的 Verdict/对应别名，删除手写近似。query key 包含确切 schema 及当前 source scope，区分 HTTP 拒绝、传输失败与当前引擎拒绝。解释仍是当前依据，不冒充历史授权证据。

## Removal and Preservation

删除只有 ready 的启动成功假设、无 catch 的贡献加载、运行失败默认空集合、ExplainVerdict 手写声明及 authority→action 映射。保留原 HostOnly/Recovery、当前权限过滤、离线准备、原 K5 答复、管理员/审计员解释资格和原控制台职责。

## Dependencies and Ownership

Publisher 的菜单/交接只由 `complete-application-delivery-handoffs` 修改，本 change 处理其余发现及结果。读取缓存隔离由 `scope-record-reference-reads` 负责，不另建“安全缓存”。当前资料稳定任务身份由 `retain-workspace-task-context` 负责。

## Risks / Trade-offs

- [错误类别被英文文本猜测] → 优先使用原 query/error 和 HTTP 状态；不额外推导业务规则。
- [导航过滤再次变成权限实现] → 只消费当前 host 的既有资格和 owner 导航；服务端继续独立拒绝。
- [改变错误空态影响既有测试] → 用真实拒绝、故障、成功空数组分别验证，不能统一为“无记录”以保持绿测。

## Migration Plan

不改 API 和存储。按启动、获权入口、监控读取、动作解释四个消费边界完成；各自补行为测试后集中做适用 Web 检查。发布后旧路由仍可解析，原不可用目标有明确反馈；不重写成员偏好和历史。
