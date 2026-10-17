# 旧实现的替代与退出依赖

这里只保留现行替代关系和历史读取责任。当前施工顺序归 [WorkQueue](WorkQueue.md)，删除原则归 [ADR-0124](ADR/0124-platform-consolidation.md)，原生应用退出条件归 [ADR-0114](ADR/0114-retiring-native-apps.md)。已完成删除的逐文件命令和调查过程从本文移除，需要时查 Git 历史。

## 1. 已退出的 ERP 适配器

apps/erpadapter 与 web/packages/erpadapter 及其模块、席位、组合宿主引用已删除。原轮询由 build.connection/build.source 承担，原 confirmation 效果由 build.writeback 承担；连接配置、权限与稳定幂等身份沿原集成路径。新增退出仍须一并移除模块、导入、席位、工作区包、lockfile 和 Catalog 引用，生成物使用原工具重生成。

## 2. 原生参考应用

apps/crm、csm、erp、hcm、mes、pms 仍有有效业务与历史责任。ERP 采购链、MES 业务规则和 DemoMaster 等只在等价构建产物存在、同任务验证完成、依赖与历史读取责任明确后退出，判定条件见 ADR-0114。不把目录名或代码行数当作删除依据。

## 3. 数据接入与企业切片

数据源直接映射对象仍是 [ADR-0071](ADR/0071-datasets-and-pipelines.md) 保留的有效配置，界面仍可选择，写入沿原对象 create/edit。Dataset/Pipeline 完整替代前不能只迁移测试数据后删掉 Source.MapRows 与对象路径。enterprise.slice.import 与 slice.sync 仍分别承担导入与同步，不再保留按闲置时长自动删除的推测。

## 4. 构建器旧动作表达（ADR-0100，已替换）

新保存和供编辑的对象将 Sets、Creates、Posts、Journal、Reverses、Conditions、Reads、To/ToInput 转为 Action.Transaction 后移除原字段；Object.Numbering 转为 CreateTransaction 与命名序列。旧分散动作/编号编辑器已删除，状态图只读写事务。

旧 Published、日志与定义字节不改。旧发布当前执行在内存编译成同一事务；take/stored/create/post/numberingOf 仅供历史 raw-input 日志的 LegacyReplay，拒绝普通调用。已保存的接受结果只应用原映像。首次编号读取已有最大值，接受后由序列 owner 管理。原逐步骤授权、记录校验、前驱与出站效果归 [ADR-0100](ADR/0100-business-transaction.md)，读取义务未结束前不能删除输入适配。

## 5. 规则表达与存储约束

旧公式、表格条件、动作比较和页面算子图的独立求值实现已被同一 Expr 引擎替代；旧文本只作编译输入，冻结公式的历史数值语义及字节仍保留，见 [ADR-0101](ADR/0101-one-rule-language.md)。

新对象存储约束由原服务端统一检查，CSV、事务、AI 与协议不各自实现；字段输入仍归 Required/InputConstraints。CSV 空白表示清空，不静默省略，预览复用原私有决定，见 [ADR-0102](ADR/0102-business-constraints.md)。

## 6. 关系、版本与业务时间

同权威 ad-hoc link/unlink 新写已冻结，新关系只经声明的所有者事务。旧边按原权限读取，显式迁移成功后退出当前读取；历史 owner-state 保留，不双写、不改旧字节。实际租户迁移仍须执行；跨应用关系、动态、评论和关注仍归 relations，见 [ADR-0104](ADR/0104-record-sets-and-relations.md)。

新构建产物使用普通记录 Versioning、固定映射和有效期查询；不另造版本对象、最新版查找器或时间模型。原生行业数据的接管及历史单据责任沿 [ADR-0103](ADR/0103-business-version-and-time.md) 与 ADR-0114。

## 7. 已合并的实现

| 已删除的重复职责 | 当前归属与保留范围 |
|---|---|
| 运行列表嵌套第二份详情、取消与重试会话 | 原正式运行详情/useRunControl；列表只读投影，历史画布、输入输出和步骤定位保留 |
| 知识/术语及应用 New 菜单的第二套创建弹窗 | 原 Records/useNewRecord/DeclaredActionForm；专用资源进入原专用编辑器 |
| FunctionAdvice 与 PublishMenu 的独立调用/归档状态 | AIWidget/createAIReader、ResourceControls；原函数版本、ProposalReview、审批与未保存保护保留 |
| 集成目录、安装解码与发布身份的重复分派 | 原 readDefinitionInventoryOf、integrationInstallationRecord、draftAssetRef 与 AcceptedPublicationSchemas；分页、上限、身份、严格解码、历史族与权限保留 |
| 页面变量底栏和决策表的手写表格 | 原 DataTable；字段编辑、增删、重排和首个命中顺序保留 |
| 四类定义的版本家族维护 | apps/build/definition_history.go；各类型校验、保留字节与关系约束仍归原定义 |
| Section 的重复公共声明与逐字段复制 | 原 PageWidgetConfig 与生成 Api.Section；五类构建期引用单独转换，扁平 JSON/API 与原切片复制保留 |
| 定义编辑器各自管理保存/修订/丢弃，包括告警与决策表 | build/session/definition-draft.ts；共享已接受修订与应用成员重试，字段、载荷、发布诊断和关系迁移仍归原功能 |
| 三处直接记录捕获与重复任务快照编码 | accepted_rows.captureDirectRows 与 stateOfTask；锁、排序、前驱、历史内容及错误语义保留，callback 只读取指定记录 |
| 页面变量访问与作用域的重复清单 | Section.variablePorts/variableReferences 与原组件检查；嵌入、观测及权限例外保留 |
| ERP 六处创建表单、Dialog/Sheet 生命周期、管理页第二套宿主接口 | 原 useNewRecord/DeclaredActionForm、OverlayFrame、useHost；ID、输入、焦点、隐藏草稿和权限保留 |

无调用私有函数、未引用依赖和同值翻译已按真实调用删除；公开导出、动态词条与历史适配不因静态匹配少而删除。scripts/escapes.sh 当前只剩 MES/PMS 两个后端事实日志条目，不通过删清单伪造退出。

## 12. 代码规模与本轮可删除空间（ADR-0124）

使用 make code-size REVISION=<提交> 与 make code-size 按同一口径复算；Python 3、cloc 2.10，按 Git 源文件统计，分开生产、测试、生成物、构建/示例和文档/配置/数据。空白、注释、文件搬迁、行压缩与生成物删量不计手写生产删量。

| 已核对的比较 | 手写生产代码变化 |
|---|---:|
| 0c715c64 → bd1ed00a | 162,374 → 160,280，净减 2,094 行（1.29%） |
| c6b76631 的 9 个生产文件 | 2,924 → 2,790，净减 134 行；测试准备另减 34 行 |

以上是固定提交的结果，不代表当前全仓规模，也不是剩余可删量的上限。报告可从相同提交重算；15%–25% 是进取目标，不以删除有效功能或测试断言凑数。一次编译、自动测试或本地部署不等于全部恢复、容量或真实使用均已证明。
