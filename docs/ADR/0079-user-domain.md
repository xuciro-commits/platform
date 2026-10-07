# ADR-0079 — 用户域：身份、成员、档案、偏好与生命周期（XL）

状态：接受，已实现有界用户域；离职移交、按域自助加入、邀请邮件已交付 2026-10-07；多身份解绑、会话持久化不做 · 2026-10-07 · 承接 ADR-0023（语言）、ADR-0067（企业模型中的人）、ADR-0078（租户与授权）


## 实际边界（As-built，2026-10-07）

正文是设计稿；下表按代码逐项核过。**正文与本表冲突时以本表为准。**

| 正文的说法 | 代码里的事实 |
|---|---|
| 四层：`platform.identity` / `platform.member` / `platform.profile` / Person | **只有 `platform.profile` 是实体**（`profile.go`，`platform.profile.update`，随 Console 账本快照/回放）。**没有 `platform.identity`**：身份是 Console 已决状态里 `subject → member` 的字符串映射，没有 `issuer/verified/label/lastSignIn`；My account「Identities」栏（`Account.subjects`，2026-10-07）只看不解绑。**没有 `platform.member` 实体**：成员是 Console 的已决状态（`platform.member.add/grant/revoke/invite/suspend/resume/offboard/delegate` 这些决定的结果），没有 `kind` person/service/agent（只有 `Agent bool`）、没有 `invitedBy/joined`。 |
| Profile 字段清单（§2） | 实有：`displayName givenName familyName title pronouns email phone language timezone dateFormat numberFormat weekStart inApp mail digest quietFrom quietTo homePage theme density`，只读 `lastSeen`。**没有** `avatar measurement contactVisibility mute[] landingApp recentLimit reduceMotion highContrast fontScale lastSignIn`。 |
| 偏好生效（§3） | 真生效：`timezone`（`Member.Location`，记账日期/"今天"）、`language`、`inApp`/`mail` 通道、`email` 收件、`homePage`/`theme`/`density`（工作台启动读 `/v1/me`）。`digest`/`quietFrom/quietTo` 只作用于**邮件**的发出时刻（`Reach.Due`：hourly 推到下一整点、daily 推到次日 08:00、静默窗内推到窗尾），**不合并**成一封汇总邮件；站内通知不延后。 |
| 邀请发邮件、`signInDomains` 自助加入 | **2026-10-07**：`platform.member.invite{subject}` 写进目录（可先授予）后，以普通通知（`Key: invite`，收件人即新成员，地址从 `user:<email>` 派生）经租户已配置的 email 端点寄出「You are invited to <组织名>」；没有 email 端点就只是站内通知，**没有签名链接**（登录仍走 IdP）。首次登录转 active 由 `lastSeen` 派生。持久化邀请的收件地址在私有 Console 状态中解析，邮件意图与成员同批接受；追加失败不留下成员、通知或邮件，回放不重新解析地址。自助加入见 ADR-0078 对照表 `signInDomains` 行（`platform.member.join`，宿主自动化记录）。 |
| 离职转继任、Person 写 `Until` | **已交付 2026-10-07**：`offboard{reason, successor?}`——继任者须是在职成员；离职者给出的委托改为以继任者名义（无继任者则随之终止），继任者收到通知（"You take over from …"，指向各应用自己的重新指派动作；work/flow 待办**不自动改派**）；企业模型作为 `host.Listener` 听 `platform.member.offboard`，以自动化身份对该 `member:<id>` 的每条在期 Membership 记 `enterprise.relationship.end{until: 当天}`（含当天开始的任职，空有效区间表示当天已结束），任职即写 `Until`、不删。企业模型以既有 owned work 路径补记自己的决定，异步完成，不属于 Console 的同批状态。新的 accepted offboard 保留 subject 到 left 成员的拒绝绑定，登录域不会重新接纳，管理员可显式 add/invite 替换；旧 input replay 仍按历史语义删除映射，过去已删除的身份无法恢复此拒绝绑定。 |
| 服务账号必须有 `Until` | 没有服务账号这一类；`client:<id>` 身份就是普通成员，不强制到期。 |
| MFA：`mfaRequired` 作为 ACR 传给 IdP 并校验 `amr/acr` | 已按租户设置检查验证过的 `amr`，具体因子规则见 ADR-0078；没有把 ACR 要求传给 IdP，也未解释 `acr`。 |
| 会话与令牌（§5） | 已做：`GET /v1/sessions`、`POST /v1/sessions/end-others`（宿主内存撤销）、`suspend` 结束全部会话；`platform.token.issue/revoke`、`GET /v1/tokens`、`/v1/tokens/{id}/secret` 一次性取密钥、`Member.Scopes` 越界被引擎以 `token-scope` 拒绝。宿主重启后内存会话/撤销表清空（令牌本身仍按状态校验）。 |
| "我的账户"页 | 有：称呼/本地化/通知/工作偏好、我的授予与委托、我的会话、我的令牌。Identities 栏可查看当前身份；**没有**身份解绑、下载我的数据。 |

