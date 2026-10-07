# ADR-0078 — 租户与授权：租户即边界、权限即一等概念、一个授权引擎（XXL）

状态：接受，待实施 · 2026-10-06 · 承接 ADR-0012（组织与单位）、ADR-0025 D4、ADR-0028 D3（字段读写角色）、ADR-0037 18b（角色与范围）、ADR-0066（范围设计器）、ADR-0067/0068/0073（企业模型）、ADR-0075（Markings）、ADR-0069 第二程

## 0. 为什么现在

平台已经有租户、角色、范围、字段权限、标记，但它们是**六七个各自为政的小机制**：`Member.Roles` 一个应用只能有一个角色字符串；`Catalog.Permits(role, schema)` 只看角色；行范围靠 `Scope.Levels`、`Participants`、`Through` 三条不同路径；Markings 另有一套判断；租户只在 `directory.json` 里以静态座席存在，没有"新建租户""租户设置"；没有自定义角色，没有策略，没有"为什么能/不能"。对一个 SaaS 平台这是地基缺角。本 ADR 把它们收敛成**一个模型、一个引擎、一处管理面**，并补上租户本身的生命周期与配置。

轻量级原则不变：不引入图数据库、策略引擎（OPA/Cedar）或外部 IAM；关系与属性都已在内存的记录存储、目录与企业模型里，授权引擎是对它们的一次统一求值，几百行 Go。

## 1. 四个边界，各管各的

| 概念 | 是什么 | 决定 |
|---|---|---|
| **Tenant** | 不可穿透的信任与数据边界：独立账本、记录、密钥、审计、配额 | 跨租户零共享；唯一的跨界读是 host console 的授权支持会话（已有）。租户之间不建"父子租户" |
| **Enterprise Model** | 租户内的现实组织：法人、工厂、部门、岗位、人（UAF，ADR-0067） | 子公司、工厂、事业部默认是**一个租户内的企业模型元素**，授权范围沿它走（`unit`/`below`/结构） |
| **Deployment** | 运行位置：哪台宿主、哪个区域、哪套 IdP | 一个 deployment 承载多个租户；租户的区域/驻留是 `platform.tenant` 上的一个声明，由宿主在创建时核对 |
| **Application** | 租户里运行的能力包（core、build、mes…） | 权限的命名空间；角色归应用，策略归租户 |

**何时另开租户**：只由隔离、数据主权、独立运维（独立备份/升级节奏/独立 IdP）决定，不由组织层级决定。文档里给出这条判断规则，host console 创建租户时让人勾选理由。

## 2. 租户的生命周期与配置

### 2.1 `platform.tenant`：一条记录说清一个租户

Console（`platform` 应用）新增只有一条的实体 `platform.tenant`（ID 即租户 ID）：

- 身份：`title`、`legalName`、`slug`、`region`（驻留声明）、`industry`（行业包名）
- 本地化默认：`language`、`languages[]`（启用的界面语言）、`timezone`、`currency`、`numberFormat`、`dateFormat`、`weekStart`、`fiscalYearStart`（供 `core.period` 预置）
- 接入：`signInDomains[]`（允许自动加入的邮箱域）、`idp`（宿主登记的 IdP 名）、`sessionHours`、`mfaRequired`（向 IdP 声明的要求）
- 治理：`retentionDays`（审计/效果/通知保留）、`defaultMarking`、`exportPolicy`（谁能导出 CSV：admin/role/anyone）、`agentPolicy`（智能体是否可自主运行）
- 配额（只读，宿主写）：`seats`、`storageMB`、`modelBudgetUSD`
- 状态（只读，宿主写）：`lifecycle` open/suspended/decommissioned、`created`、`plan`

动作 `platform.tenant.edit`（租户 admin，不能改配额/状态/区域），宿主的 lifecycle 动作沿 `/v1/host/tenants/{t}/lifecycle` 不变。各应用现有 `Settings` 保持，但**语言、时区、币种三项从 `platform.tenant` 读**（`t.setting("platform/…")` 已有这条路，统一到记录上），删掉散落的 `SettingLanguage` 等。

### 2.2 创建与预置：从模板出发

宿主新增 `POST /v1/host/tenants`（宿主管理员）：`{id, title, template, region, firstAdmin: {subject, email}}`。**模板** = `deploy/templates/<name>.json`：应用清单、企业模型种子（ADR-0068 slice）、语言/时区/币种、行业包（ADR-0046 的 package index 条目）、初始角色。宿主据此组合应用（与今天 `NewTenant(id, NewConsole(id, seats...), apps...)` 同一条路，只是来源是模板而不是代码）、写入 `platform.tenant`、添加首位管理员、落到 `tenants.json`（取代 `directory.json` 作为静态座席的唯一来源；现有 `directory.json` 作为首次导入保留一个版本后删除）。重启按 `tenants.json` 重建；快照/回放不变。

