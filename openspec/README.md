# 模块规范

这里使用 **OpenSpec** 管理现行规范（specs）及其变更。按系统层级和完整职责划模块，模块内的相关能力共用一份规范；不为每个按钮、接口、组件或施工任务新建 spec。

先从 [Architecture](../docs/Architecture.md) 理解结构，再按下表只读受影响模块及其依赖。产品目标见 [Intent](../docs/Intent.md)，交付顺序唯一归 [WorkQueue](../docs/WorkQueue.md)，操作步骤归 [部署指南](../deploy/local/README.md)。

## 层级与模块

| 层 | 模块规范 | 主责与边界 |
|---|---|---|
| 内核 | [契约与语言边缘](specs/kernel/contracts/spec.md) | 内核中立性、语义来源、向量与证明范围；不复制 K1–K9 正文 |
| 运行基础 | [宿主与接受结果](specs/runtime/host/spec.md) | 权威、私有决定、持久接受、提交协调与租户故障 |
| 运行基础 | [存储、历史与恢复](specs/runtime/storage/spec.md) | 正式 SQL 状态、按需读取、历史与格式、检查点及兼容恢复 |
| 运行基础 | [身份、租户与授权](specs/runtime/access-control/spec.md) | 成员与组织身份、当前权限、隔离、来源和敏感信息 |
| 运行基础 | [组合、部署与运维](specs/runtime/deployment/spec.md) | 部署形态、依赖、受控执行接线、版本交付和恢复操作边界 |
| 平台能力 | [业务与企业模型](specs/platform/business-models/spec.md) | 对象、规则、事务、约束、关系、业务时间、组织与 UAF |
| 平台能力 | [应用与资产生命周期](specs/platform/application-lifecycle/spec.md) | 草稿、候选、依赖、包、发布、受控代码与替代退出 |
| 平台能力 | [流程与持久工作](specs/platform/automation/spec.md) | Flow、Work、审批、等待、世代、取消与重试 |
| 平台能力 | [数据接入与外部协同](specs/platform/integrations/spec.md) | Source、Dataset、Pipeline、Effect、确认、观测与设备协议 |
| 平台能力 | [AI 与知识](specs/platform/ai-knowledge/spec.md) | 函数、智能体、预算、评估、知识来源与人工采纳 |
| 平台能力 | [自举与系统运营](specs/platform/self-hosting/spec.md) | 动态系统范围、静态控制面边界、既有全量自举的冻结与历史保留 |
| 产品体验 | [工作区与构建编辑](specs/experience/workspace/spec.md) | 单一工作区、应用入口、共享 UI、草稿会话和专业编辑器 |
| 产品体验 | [专业可视化](specs/experience/professional-visualization/spec.md) | 地图、三维、图像标注的坐标、身份、资源与业务交互 |

全量源码自举已冻结：既有系统源码、发布和激活保留在 `system` 作用域，新动态自举不再开放，边界由 [自举与系统运营](specs/platform/self-hosting/spec.md) 与 [组合、部署与运维](specs/runtime/deployment/spec.md) 规定。

层是导航与归属分类，不是新的服务边界，也不要求每层独立部署。模块依赖允许跨层引用；代码依赖仍按 [Architecture](../docs/Architecture.md) 与现有边界检查执行。一次变化可以涉及多个既有模块，不为容纳一项变化另建模块。

## 文档各管什么

| 位置 | 内容 | 不承担的职责 |
|---|---|---|
| `docs/Intent.md` | 产品为什么存在、为谁服务、成功标准 | 代码清单和执行计划 |
| `docs/Architecture.md` | 当前结构、数据流、实现归属与代码地图 | 逐条复制模块需求 |
| `contract/spec/`、`contract/vectors/` | K1–K9 精确语义、错误与跨语言一致性 | 产品任务和行业规则 |
| `openspec/specs/<层>/<模块>/spec.md` | 现行行为、不变量、兼容与拒绝条件、代表性场景 | 未实施目标、实现步骤和运行证据台账 |
| `openspec/changes/<change>/` | 本次变化的理由、规范差量、必要设计和局部任务 | 项目优先级与第二份 WorkQueue |
| `docs/WorkQueue.md` | 当前交付范围与顺序 | 完成历史和模块规范 |
| 模块源码与就地 README | 实现细节、生成来源和操作步骤 | 再定义一套公共规则 |