退出判据（§7）据此修正（2026-10-07）：第 1 条达成"邀请邮件（无签名链接）、时区→记账日期"，quietHours 只延后不"合并投递"；第 2 条达成"委托转继任、授予全部收回、审计仍显示其姓名、企业任职写 `Until`"，work/flow 待办**不自动改派**（通知继任者去各应用改派）；第 3 条达成前两句，"服务账号到期"不存在（用授予的 `Until` 代替）；第 4 条达成。

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
| **A 成员实体化 + Profile** | `platform.member`/`platform.identity`/`platform.profile` 成为 Console 账本实体（快照/回放沿账本）；`Member.Language` 迁到 profile；My account 页（称呼/本地化/通知）；Members 页读新实体。**部分交付 2026-10-06**：只有 `platform.profile` 成为实体；成员与身份仍是 Console 已决状态（见 E 行与顶部「实际边界」），`platform.identity` 未建 | L |
| **B 偏好生效** | `Caller.Location` 与所有"今天/现在"；通知 channels/digest/quietHours；主页/密度落盘。**部分交付 2026-10-06**：`Member.Location`、inApp/mail 通道、主页/主题/密度已生效；digest/quietHours 只推迟邮件发出时刻，不合并 | M |
| **C 生命周期** | invite/suspend/resume/offboard、撤销表、自助加入、服务账号。**已交付 2026-10-06**：`Member.Status` ∈ {active(空), invited, suspended, left}；`platform.member.invite{subject}`（尚未登录者先入目录、可先授予；首次登录即转 active——由宿主的 `lastSeen` 内存派生，不是决定，宿主重启后 invited 的活动显示需再次登录）、`.suspend{reason}`（角色留在记录上但一无所持、不能操作，会话全部结束）、`.resume`、`.offboard{reason}`（登录身份与令牌移除、授予清空、记录与历史保留，不可逆；`Member(subject)` 不再认得）；不能改自己的状态；离职委托转继任并异步结束企业任职；work/flow 待办不自动改派。Members 页「Standing」列与成员页「Standing」面板。「撤销表」即宿主的 `sessionTable.revoked`；「自助加入」按登录域受控入席（见顶部实际边界）；服务账号仍是 `platform.member.add` 的 `client:` 主体 | M |
| **D 会话与令牌** | sessions 视图、退出其它会话、`platform.token`。**已交付 2026-10-06**：`platform.token.issue{label,scopes[],until}`（任何成员，target=`platform.token/<id>`；密钥 `pat_…` = HMAC(宿主签名密钥, 租户/令牌/决定 change id)，账本与状态都不存密钥或哈希，`GET /v1/tokens/{id}/secret` 仅签发进程内、签发后十分钟内对签发人给一次，重启关闭已有令牌的密钥展示窗口但令牌仍可使用；持令牌者得 `Member.Scopes`，越界动作被引擎以 `token-scope` 拒绝；令牌不能再签令牌）、`platform.token.revoke`（本人或管理员）、`GET /v1/tokens`；`Host.member` 接受 `Bearer pat_…`。会话：宿主内存按凭证哈希记 sign-in/token、UA 摘要、首末时间，`GET /v1/sessions`，`POST /v1/sessions/end-others` 让宿主此后拒绝其它凭证；停用/离职/撤销令牌在接受结果提交后结束正式会话；令牌会话绑定其令牌 ID，撤销不留下会话条目。OIDC delivery 必须配置独立、持久化的 `PLATFORM_PERSONAL_TOKEN_KEY`（至少 32 字节），不使用开发默认密钥；lightweight 使用已有私有签名密钥。撤销表与活动时间仍为进程内存，重启不保留已结束的登录会话；同一凭证在两浏览器不是两条会话。My account 新增「Personal tokens」「Sessions」两栏 | S |
| **E 减法** | 删 `Console.members` 内存 map 与 `subjects` 表、`SchemaLanguage`、`MemberView`、`Identities()` 开发令牌清单改从实体派生；`directory.json` 的 seats 变成首次导入。**已结 2026-10-06**：`platform.member.language` 不再进入线上目录，新提交被拒；历史 schema 仅在账本恢复注册，保留原提交/成员语言字节，并投影为账户偏好，后续档案编辑可显式回到租户默认。`directory.json` 已删除；`Console.members`/`subjects` 就是 Console 的已决状态（随账本快照/回放），`MemberView` 是生成到 `host.ts` 的应答类型，`Identities()` 已从该状态派生——三者是模型本身而非残留，保留；`Profile.LastSeen`、令牌 `LastUsed` 与会话改为宿主内存，不再污染已决状态 | S |

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