`DELETE` 不做；退役沿 lifecycle `decommission`，数据保留到 `retentionDays` 后由宿主运维脚本清理（ADR-0039 边界）。

### 2.3 租户管理面

Settings 里新增 **Organisation** 页（租户 admin）：上面所有可编辑字段 + 配额/状态只读 + "本租户的应用"（已有 apps 页并入）。Host console 新增 **New tenant** 表单与模板选择。

## 3. 权限模型

### 3.1 Permission 是一等概念，但**不手写清单**

```
permission := <app>.<entity>.<verb>          // 动作，等于今天的 action schema
            | <app>.<entity>.read            // 读类型
            | <app>.<entity>.<field>.read|write
            | <app>.<entity>.export
            | <app>:read:<name>              // 应用具名读取（原 read:<name>）
            | <app>:setting:<name>           // 改应用设置
            | platform.*                     // 租户管理：members、roles、policies、tenant、audit、support
```

**权限目录由 manifest 派生**（`platform.Permissions(manifest)`），每个应用的实体、字段、读取、设置自动展开；构建器发布的对象同样派生。目录只读，是角色编辑器与解释器的词表。

### 3.2 Role = 命名的权限集；角色归应用，也可以归租户

- **内置角色**：应用 manifest 今天声明的 `Roles` 不变（builder、accountant、operator…），其权限集 = 该 manifest 里把此角色写进 `Roles`/`Scope.Levels`/字段 `read`/`write` 的全部条目——这就是今天的行为，只是现在可以被**列出来**。
- **自定义角色**：`platform.role`（租户 admin）：`{name, title, app, includes[]（内置或自定义角色）, grants[]（权限模式，支持通配 core.journal.*）, denies[]（永远不给，覆盖 includes）, scope（该角色默认的行范围 all/below/unit/own）, fields{read[], write[]}}`。编译后就是一张权限集，与内置角色同等对待。
- **一人多角色**：`Member.Roles map[app]string` → `Member.Grants []Grant{App, Role, Unit?, Structure?, From?, Until?, By, Reason}`。`Roles` 保留为**派生只读视图**（`map[app][]string`）。**Catalog.Permits(roles[])**、`Scope` 取多个角色里最宽的范围、字段读写取并集。
- **带范围与时效的授予**：`Grant.Unit` 把一个角色限制在某个单位（及其以下）；`From/Until` 让授予自然到期（代班、审计员进场）；**委托**：成员把自己的某个授予委托给另一人一段时间（`platform.member.delegate`，可被 admin 禁止），委托出的授予带 `By`，到期自动收回，审计里看得见。

### 3.3 三种判断，一个顺序

授权引擎回答 `Decide(member, permission, resource, ctx) → Verdict{Allow, Reason, Rule}`，按固定顺序：

1. **租户与状态**：成员属于该租户、未停用、租户未 suspended（宿主支持会话例外，已有）。
2. **RBAC — 能不能做**：成员的有效角色（含到期过滤）里是否有人授予该 permission（自定义角色先展开 includes/denies；应用 capability 关闭则否）。
3. **ReBAC — 对哪些资源**：资源落不落在该授予的范围里。范围谓词统一为：`all` · `below(structure)` · `unit` · `own(field)` · `participant` · `through(field)` · `related(link, hops=1)`。前五个今天已有（Scope.Levels / Participants / Through），`related` 是新加的：沿 relations 应用的链接一跳（"我负责的客户的工单"），**用链接索引的邻接查找，不做图遍历**。企业模型的单位、岗位、结构是 `unit/below` 的来源（ADR-0073 同步进来的也算）。
4. **ABAC — 在什么条件下**：`platform.policy`（租户 admin）：`{name, effect: deny|allow, permissions[] 模式, roles[]（空=所有）, resource 类型, when: 表达式}`，表达式沿 ADR-0064 公式语言，变量有 `record.*`、`member.*`（单位、岗位、属性）、`ctx.*`（now、marking、channel: ui/api/agent/automation、ip 段）。**deny 优先**。Markings（ADR-0075）改写成两条内置策略（机密需指明读者、受限不出 CSV），不再是单独的 if。
5. **解释**：Verdict 带 `Rule`（"role accountant via grant #12 (until 2026-12-31) · scope unit plant-sz · policy no-export-restricted"），所有拒绝都有一条人能读的理由；成功的裁决在 `?explain=1` 时也返回。

