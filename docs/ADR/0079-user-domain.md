# ADR-0079 — 用户域：身份、成员、档案、偏好与生命周期（XL）

状态：接受，待实施 · 2026-10-06 · 承接 ADR-0023（语言）、ADR-0067（企业模型中的人）、ADR-0078（租户与授权）

## 0. 为什么现在

今天的"用户"只是 `platform.Member{ID, Tenant, Roles, Agent, Language}` 加一张 `subject → member` 表：没有姓名、邮箱、头像、时区、通知偏好，没有邀请/停用/离职，没有"我的账户"。时区尤其要紧——ADR-0076 的记账日期、`core.period` 的归属、"今天"的默认值，现在都按 UTC 或浏览器算。本 ADR 把用户拆成四层并给每层一个归宿，不引入外部用户目录。

## 1. 四层，各一个归宿

```
Identity  ──signs in as──▶  Member  ──has one──▶  Profile
(IdP 主体)                  (租户内账户)            (姓名/联系/偏好)
                               │
                               └──is placed as──▶  Person（企业模型 ActualPerson，可选）
```

| 层 | 记录 | 归宿 | 内容 |
|---|---|---|---|
| **Identity** | `platform.identity`（成员的子列表） | Console | `subject`（`user:<email>` / `client:<id>` / `oidc:<iss>#<sub>`）、`issuer`、`verified`、`lastSignIn`、`label`（"工作邮箱""SSO"） —— 一个成员可有多个身份；身份不跨租户共享 |
| **Member** | `platform.member`（**成为真正的实体**，取代 Console 内存 map） | Console | `id`、`kind` person/service/agent、`status` invited/active/suspended/offboarded、`grants[]`（ADR-0078）、`invitedBy`、`joined`、`lastSeen`、`primaryUnit` |
| **Profile** | `platform.profile`（与 member 同 ID） | Console | §2 |
| **Person** | `enterprise.element` stereotype Person（已有） | enterprise | 岗位、单位、汇报线、`From/Until`；通过 `member:<id>` 挂接（ADR-0067/0073），一人一个；离职只写 `Until`，不删 |

**边界**：Profile 是"这个人希望平台怎么对他"，Person 是"组织怎么安排他"，Member 是"他在这个租户里能做什么"。三者谁也不包含谁；企业模型同步（ADR-0073）可以**建议**创建成员，不会自动授予角色。

## 2. Profile：一张记录

`platform.profile`（成员本人可改大部分；admin 可改全部）：

- **称呼**：`displayName`、`givenName`、`familyName`、`avatar`（files 应用的文件引用）、`title`（职衔）、`pronouns`
- **联系**：`email`（通知收件，默认取 `user:` 身份）、`phone`、`contactVisibility` tenant/unit/none
- **本地化**：`language`（取代 `Member.Language` 与 `platform.member.language`）、`timezone`（IANA）、`dateFormat`、`numberFormat`、`weekStart`、`measurement` metric/imperial —— 空值继承 `platform.tenant`
- **通知偏好**：`channels{inApp, mail}`、`digest` instant/hourly/daily、`quietHours{from,to}`、`mute[]`（按应用或按告警规则）
- **工作偏好**：`homePage`（启动落点）、`density`、`theme`、`landingApp`、`recentLimit`
- **可达性**：`reduceMotion`、`highContrast`、`fontScale`
- 只读：`lastSeen`、`lastSignIn`、`sessions`（§4）

## 3. 偏好真正生效的地方

- **时区**（最重要）：宿主的 `Caller` 带 `Location`；所有"今天/现在"的默认（动作 `$now`、ADR-0076 的凭证日期、期间归属、告警的 quietHours、流程的截止）按成员时区计算；API 返回仍是 UTC，`Clock/Date` 组件按 profile 渲染；表格导出按成员时区写本地时间列。
- **语言**：`t()` 取 profile.language → tenant.language → 浏览器（已有链路，换数据源）。
- **通知**：`Notify` 按 `channels/digest/quietHours/mute` 投递；digest 由一个定时作业合并（work 应用）；mail.go 读 profile.email。
- **主页与密度**：工作台启动读 `homePage`；ui 的密度/主题开关落盘到 profile 而不是 localStorage。

## 4. 生命周期与自助

- **邀请**：`platform.member.invite {email, grants[], unit?, message}` → 成员 `invited` + 一条带签名链接的邮件（开发令牌环境打印链接）；首次用该邮箱的身份登录即 `active` 并绑定身份。`signInDomains`（ADR-0078 §2.1）里的域可以**自助加入**为 `kind=person` 的无授予成员。
- **停用/恢复**：`suspend/resume`：停用立即使令牌失效（宿主维护撤销表 `revoked[member] = since`，`authenticate` 比对签发时间）、保留一切。
- **离职**：`offboard {successor?}`：收回全部授予与委托、把其拥有的待办/审批/未完成流程转给 successor（work/flow 已有重新分配动作）、企业模型 Person 写 `Until`、Profile 保留供历史显示（名字仍能出现在审计里）。不删除记录。
- **服务账号**：`kind=service`，身份 `client:<id>`，只能被 admin 建、必须有 `Until`，令牌沿宿主签发（ADR-0010 路径）。
- **我的账户**页（每个成员）：Profile 全部可自改项、我的身份（看/解绑非最后一个）、我的授予与委托（只读 + 发起委托）、我的会话（设备、最近活动、"退出其它所有会话" = 撤销表）、我的通知偏好、下载我的数据（profile + 我发起的决策清单，JSON）。
- **Members** 管理页（admin）：列表（状态、单位、最近活动）、邀请、成员详情 = Profile + 身份 + 授予（ADR-0078 B）+ 活动（该成员的审计）+ 停用/离职。

