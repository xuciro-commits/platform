# ADR-0027 — 租户隔离、设置与权限管理

<a id="adr-0027"></a>

状态：已实现有界租户和权限管理；包含共享运维职责。授权引擎、合成顺序及来源边界归 ADR-0044；用户身份/档案归 ADR-0028。取消早期独立 platform.tenant 实体、表达式策略和岗位自动授角色的方案，不再把它们列作工程欠账。

## 实际边界（As-built）

以下保留原实现的准确支持范围；已被取代的设计稿不再与此表并存。

| 正文的说法 | 代码里的事实 |
|---|---|
| "一个授权引擎，UI/API/智能体/自动化/导出全部经过 `authz.Decide`" | ADR-0044 已接通动作、行/字段/来源、CSV 标记、集合和接受依据；命名读取、输入、作业与回调共用同一引擎，独立 `readsField` 判断已删除。`TestAuthorizationNineEntryFamilies`、宿主管理与来源投影检查的准确范围归 [ADR-0044 As built](0044-one-authorization-engine.md#as-built)。这些证据不证明任意新原生算法都正确声明来源，也不等于生产验证。 |
| "RBAC + ReBAC + ABAC 三种判断一个顺序" | 当前固定九层：租户 → 主体 → deny → 动词 → 行 → 字段 → 来源 → 标记 → allow。Scope 的 own/participant/through 是 owner 的行事实，保留多角色并集与有界来源；不实现 `related` 或任意关系遍历。 |
| "`platform.tenant` 一条记录" | **没有这个实体**。租户信息是 `platform.setting` 的若干键（名称、语言、时区、币种、`signInDomains`、`mfaRequired`、`sessionHours` …）在 `GET /v1/tenant` 里拼成的 `TenantRecord` 视图。 |
| 租户设置 `signInDomains` / `mfaRequired` / `sessionHours` | **已生效**：命中登录域的已验证陌生身份，在满足 MFA 要求后由宿主记 `platform.member.join`，成员 id 为邮箱 slug、重名加序号，无角色直到授予；并发加入在租户锁内重新检查身份与分配 id。`sessionHours` 从该凭证首次见到起计时，到期凭证持续拒绝，须换新凭证；0 不限。会话/撤销仍为内存，重启清空，不检查 IdP 的 `auth_time`。`mfaRequired` 要求签名验证后的 `amr` 明示 `mfa`，或包含知识/持有/生物中的至少两类方法；`user`、单独 `otp/hwk/sms/swk` 不能证明多因素（[RFC 8176](https://datatracker.ietf.org/doc/html/rfc8176)）。开发/lightweight 宿主无法证明时拒绝登录。个人令牌走独立授权路径，不受三者约束。 |
| 授予带单位与时效 `Grant{Unit, Structure, From, Until}` | `From/Until` 过滤当前角色；行范围按每个角色/授予的谓词取并集：`Unit` 只收窄 unit/below 层级，all/own 保持原有范围；带单位授予从该单位起，below 仅沿 `Scope.Structure` 的有效 Placement 展开，与 scope 不符的显式 Structure 不扩大 below；无单位授予与旧席位按成员所属单位计算。单位关闭/关系到期不继续展开。只约束记录可见性；`Caller.Units`（决策内）仍按所属单位。 |
| 策略有"表达式变量" | 策略是 deny/allow × 权限或前缀 × `where{member, app, agent, target}` **精确匹配** × 起止日。没有表达式语言、没有记录属性条件（`Policy.When` 只是 Go 内部钩子，租户无法写）。 |
| 角色可由企业模型岗位推出 | 未做。角色只来自授予（直接、团队、委托）。 |
| 模板建租户 | 已做：`deploy/templates/{blank,hospitality,manufacturing}.json`、`GET /v1/host/templates`、`POST /v1/host/tenants`、宿主控制台入口。 |
| 租户代表组织与来源关联 | `platform.setting.enterpriseRoot` 可选引用本租户有效组织，只说明租户代表谁，不自动改名、建子租户或授予。Enterprise TenantLink 用明确来源 tenant/root/Connection 预览并导入只读结构，来源、凭据与模型历史仍由原归属负责。 |
| 自定义角色 / 策略 / 团队 / 委托 / explain | 已做，具体声明与管理动作见下文。 |

入口实现和检查范围归 ADR-0044 As built；单位范围已按上表生效，不把指定入口与反例的检查外推为所有未来实现或生产环境的证明。

## 1. 四个边界

Tenant 是身份、账本、密钥、审计和运维隔离单位；Enterprise 表达租户内的法人/工厂/部门/人/岗位与有效关系。组织层级本身不是另开租户的理由，只有数据主权、独立 IdP、备份/升级责任等隔离需要才另开租户。Deployment 是宿主运行位置；Application 是权限和写入的命名空间。

跨租户不共享存储或做联合事务。授权支持会话和企业模型的显式只读切片各沿原受控入口，不形成租户父子权限或集团直接写子公司模型。企业模型的单位与 Scope 的有效结构提供 unit/below 范围，不自动产生角色。

同一集团的子组织默认在同租户模型内表达；有独立数据、身份或运维责任时，仍由 Host Console 显式创建独立租户。创建租户不会复制 Enterprise 子树、成员、岗位或授权。需要集团视图时，由来源显式共享元素，接收方用 TenantLink 的 ready HTTP Connection 预览结构，再提交原导入动作。代表组织、来源关联与租户 id 是三个不同引用，不能推断彼此的权限或生命周期。来源连接与手动预览/确认的实际边界见 ADR-0022、ADR-0034。

## 2. 租户生命周期与设置

租户视图由 platform.setting 投影成 TenantRecord，没有第二张 tenant 实体。语言/时区/币种仍从原设置读取，SettingLanguage 保留为账户有效语言的默认回退。应用自身的 Settings 继续由 Manifest 声明，不能扩成任意代码。

Control Panel → Organisation 的 Representative organization 保存原 `enterpriseRoot` 设置，可打开 `enterprise?element=<id>&section=organization`。设置改变不重命名企业节点，节点改名不重命名租户；该引用参加企业元素的关闭/删除使用检查。People and places 的 Core 记录、成员授权和企业人员/任职仍分别维护，新增显式引用不构成自动同步。

原 Host Console 的模板目录、POST /v1/host/tenants、显式 -host-admins 与 lifecycle 管理共同负责租户创建/停用/退役；宿主管理主体与租户 admin 分开。模板含应用、组织种子、默认设置与首位管理员，持久目录取代旧 directory.json，重启沿原日志恢复。不存在自动按组织层级复制权限或无条件按保留天数删客户数据的机制。

登录域、MFA 与会话时长的执行边界见上表；个人令牌使用独立路径。会话/撤销的内存范围仍明确，不把 IdP 的证明或一次登录等同于跨重启的持久会话。

## 3. 权限声明与管理

权限从 Manifest、原动作和实体读取声明产生（platform.Permissions），不要求应用再维护第二份清单。GET /v1/permissions 给出同一目录。Member.Grants 与 RolesIn/Holds 解析多授予，Catalog.ForRoles/PermitsAny、字段 ReadsAny/WritesAny 和原 Scope 复用这些角色；实际顺序与接受依据以 ADR-0044 为准，不保留旧单角色路径。

Grant 保存 App、Role、Unit、Structure、From、Until、By、Reason、At。有效期和单位只收窄原授权，不能用授予扩大对象自己的 all/own/unit/below 范围；多个有效角色和授予的可见行取原并集。

platform.role.save/remove 定义租户内某应用动作集合的自定义角色，不能覆盖原应用角色。目录变更只在持久接受后安装，删除角色同步撤销其目录权限。没有租户可写的 includes/denies/scope/fields 第二套角色语言。

platform.policy.save/remove 保存 deny/allow、权限或前缀、where{member,app,agent,target} 的精确匹配和起止日，原 Catalog.UsePolicies 读取。没有租户表达式、记录任意属性、网络位置或 related 遍历规则；Policy.When 仅为可信 Go 内部接口。Markings 保留原传播、机密读者与受限导出保护，不以“统一”删掉现有行为。

platform.team.save/remove 在成员属于团队期间合成 By=team:<id> 的团队授予，不等于企业岗位自动赋权。platform.member.delegate 允许成员在截止日内委托其原应用角色，记实际委托人，管理员可撤销；服务账号与智能体仍是受权成员，没有旁路。

## 4. 管理面、解释与审计

Roles and permissions 使用原成员授予、角色目录、策略/团队表单与解释器；My account 提供 Delegate my roles。GET /v1/access 和 /v1/authz/explain 提供实际结果。授予、撤销、角色、策略、团队和委托变更本身是正式决定，拒绝说明原因，不另建权限事实账。

构建器 access[] 仍是对象 owner 的角色权限声明，发布到同一目录；项目的角色矩阵只读该结果。管理界面看见的声明不替代真正动作、行、字段、来源与标记的执行授权。

检查方法与明确入口证据归 Testing 和 ADR-0044；不重开历史 XXL 分块、SettingLanguage 删除、岗位自动授权、自由表达式策略或第二 IAM/图数据库工程。

## 共享运维职责

K9 受属工作与定时作业沿同一宿主提交、重试和人工处理入口。当前恢复应用已保存的接受结果（ADR-0010、ADR-0037），旧输入日志按其原格式保留；取消 ADR-0027 中“恢复必须重新执行事件处理函数”的当前设计要求。空轮询不产生业务事实时不追加日志。

K8 连接器由部署绑定，Deliver 使用其服务主体；游标/心跳等诊断与启停决定分开，启停必须持久化。通知受众在接受时按成员、组织或原应用角色解析并保存，个人读取和已读动作沿原权限；不另建行业通知中心。应用设置由 Manifest 声明名称、类型、默认值与说明，管理提交检查类型，`Caller.Setting` 只读该声明内的值，设置不是任意执行代码。

`Everyone` 只表示所有合法租户成员可调用的读取声明，实际数据仍按调用者身份过滤。连接器、通知和配置不增加第二套授权；新授权统一以 ADR-0044 为准。原文邮件/移动渠道与并行工作节点的未来设想不再自动成为待实现任务。
