# 术语表

一词一行。界面文案、文档与代码标识只用这里的词；新词先加到这里。

## 应用

应用按它承担的工作命名，中英文各一个名字，随界面语言显示。

| English | 中文 | 承担的工作 |
|---|---|---|
| Home | 主页 | 个人工作台与资源的位置 |
| Data | 数据 | 接入外部系统，加工成数据集，追溯来源 |
| Ontology | 本体 | 定义业务对象并浏览它们 |
| Logic | 逻辑 | 写计算与自动反应 |
| Apps | 应用 | 搭建业务界面 |
| AI | AI | 构建与评估智能体 |
| Delivery | 交付 | 打包、发布、安装与升级 |
| Operations | 运行 | 看系统在做什么 |
| Admin | 管理 | 管人、权限与工作区设置 |
| Host | 宿主 | 运维宿主 |
| Search | 搜索 | 外壳：统一搜索与命令 |
| Notifications | 通知 | 外壳：通知、工作项、审批请求 |
| Assist | 助手 | 外壳：作用于当前页面的助手 |

## 资源与概念

“归属”一栏是拥有它的应用，或讲它的那份文档。

| English | 中文 | 定义 | 归属 |
|---|---|---|---|
| Resource | 资源 | 有 RID、属于一个项目的定义 | domain |
| RID | RID | 资源的不可变标识 `ri.<service>.<tenant>.<kind>.<id>` | domain |
| API name | API 名 | 可被引用的稳定名 | domain |
| Record type | 记录类型 | 声明记录的字段、关系、生命周期与动作，在 Go SDK 中称为 `RecordType` | 本体 |
| Project | 项目 | 资源的容器与访问边界 | 主页 |
| Folder | 文件夹 | 项目内的组织层级 | 主页 |
| Managed project | 受管项目 | 安装产品后放置其资源的只读项目；名字即包里类型的前缀 | 交付 |
| Connection | 连接 | 外部系统的访问定义（地址与凭据引用） | 数据 |
| Data source | 数据源 | 从连接按计划或请求拉取行到对象 | 数据 |
| Listener | 监听器 | 接收外部系统入站调用的入口 | 数据 |
| Writeback | 回写 | 按已声明规则把结果发回外部系统 | 数据 |
| Observation source | 观测源 | 按时间窗读取外部信号并形成业务观测 | 数据 |
| Matching rule | 匹配规则 | 判定不同来源的行是否指同一对象并选择取值 | 数据 |
| Sync | 同步 | 数据源列表的界面分组名 | 数据 |
| Stream | 流 | 流式数据源与观测源列表的界面分组名 | 数据 |
| Export | 导出 | 回写列表的界面分组名 | 数据 |
| Webhook | Webhook | 出站投递监控的界面分组名 | 数据 |
| Dataset | 数据集 | 带模式与版本的表 | 数据 |
| Transaction | 事务 | 数据集的一次写入，形成新版本 | 数据 |
| Media set | 媒体集 | 非结构化文件集合 | 数据 |
| Pipeline | 管道 | 数据集之间的确定性变换图 | 数据 |
| Build | 构建 | 管道的一次执行 | 数据 |
| Schedule | 调度 | 构建的时间计划 | 数据 |
| Ontology | 本体 | 对象类型及其关系与动作的总和 | 本体 |
| Object type | 对象类型 | 一类业务对象的定义 | 本体 |
| Object | 对象 | 对象类型的实例 | domain |
| Property | 属性 | 对象类型的字段 | 本体 |
| Shared property | 共享属性 | 跨类型复用的属性 | 本体 |
| Link type | 关系类型 | 两类对象之间的关系定义 | 本体 |
| Interface | 接口 | 多个对象类型共有的形状 | 本体 |
| Value type | 值类型 | 带约束的属性类型 | 本体 |
| Object type group | 对象类型组 | 浏览用的分组 | 本体 |
| Action type | 动作类型 | 一种受治理的对象变更的定义 | 本体 |
| Action | 动作 | 动作类型的一次执行 | domain |
| Import | 导入 | 把文件的行逐行交给对象类型的新建或编辑动作，先试算再写入 | domain |
| Parameter | 参数 | 动作或函数的输入 | 本体、逻辑 |
| Rule | 规则 | 动作产生编辑的步骤 | 本体 |
| Submission criteria | 提交条件 | 动作执行前必须满足的条件 | 本体 |
| Side effect | 副作用 | 动作接受后的通知或 webhook | 本体 |
| Edit | 编辑 | 动作产生的按属性的修改事实 | domain |
| Datasource | 数据源 | 支撑对象类型的数据集或管道输出 | 本体 |
| Function | 函数 | 无副作用的计算 | 逻辑 |
| Decision table | 决策表 | 表格形式的函数 | 逻辑 |
| Logic flow | 逻辑流 | 节点图形式的函数 | 逻辑 |
| Automation | 自动化 | 条件 → 效果 | 逻辑 |
| Module | 模块 | Workshop 中的多页面应用 | 应用 |
| Object view | 对象视图 | 为某个对象类型专门搭建的页面，替代默认的记录页 | 应用 |
| Widget | 组件 | 模块页面上的元素 | 应用 |
| Variable | 变量 | 模块中的值或对象集 | 应用 |
| Exploration | 探索 | 保存的对象查询 | 本体 |
| Object set | 对象集 | ObjectSet 读取代数的结果 | domain |
| Expression | 表达式 | Expr 语言的谓词或计算 | domain |
| Agent | 智能体 | 可调用工具的模型配置 | AI |
| Retrieval source | 检索源 | 智能体可检索的文档与对象 | AI |
| Evaluation set | 评估集 | 智能体或函数的评测样例 | AI |
| Work item | 工作项 | 指派给成员或团队的任务或审批 | 通知 |
| Approval | 审批 | 带批准与驳回动作的工作项 | 通知 |
| Notification | 通知 | 投递给成员的消息 | 通知 |
| Branch | 分支 | main 之外的定义空间 | domain |
| Proposal | 提案 | 请求把分支合并到 main | domain |
| Working state | 工作状态 | 构建者个人尚未保存的编辑，别人看不到 | domain |
| Product | 产品 | 可交付的资源集合 | 交付 |
| Candidate | 候选 | 产品的待审阅快照；不指内核中未提交的决定 | 交付 |
| Product version | 产品版本 | 不可变的已发布候选 | 交付 |
| Test plan | 测试计划 | 发布前运行的业务测试 | 交付 |
| Installation | 安装 | 产品版本在租户中的实例 | 交付 |
| Extension | 扩展 | 客户在受管资源之上的资源 | domain |
| Lineage | 血缘 | 资源之间的数据来源关系 | 数据 |
| Preview marking | 预览标记 | 按当前保存的定义推导的生效标记，只用于展示 | domain |
| Protection | 保护标记 | 按执行使用的已接受定义与写出证据推导的标记，用于授权；内部称 Guard | domain |
| Impact | 影响 | 从一个资源出发可达的上游或下游资源与字段 | 数据 |
| Boundary | 边界 | 因深度、其他项目或无权而未展开的邻居计数 | 数据 |
| Run | 运行 | 同步、构建、自动化、函数、投递的一次执行记录 | 运行 |
| Member | 成员 | 租户中的主体 | 管理 |
| Team | 团队 | 成员集合 | 管理 |
| Organization unit | 组织单元 | 组织树的节点 | 管理 |
| Business role | 业务角色 | 授权动作与字段的角色 | 管理 |
| Project role | 项目角色 | Owner、Editor、Viewer、Discoverer | 主页 |
| Marking | 标记 | 强制访问控制 | 管理 |
| Clearance | 许可 | 获准某个标记 | 管理 |
| Row policy | 行策略 | 对象级可见性规则 | 本体 |
| Credential | 凭据 | 部署注入或托管的秘密；应用只存引用 | 管理 |
| Tenant | 租户 | 一个客户的隔离空间 | 宿主 |
| Template | 模板 | 生成模块或动作类型的预置起点 | 应用、本体 |
| Object list | 对象列表 | 保存的一组具体对象 | 本体 |
| Business concept | 业务概念 | 项目中对象类型与动作的业务视图 | 主页 |
| Connector | 连接器 | 连接的访问类型（数据库、REST、文件、消息）；也是能力实现的一种形态 | 数据 |
| Person | 人 | 组织里的一个人类；账号可以登录为某人 | 管理 |
| Facet | 附属对象 | 包挂在下层名词（人、地点、当事人）上的一组自己的字段，以那个名词为唯一键 | architecture |
| Base data package | 基础数据包 | 多个应用共用的名词与参考数据（当事人、区划、计量单位） | architecture |
| Capability contract | 能力契约 | 一项能力的带版本接口：动作、读取、事件、计量与一致性测试 | architecture |
| Implementation | 能力实现 | 实现某个能力契约的东西：声明式、连接器、沙箱代码或受管服务 | architecture |
| Binding | 绑定 | 租户选定由哪个实现提供某个能力契约 | 管理 |
| Managed service | 受管服务 | 宿主之外、经注册与探活接入的能力实现进程 | architecture |
| Activate | 激活 | 在产品所在租户中把某版本设为生效 | 交付 |
| Capability matrix | 能力矩阵 | 角色到动作与字段的授权总览 | 管理 |
| Independent builder | 独立构建者 | 直接被授予构建者角色、而非经项目角色获得的成员 | domain |
| Support access | 支持授权 | 成员开启的限时支持访问 | 管理 |
| Service | 服务 | 拥有一组资源类型或记录、对其写入作出决定的 Go 包 | backend |
| Submission | 提交 | 请求一次权威写入的协议消息 | architecture |
| Decision | 决定 | 服务对一次提交算出的待提交结果（编辑、意图或拒绝），带基准位置 | architecture |
| Accepted result | 接受结果 | 权威写入的唯一形式 | architecture |
| Format epoch | 格式纪元 | 宿主保证逐字节可读的存储格式代际；纪元之前的格式不读取 | architecture |
| Self-hosting | 自举 | 行业应用与内置应用都只用平台自己的工具构建，并经市场安装 | intent |
| System product | 系统产品包 | 随宿主预装、交付内置应用的产品 | plan |
| Lane | 车道 | 每个租户唯一的提交序列锁，可取消、按到达排队 | architecture |
| Read image | 读取映像 | 某个已应用位置上读取所需租户状态的不可变快照 | architecture |
| Type description | 类型描述 | 宿主给出的一个类型的形状：字段、字段类型、选项、显示字段、标准动作、状态 | frontend |
| View definition | 视图定义 | 一个类型的列表怎样呈现：列、筛选、排序、分组与布局 | frontend |
| View | 视图 | 命名并保存的一组列表设置 | ux |
| Contract | 契约 | 界面运行时的底层：宿主给出的形状与唯一的写入 | frontend |
| Presentation | 表现 | 由官方模板改造一次、只认契约的界面组件 | frontend |
| Entry | 入口 | 应用菜单里的一项；代码里是一句声明 | frontend |
| Page archetype | 页面原型 | 列表、记录、画布、仪表盘四种之一 | ux |
| List | 列表 | 一类记录的集合页 | ux |
| Record | 记录 | 一个资源或一个对象；也指它的页面 | ux |
| Board | 看板 | 列表的一种布局：按选项或状态字段分列 | ux |
| Lines | 明细行 | 记录里的表中表；行有自己的字段 | domain |
| Reference | 引用 | 指向另一类对象的字段 | domain |
| Attachment | 附件 | 挂在记录上的文件 | domain |
| Activity | 活动 | 记录页上的时间线：评论、字段变更与状态流转 | ux |
| State | 状态 | 类型声明的流程位置；转换只由动作执行 | domain |
| Draft | 草稿 | 已保存、尚未发布的定义 | domain |
| Publish | 发布 | 把已保存的定义冻结并激活，业务用户此后才用到 | domain |
| Product pack | 产品包 | 产品在仓库里的作者态源与生成的制品；口语里也指产品 | domain |
| Flagship pack | 旗舰包 | 第一个只用平台工具搭成、交给第一个客户的行业产品包 | plan |

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