## 5. 会话与安全（有界）

- 不自建密码与 MFA：仍由 IdP（rauthy/OIDC）承担；租户的 `mfaRequired` 作为 ACR 要求传给 IdP，宿主只校验令牌里的 `amr/acr`。
- 会话 = 宿主对令牌的记忆：`sessions` 列表（issuer、首次/最近见到、UA 摘要），撤销表支持"退出其它会话"与 admin 的 `suspend`。
- 个人 API 令牌：`platform.token {label, scopes[]（权限模式）, until}`，宿主签发短期 JWT 的替代——给集成与脚本用，受 ADR-0078 引擎同一判断（`ctx.channel=api` 且权限 ⊆ 令牌 scopes）。

## 6. 分块与顺序（XL）

| 块 | 内容 | 规模 |
|---|---|---|
| **A 成员实体化 + Profile** | `platform.member`/`platform.identity`/`platform.profile` 成为 Console 账本实体（快照/回放沿账本）；`Member.Language` 迁到 profile；My account 页（称呼/本地化/通知）；Members 页读新实体 | L |
| **B 偏好生效** | `Caller.Location` 与所有"今天/现在"；通知 channels/digest/quietHours；主页/密度落盘 | M |
| **C 生命周期** | invite/suspend/resume/offboard、撤销表、自助加入、服务账号。**已交付 2026-10-06**：`Member.Status` ∈ {active(空), invited, suspended, left}；`platform.member.invite{subject}`（尚未登录者先入目录、可先授予；首次登录即转 active——由宿主的 `lastSeen` 内存派生，不是决定）、`.suspend{reason}`（角色留在记录上但一无所持、不能操作，会话全部结束）、`.resume`、`.offboard{reason}`（登录身份与令牌移除、授予清空、记录与历史保留，不可逆；`Member(subject)` 不再认得）；不能改自己的状态。Members 页「Standing」列与成员页「Standing」面板。「撤销表」即宿主的 `sessionTable.revoked`；「自助加入」不做（邀请即席位，SaaS 外部注册不属本期）；服务账号仍是 `platform.member.add` 的 `client:` 主体 | M |
| **D 会话与令牌** | sessions 视图、退出其它会话、`platform.token`。**已交付 2026-10-06**：`platform.token.issue{label,scopes[],until}`（任何成员，target=`platform.token/<id>`；密钥 `pat_…` = HMAC(宿主签名密钥, 租户/令牌/决定 change id)，账本与状态都不存密钥或哈希，`GET /v1/tokens/{id}/secret` 签发后十分钟内对签发人给一次；持令牌者得 `Member.Scopes`，越界动作被引擎以 `token-scope` 拒绝；令牌不能再签令牌）、`platform.token.revoke`（本人或管理员）、`GET /v1/tokens`；`Host.member` 接受 `Bearer pat_…`。会话：宿主内存按凭证哈希记 sign-in/token、UA 摘要、首末时间，`GET /v1/sessions`，`POST /v1/sessions/end-others` 让宿主此后拒绝其它凭证；停用/离职/撤销令牌同时结束会话。My account 新增「Personal tokens」「Sessions」两栏 | S |
| **E 减法** | 删 `Console.members` 内存 map 与 `subjects` 表、`SchemaLanguage`、`MemberView`、`Identities()` 开发令牌清单改从实体派生；`directory.json` 的 seats 变成首次导入。**已结 2026-10-06**：`SchemaLanguage` 与 `directory.json` 已不存在；`Console.members`/`subjects` 就是 Console 的已决状态（随账本快照/回放），`MemberView` 是生成到 `host.ts` 的应答类型，`Identities()` 已从该状态派生——三者是模型本身而非残留，保留；`Profile.LastSeen`、令牌 `LastUsed` 与会话改为宿主内存，不再污染已决状态 | S |

A 可与 ADR-0078 A 并行；B 依赖 A；C 的授予部分依赖 ADR-0078 B。

## 7. 退出判据

- 新成员经邀请邮件加入，设置时区 Asia/Shanghai 后，在 23:30 过账的收货凭证记入本地日期的期间；凌晨的告警按 quietHours 延后到 08:00 合并投递；
- 离职一名主管：其待办与审批转给继任者，授予全部收回，审计里仍显示其姓名，企业模型岗位写 `Until`；
- "退出其它会话"后旧令牌被拒；服务账号到期自动失效；
- Console 里不再有成员的内存 map：重启后成员、身份、档案全部由账本回放。

## 8. 不做

- 不做跨租户的全局用户（同一个人在两个租户是两个成员，身份可以相同）；
- 不做自建密码、MFA、SCIM 服务端（SCIM 若需要，走 ADR-0070 的 Source profile 同步到 `platform.member.invite`）；
- 不做社交资料、关注、状态消息。