一个引擎，所有入口共用：`Tenant.Submit`、`Tenant.Read`、记录查询与字段窄化（narrow.go）、导出、页面可见性（navigation）、智能体工具选择、A2A、自动化（`Automation` 调用者 = 应用自己的 manifest 授予，不变）。替代今天 `Catalog.Permits`、`readableIn`、`mayRead`、`readsField`、`inScope`、`restricted()` 各处各判一次。

### 3.4 组与团队

不新建第二套组织：**组 = 企业模型的单位或岗位**（给岗位授角色，坐上岗位的人自动获得，离岗自动失去，沿 `From/Until`）。临时团队用 `platform.team`（成员列表，可作为 Grant 的对象），编译时展开成成员授予，不进引擎。

### 3.5 服务账号与智能体

`client:<id>`（服务账号）与 `agent:<app>.<name>` 都是成员（已有），走同一引擎；区别只在 `ctx.channel` 与策略（如"api 渠道不许导出受限数据""agent 自主运行只能读"）。

## 4. 管理面与 API

- **Settings → Members**（重做）：成员的授予列表（角色 · 范围单位 · 有效期 · 来源），加/撤/委托；"以此人视角"的有效权限矩阵（按应用 × 实体 × 读/建/改/归档/导出，字段级展开）。
- **Settings → Roles**：内置角色只读展开 + 自定义角色编辑器（勾选权限目录、含/排角色、默认范围、字段）。
- **Settings → Policies**：策略列表与编辑（表达式编辑器复用 ADR-0064 的）。
- **Settings → Organisation**：§2.3。
- **解释器**：`GET /v1/authz/explain?member=&permission=&resource=`（admin 任何人、成员自己）；成员页与记录页的 "Why can't I…" 入口。
- **审计**：授予/撤销/委托/角色/策略变更已是账本决策，审计页加 "权限变更" 过滤。
- 构建器：对象的 `access[]`（ADR-0066）保持为**对象级的角色权限声明**，发布时派生到权限目录；项目 Roles 矩阵读同一目录。

## 5. 分块与顺序（XXL）

| 块 | 内容 | 规模 |
|---|---|---|
| **A 租户** | `platform.tenant` 记录与 Organisation 页；模板与 `POST /v1/host/tenants`；`tenants.json` 取代 `directory.json`；语言/时区/币种统一来源 | L |
| **B 权限目录与多授予** | `platform.Permissions(manifest)`；`Member.Grants` + 派生 `Roles`；`Permits(roles[])`、Scope/字段取并集；授予的单位/时效；Members 页重做 | XL |
| **C 引擎** | `platform/authz`：Decide/Verdict/explain；把 Submit/Read/narrow/export/navigation/agents 全部切到引擎；`related` 谓词；Markings 改写为内置策略 | XL |
| **D 自定义角色、策略、委托、团队** | `platform.role`、`platform.policy`、`platform.team`、`member.delegate`；Roles/Policies 页；表达式变量 | L |
| **E 减法与迁移** | 删 `c.Role()` 单角色调用点、`SettingLanguage`、`directory.json`、Markings 的专用 if；两行业应用的角色声明迁到目录；Testing/Platform §10.6 行 | M |

顺序 A → B → （ADR-0079 的 A/B 可并行）→ C → D → E。B 之前 ADR-0079 的 Profile 已能用（它只依赖成员 ID）。

## 6. 退出判据

- host console 用模板新建一个租户，首位管理员登录，Organisation 页改时区/币种后 `core.period`、金额输入、日期显示随之变化；
- 一名成员在 mes 同时持 `operator`（单位 L1）与 `inspector`（单位 plant-sz，至 2026-12-31），记录列表与动作按钮与之一致；到期后自动消失；委托给同事一周，审计可见；
- 自定义角色 `shift-lead` = operator + `mes.order.close`，策略 "受限标记的记录 api 渠道不许导出" 生效，`explain` 对一次拒绝给出完整链条；
- 全部入口（UI、API、智能体、导出、自动化）只经过 `authz.Decide`；`grep -c "c.Role()"` 为 0。

## 7. 不做

- 不做跨租户共享对象/联合查询、不做租户层级；
- 不做运行时策略语言以外的脚本钩子；不引入 OPA/Cedar/Zanzibar 服务；
- 不做细到单元格的标记（ADR-0075 不变）；
- 不做自建 IdP/密码库：身份仍来自 OIDC/开发令牌（ADR-0079 §1）。
