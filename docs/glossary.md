---
level: 术语
covers: []
read-when: 给资源、界面元素、代码标识命名，或翻译界面文案
---

# 术语表

一词一行。界面文案、文档与代码标识只用这里的词；新词先加到这里。工具应用名是专有名词，中英文相同。

## 工具应用（不翻译）

Home、Search、Files、Applications、Notifications、Data Connection、Pipeline Builder、Dataset Preview、Ontology Manager、Logic、Automate、Workshop、Object Explorer、Agent Studio、AIP Assist、Control Panel、DevOps、Marketplace、Data Lineage、Monitoring、Host Console。

## 资源与概念

| English | 中文 | 定义 | 归属 |
|---|---|---|---|
| Resource | 资源 | 有 RID、属于一个项目的定义 | 6-domain §6.3 |
| RID | RID | 资源的不可变标识 `ri.<service>.<tenant>.<kind>.<id>` | 3-information §3.5 |
| API name | API 名 | 可被引用的稳定名 | 3-information §3.5 |
| Project | 项目 | 资源的容器与访问边界 | Files |
| Folder | 文件夹 | 项目内的组织层级 | Files |
| Managed project | 受管项目 | 安装产品后放置其资源的只读项目 | Marketplace |
| Source | 源 | 外部系统的连接 | Data Connection |
| Sync | 同步 | 从源写入数据集的计划任务 | Data Connection |
| Stream | 流 | 持续写入的数据来源 | Data Connection |
| Export | 导出 | 写回外部系统 | Data Connection |
| Listener | 监听器 | 入站 webhook | Data Connection |
| Webhook | Webhook | 出站 HTTP 调用定义 | Data Connection |
| Dataset | 数据集 | 带模式与版本的表 | Dataset Preview |
| Transaction | 事务 | 数据集的一次写入，形成新版本 | Dataset Preview |
| Media set | 媒体集 | 非结构化文件集合 | Dataset Preview |
| Pipeline | 管道 | 数据集之间的确定性变换图 | Pipeline Builder |
| Build | 构建 | 管道的一次执行 | Pipeline Builder |
| Schedule | 调度 | 构建的时间计划 | Pipeline Builder |
| Ontology | 本体 | 对象类型及其关系与动作的总和 | Ontology Manager |
| Object type | 对象类型 | 一类业务对象的定义 | Ontology Manager |
| Object | 对象 | 对象类型的实例 | 6-domain §6.5 |
| Property | 属性 | 对象类型的字段 | Ontology Manager |
| Shared property | 共享属性 | 跨类型复用的属性 | Ontology Manager |
| Link type | 关系类型 | 两类对象之间的关系定义 | Ontology Manager |
| Interface | 接口 | 多个对象类型共有的形状 | Ontology Manager |
| Value type | 值类型 | 带约束的属性类型 | Ontology Manager |
| Object type group | 对象类型组 | 浏览用的分组 | Ontology Manager |
| Action type | 动作类型 | 一种受治理的对象变更的定义 | Ontology Manager |
| Action | 动作 | 动作类型的一次执行 | 6-domain §6.7 |
| Parameter | 参数 | 动作或函数的输入 | Ontology Manager、Logic |
| Rule | 规则 | 动作产生编辑的步骤 | Ontology Manager |
| Submission criteria | 提交条件 | 动作执行前必须满足的条件 | Ontology Manager |
| Side effect | 副作用 | 动作接受后的通知或 webhook | Ontology Manager |
| Edit | 编辑 | 动作产生的按属性的修改事实 | 6-domain §6.5 |
| Datasource | 数据源 | 支撑对象类型的数据集或管道输出 | Ontology Manager |
| Function | 函数 | 无副作用的计算 | Logic |
| Decision table | 决策表 | 表格形式的函数 | Logic |
| Logic flow | 逻辑流 | 节点图形式的函数 | Logic |
| Automation | 自动化 | 条件 → 效果 | Automate |
| Module | 模块 | Workshop 中的多页面应用 | Workshop |
| Object view | 对象视图 | 对象的唯一落点页面 | Workshop |
| Widget | 组件 | 模块页面上的元素 | Workshop |
| Variable | 变量 | 模块中的值或对象集 | Workshop |
| Exploration | 探索 | 保存的对象查询 | Object Explorer |
| Object set | 对象集 | ObjectSet 读取代数的结果 | 6-domain §6.12 |
| Expression | 表达式 | Expr 语言的谓词或计算 | 6-domain §6.12 |
| Agent | 智能体 | 可调用工具的模型配置 | Agent Studio |
| Retrieval source | 检索源 | 智能体可检索的文档与对象 | Agent Studio |
| Evaluation set | 评估集 | 智能体或函数的评测样例 | Agent Studio |
| Work item | 工作项 | 指派给成员或团队的任务或审批 | Notifications |
| Approval | 审批 | 带批准与驳回动作的工作项 | Notifications |
| Notification | 通知 | 投递给成员的消息 | Notifications |
| Branch | 分支 | main 之外的定义空间 | 6-domain §6.20 |
| Proposal | 提案 | 请求把分支合并到 main | 6-domain §6.20 |
| Working state | 工作状态 | 个人未保存到分支的编辑 | 4-interaction §4.5 |
| Product | 产品 | 可交付的资源集合 | DevOps |
| Candidate | 候选 | 产品的待审阅快照 | DevOps |
| Product version | 产品版本 | 不可变的已发布候选 | DevOps |
| Test plan | 测试计划 | 发布前运行的业务测试 | DevOps |
| Installation | 安装 | 产品版本在租户中的实例 | Marketplace |
| Extension | 扩展 | 客户在受管资源之上的资源 | 6-domain §6.22 |
| Lineage | 血缘 | 资源之间的数据来源关系 | Data Lineage |
| Run | 运行 | 同步、构建、自动化、函数、投递的一次执行记录 | Monitoring |
| Member | 成员 | 租户中的主体 | Control Panel |
| Team | 团队 | 成员集合 | Control Panel |
| Organization unit | 组织单元 | 组织树的节点 | Control Panel |
| Business role | 业务角色 | 授权动作与字段的角色 | Control Panel |
| Project role | 项目角色 | Owner、Editor、Viewer、Discoverer | Files |
| Marking | 标记 | 强制访问控制 | Control Panel |
| Clearance | 许可 | 获准某个标记 | Control Panel |
| Row policy | 行策略 | 对象级可见性规则 | Ontology Manager |
| Credential | 凭据 | 部署注入或托管的秘密；应用只存引用 | Control Panel |
| Tenant | 租户 | 一个客户的隔离空间 | Host Console |
| Template | 模板 | 生成模块或动作类型的预置起点 | Workshop、Ontology Manager |
| Object list | 对象列表 | 保存的一组具体对象 | Object Explorer |
| Business concept | 业务概念 | 项目中对象类型与动作的业务视图 | Files |
| Connector | 连接器 | 源的连接类型（数据库、REST、文件、消息） | Data Connection |
| Activate | 激活 | 在产品所在租户中把某版本设为生效 | DevOps |
| Capability matrix | 能力矩阵 | 角色到动作与字段的授权总览 | Control Panel |
| Independent builder | 独立构建者 | 直接被授予构建者角色、而非经项目角色获得的成员 | 6-domain/security |
| Support access | 支持授权 | 成员开启的限时支持访问 | Control Panel |
| Accepted result | 接受结果 | 权威写入的唯一形式 | 7-system §7.3 |

## 角色名

| English | 中文 |
|---|---|
| Owner | 所有者 |
| Editor | 编辑者 |
| Viewer | 查看者 |
| Discoverer | 发现者 |
| Builder | 构建者 |
| Integrator | 集成者 |
| Publisher | 发布者 |
| Administrator | 管理员 |
| Auditor | 审计员 |
| Business user | 业务用户 |
| Host operator | 宿主运维者 |

## 标记名

| English | 中文 |
|---|---|
| Internal | 内部 |
| Confidential | 机密 |
| Restricted | 受限 |