规范描述应遵守的约定，代码和现有检查用于核对这些约定。发现二者冲突时明确指出并修正，不能把缺陷改写成规范来消除差异。规范中的场景是接受条件，**不是已经运行的测试结果**；自动检查、浏览器观察、负责人体验、客户交付与生产容量分别报告。

## 模块组织规则

1. **按职责聚合。** 增量优先进入现有模块。只有出现独立消费者、稳定数据/行为边界和独立演进理由，才拆分或新增模块；文件行数不是理由。
2. **只定义一次。** 例如持久接受归宿主、历史格式归存储、权限归授权；其他模块链接它们，只补自己的特殊语义。不能每个模块各写一套恢复、权限或发布规则。
3. **要求可辨别。** 每项 `Requirement` 使用 `SHALL` / `MUST`，写清触发、结果与必要的拒绝条件；至少一个 `Scenario`。实现位置单独列作来源，不把类名、框架选择或操作清单变成不变量。
4. **保留真实边界。** 历史读取、在途版本与公开兼容要写在对应模块；未知能力和未证实性能不能写成当前保证。小功能在模块中增加需求或场景，不新建目录。

## 后续怎样使用

本仓库已通过官方 `openspec init --tools codex --language zh-CN --profile core` 初始化，六个 Codex Skill 位于 `.agents/skills/openspec-*/SKILL.md`，由 CLI 生成，不手改。CLI 版本固定在根 Makefile；无需全局安装：

```bash
make openspec ARGS='list --specs'
make openspec ARGS='show runtime/storage --type spec'
make openspec ARGS='list --json'
make check-specs
```

- **查清问题**：使用 `$openspec-explore`，按相关规范与代码讨论。
- **修改约定**：行为、公共契约、存储格式、权限或发布边界变化时，使用 `$openspec-propose` 创建一项完整变化。`proposal.md` 写为什么及范围，`specs/` 写差量；跨模块或有迁移风险时在 `design.md` 写取舍，`tasks.md` 只拆本次工作。已有明确实施授权时继续落实该范围，不重复请求同一授权；用户仅要求提案时停在提案。
- **落实与调整**：使用 `$openspec-apply-change`；发现设计变化用 `$openspec-update-change` 修正受影响材料，保留仍成立的场景。修复回归、纯搬迁、措辞和实现定位更新不改变约定时，直接修改，不为凑流程发明需求。
- **完成后同步**：用 `$openspec-sync-specs` 将已经落实的差量合入现行模块，再用 `$openspec-archive-change` 归档已完成变化。规范保持现行状态，未完成项不靠勾选或同步变成完成；完成变化从活动目录退出。

用 CLI 的 `new change` 建立变更目录与元数据；不要手造元数据、维护独立 OpenSpace 目录或第二个规范存储库。一次完整交付可同时改多个模块 spec；根 WorkQueue 决定先做哪一块。升级工具时显式调整 Makefile 版本并运行 `make openspec ARGS='update'` 更新已配置的官方 Skill。

## 基线与取材

本次规范核对的代码基线是 `e3808100375cf3fb4d7d5ec6f6ea025593179171`。已删除的设计文档从 Git 提炼，来源树为 `92a7a9d178d2df9d438b1dbc4f6bfbc4151a94f9`，不恢复旧编号制度、过时施工限制或完成日记。每个模块的 Purpose 标明实际源码定位；后续随实现演进维护，不要求重复审计全仓。

组织方式采用 [OpenSpec 的领域规范与变更差量](https://github.com/Fission-AI/OpenSpec/blob/main/docs/concepts.md)，结合 [arc42 对大型系统的模块化建议](https://faq.arc42.org/questions/J-1/)：总体文档保存公共结构，子系统保存自己的约定并互相链接；按 [Diátaxis](https://diataxis.fr/) 区分规范参考、结构解释与操作指南。它们提供组织方法，不定义 Platform 的业务语义。
