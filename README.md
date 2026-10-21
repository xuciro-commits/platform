# Platform · 业务应用构建平台

**一句话：给 FDE 一套"给客户造业务系统"的底座。交付的是造系统的能力，不是某个行业的系统。**

第一次真实交付是楼宇供暖（[ADR-0060](docs/ADR/0060-first-real-delivery.md)）；MES 是第二个验证场景。CRM/ERP/PMS/HCM/CSM 是参考应用，按 [ADR-0053](docs/ADR/0053-retiring-native-apps.md) 在等价构建产物出现后退场。

---

## 一分钟认识仓库

后端只有三层，前端只有一个工作区。其余全部是这三层的组合。

| 层 | 位置 | 负责 |
|---|---|---|
| **内核契约** | `contract/` | 语言中立的 K1–K9：身份、事实、决策、权威归属、租户隔离、模式版本、连接器、工作所有权。不含任何业务词汇 |
| **宿主运行时** | `capabilities/server/`（Go） | 唯一一条写入路径、日志与重放、记录存储、授权、效果投递、模型调用 |
| **应用** | `capabilities/server/apps/*`（平台应用）<br>`apps/*`（行业应用，会退场） | 用内核能力拼出的业务语义；应用之间只通过 `protocols/` 相遇 |
| **前端** | `web/` | 一个工作区 + 若干 UI 包；每个应用用 `defineApp` 贡献视图和菜单 |

**宿主只有一条写入路径**，所有权威状态都走它：

```
调用者 → 私有决定（校验） → 接受结果（持久化，可恢复） → 纯应用（改状态） → 返回
```

这条路径是理解整个系统的钥匙：任何"某处直接改了数据库/内存"的写法都是 bug，不是优化。

---

## 三种人，三条路径

| 谁 | 主要任务 | 从哪进 |
|---|---|---|
| **FDE / 构建者** | 建对象与字段 → 建动作与流程 → 拼页面 → 试跑 → 发布 | 门户里的「构建者」；每个应用有自己的 Studio 树 |
| **业务使用者** | 处理待办、审批、记录与报表 | 首页 → 「我的工作」→ 待办/记录 |
| **治理与运维** | 成员与权限、运行与失败、发布历史、连接健康 | 「控制台」「运行」「数据连接」 |

前端只有一个入口页（`web/apps/workspace`）：一个身份、一个工作区、应用以标签页打开。**没有第二套导航**——首页、门户、应用内菜单说的是同一批应用。

---

## 五个概念，一条脊柱

```
定义 Definition → 候选 Candidate → 发布 Release → 运行 Run → 结果 Result
```

新建的任何东西都沿这条线走；不在这条线上的能力（企业模型、知识库、AI 评测、时序观测、协议包……）都是**可选件**，装不装取决于当前交付，而不是默认打开。

| 概念 | 一句话 | 归属 |
|---|---|---|
| 定义 | 对象、字段、动作、页面、流程、连接；有版本、有 owner | 各自的 owner 应用 |
| 候选 | 一次发布要带上的东西；不可变、校验过依赖 | `build` |
| 发布 | 候选被激活到某个租户；不可回滚地改历史，但可以再发布一次 | `build` + 宿主 |
| 运行 | 动作、流程、任务、外部效果；有 generation 和恢复入口 | `work` / `flow` / 宿主 |
| 结果 | 已接受的结果；恢复时**应用已保存的结果**，不重新裁决业务 | 宿主日志 |

---

## 代码地图

| 想改什么 | 打开 |
|---|---|
| 内核语义（K1–K9） | `contract/`（先改规范与向量，再改 Go/Rust/TS） |
| 写入路径、日志、恢复、授权 | `capabilities/server/` 根包（423 个文件，`doc.go` 是地图，`routes_*.go` 是路由） |
| 应用能看见的 API | `capabilities/server/platform/`——应用只能导入它，不能导入宿主 |
| 平台应用 | `capabilities/server/apps/{build,flow,ai,work,enterprise,knowledge,files,relations,core}` |
| 行业应用 | `apps/<id>/server` + `web/packages/<id>` |
| 跨应用协议 | `protocols/` |
| 前端应用 API / UI Kit | `web/packages/app`、`web/packages/ui` |
| 构建者编辑器 | `web/packages/build` |
| 治理与运维界面 | `web/packages/platform` |
| 工作区外壳 | `web/apps/workspace`（`session/` 身份、`host/` 读取与决策、`shell/` 导航） |

---

## 跑起来

需要 Go 1.27.1、Node.js ≥ 22.18.0、pnpm 12.5.1。

```bash
cd web && corepack pnpm install --frozen-lockfile && cd ..   # 首次
make lite SOLUTION=manufacturing     # 无 Docker；酒店默认 8495，制造 8490
make local                           # 全栈开发（需要 Docker：Air + Vite 热更新）
```

| 命令 | 用途 |
|---|---|
| `make help` | 所有命令 |
| `make check` | 提交前：宿主编译与 vet、边界、Web 类型、复杂度预算 |
| `make test` / `make test-go PKG=./platform RUN=TestX` | 单元测试 |
| `make e2e SPEC=page-notice` | 一条浏览器路线 |
| `make budget` | 复杂度预算（见下） |
| `make verify` | 发布前的完整证据 |

---

## 文档怎么读

**不要通读 ADR。** 按你要回答的问题读一份。

| 我想知道 | 读 |
|---|---|
| 这个系统是什么、代码在哪 | 本文件 |
| 现在的结构与能力边界 | [Platform](docs/Platform.md)（§2 是结构，§2.4 是能力清单，其余按需查） |
| 某个决定为什么这样、现在还有效吗 | [ADR 索引](docs/ADR/README.md)——37 份，一行一份，标了现行范围与并入了谁 |
| 现在做什么 | [WorkQueue](docs/WorkQueue.md)（唯一的施工队列） |
| 为什么做这个产品、为谁做 | [Intent](docs/Intent.md) |
| 怎么检查、检查能证明什么 | [Testing](docs/Testing.md) |
| 已有能力怎么用 | [Apps](docs/Apps.md) |
| 编码规则与边界 | [AGENTS](AGENTS.md) |

文档分工的唯一原则：**一个事实只有一个地方写**。同一件事在第二处出现，就是 bug。

---

## 复杂度预算：数字只减不增

这个仓库过去的做法是每轮审计一遍、再写一份结论；一轮审计的净收益是**减 72 行**，而文档又长了 3,000 行。现在换成一条可运行的规则（[ADR-0064 §9](docs/ADR/0064-architecture-subtraction-and-code-organization.md#9-复杂度预算)）：

```bash
make budget        # 14 个数字：ADR 份数与行数、入口数、菜单项、视图、最大文件……
```

数字记在 `.budget.json`。**超上限即失败**：想加一个应用、一个菜单项、一份 ADR，就在同一次改动里删掉等量的东西。删不动，说明这个新增不是必需的。

这不是目标管理，是刹车。
