# Design

## Context

现状（已核对代码）：

- 内核 `contract/` 5.5k 行，宿主 `capabilities/server` 197k 行（其中测试 64.7k），Web 包 66.6k 行，e2e 6.9k 行。
- `build` 应用已经把对象、页面、动作、查询、函数、流程与智能体保存为数据；发布即安装，租户页面由 `@platform/app` 的通用页面运行时渲染。这是**对租户应用的自举**。
- 平台自身的应用（`build`、`ai`、`work`、`flow`、`core`）、Studio 编辑器、路由表与 UI 包都是编译期代码；工作区通过 `defineApp` 静态加载代码包，把租户定义（`source !== "code"`）投影成普通应用入口。
- 宿主是一个二进制 + 一份工作区 dist 服务全部租户（`-tenants`、`-web`）；`deploy/local/update.sh` 已具备"重建 → 核对 revision → healthz"的骨架，但要求工作树干净、由外部人执行。
- 没有任何"系统变更"对象，也没有人能改系统本身；AI 只能调用已发布的动作与查询。

因此要补的不是"更快的发布脚本"，而是一层**系统作用域**：把系统自身变成与租户应用同构的受控对象，并为它定义唯一的写入路径、构建路径与回退路径。

## Goals / Non-Goals

**目标**

- 一条路径改系统：人（前端）与 AI（提示词）都通过 `Tenant.Submit` 提交 `system.change`，经校验、批准、构建、激活生效。
- 内核冻结：`contract/`、提交管线、接受结果、日志/重放/快照不可被自举改动，由代码拒绝而非约定。
- 定义类改动热生效；代码类改动走独立 updater、有窗口切换、失败自动回滚。
- 系统定义对全部租户只读可见，对 system 租户可写。
- 同批删除与自举无关的设计、代码与测试。

**非目标**

- 不承诺零停机、蓝绿双活或每租户独立系统版本。
- 不把现有 Studio 编辑器改写成数据页面（第一阶段只新增 Platform 应用与系统记录；Studio 本体仍随源码演进）。
- 不引入新的执行器、第二数据库、插件市场或外部 CI 依赖。
- 不允许修改内核；内核演进仍走正常版本发布。
- 不为行业应用、集成驱动、企业建模或专业可视化保留兼容层。

## 已定决策

| # | 决策 | 结论 | 直接后果 |
|---|---|---|---|
| 1 | 可编辑基质 | 元对象 + 源码 | 系统既有定义层也有代码层 |
| 2 | 内核 | 冻结 | 路径闸门必须可执行 |
| 3 | 权威 | system 租户日志 | 与租户应用同一条写入路径 |
| 4 | 生效 | 分层 | 定义热激活，代码构建重启 |
| 5 | AI 产物 | 提案 + 人工批准 | AI 不能自批 |
| 6 | 人工入口 | 复用 Studio | 不新造编辑器 |
| 7 | 作用域 | host 单例 | 一份二进制/前端服务所有租户 |
| 8 | 源码粒度 | 整文件 | 每个文件一条记录，按内容寻址 |
| 9 | 物化 | 独立构建工作树 | 仓库只是 seed，可导出 |
| 10 | 构建/重启 | 独立 updater | 宿主不编译、不重启自己 |
| 11 | 停机 | 有窗口、先排空 | 不承诺零停机 |
| 12 | 删除力度 | 只剩内核 + 自举 | 约 10 万行退出 |
| 13 | 保留应用 | build/ai/work/flow/core | enterprise 收敛为组织目录 |
| 14 | 测试 | 跟随删除 + 最小验收 | 保留向量、管线、存储与自举测试 |
| 15 | AI 文件范围 | 全树 + 内核闸门 | 白名单之外拒绝 |
| 16 | 批准 | 一次批准 + 可取消窗口 | 提案页给 diff、影响面与日志 |
| 17 | 人工顺序 | 先元对象，后源码 | 每步独立验收 |
| 18 | 变更对象 | 复用 build 发布机制 | 不建第二套发布 |
| 19 | 组织模型 | 保留最小目录 | 删 UAF 元素/视图/生成器 |
| 20 | seed 与 git | seed + 导出 | 运行时永不依赖 git |
| 21 | AI 执行 | 宿主内 agent + 上限 | 20 轮、构建 10 分钟、2 次失败即停 |
| 22 | 元对象作用域 | system 租户 | 不给实体加 scope 字段 |
| 23 | 并发 | 修订检查 + 拒绝 | 冲突可见、可重取 |
| 24 | 验收 | 四次真实演练 | 单元测试不算交付证据 |
| 25 | 交付物 | OpenSpec change | 本目录即蓝图、规范与任务 |
| 26 | 删除时机 | 同一 change，第一块 | 先得到可编译的精简树 |
| 27 | 界面入口 | Studio 内 Platform 应用 | 不新增顶层入口 |
| 28 | 角色 | builder / publisher | AI 以受管主体提案 |
| 29 | 构建环境 | 本机工具链 | lite 模式无需 Docker |
| 30 | 依赖 | 允许新增，记录进 release | 构建可联网，离线明确失败 |

## 目标结构

```
                     system 租户（host 单例，唯一权威）
   人 Studio/Platform │  system.file/change/release/activation + build 元对象
   编辑元对象/源码 ──► │  Tenant.Submit → 私有决定 → 接受结果 → 持久接受
   AI 提示词 ────────► │  system.architect（工具、预算、无批准权）
                     └───────────────┬───────────────────────────┘
                                     │ 已批准 system.change
                                     ▼
                          platform-updater（独立进程，owner-only socket）
                          物化工作树 → 固定命令构建 → 体检 → 排空 → 切换
                                     │ 失败
                                     └──────► 回滚上一 release
                                     ▼
                 platform-server（N 租户）+ workspace dist（共享一份）
```

两条生效路径：

```
定义类（对象/字段/页面/导航/动作/函数/智能体）
  草稿 → 候选 → system.release（无二进制） → 发布 → 安装到 system 作用域
        → 各租户下一次读取 definitions 时生效，不重启

代码类（Go / TypeScript / 构建配置）
  system.change（文件清单） → 校验与内核闸门 → publisher 批准
        → updater 构建并冻结 system.release（二进制摘要 + dist 摘要）
        → 排空 → 切换 → 健康 → 生效；失败自动回滚
```

## 系统作用域与元对象

- **system 租户**：宿主级、唯一、不可由 Console 创建或删除；成员是平台操作者席位。它运行与业务租户相同的应用组合（`build`、`ai`、`work`、`flow`、`core` + Console/Agents），因此不需要第二套提交或存储。
- **元对象**：system 租户里的 `build.*` 记录（应用、对象、动作、页面、查询、函数、智能体、流程）与四个 `system.*` 记录。它们描述平台自己，而不是某个租户的业务。
- **只读投影**：宿主在读取任意租户的 definitions 时合并 system 作用域已安装的定义，标记 `source: "system"`；页面与导航由此出现在每个租户的工作区。编辑动作只在 system 租户的提交中可用；其他租户提交 `build.*` 到 system 资产一律拒绝。
- **可见性仍按当前权限过滤**：投影不绕过只读范围、角色或标记。
- **Platform 应用**：system 租户里一个特殊应用，进入 Studio 的应用切换器；它的页面就是系统元对象本身（系统变更列表、发布、文件、激活），编辑体验与业务应用一致。

## 数据模型

新增四个实体，全部在 `build` 应用中声明（system 租户安装后生效），沿用 `platform.Record`、生命周期、标准动作与接受结果。

### system.file

| 字段 | 含义 |
|---|---|
| `path` | 仓库相对路径，唯一；不含 `.git`、密钥或内核路径 |
| `digest` | 内容 sha256，blob 按摘要存储 |
| `size`, `mode` | 字节数与可执行位 |
| `revision` | 该路径的修订号，写入必须携带期望值 |
| `deleted` | 墓碑；内容与历史仍可读 |

内容寻址：同一内容只存一份；被任何 change 或 release 引用的 blob 不回收。删除是追加墓碑，不重写历史。

### system.change

| 字段 | 含义 |
|---|---|
| `title`, `rationale` | 人读的意图 |
| `origin` | `prompt` 或 `ui` |
| `prompt` | 原始提示词（origin=prompt） |
| `base` | 基线 release 版本 |
| `files` | 冻结清单：`[{path, digest, deleted}]` |
| `assets` | 关联的 `build.*` 草稿引用 |
| `diagnostics` | 构建/校验输出摘要 |
| `state` | draft → validated → approved → activating → applied / failed / rejected / withdrawn |
| `approvedBy`, `approvedAt` | publisher 的批准证据 |
| `window` | 切换前可取消秒数（默认 30） |
| `agent` | 提案的受管主体（AI 时非空） |

变更一经 `approved`，文件清单冻结；之后再改必须新建 change。合并冲突按 `system.file.revision` 拒绝，返回当前修订，不静默覆盖。

### system.release

| 字段 | 含义 |
|---|---|
| `version` | 递增整数 |
| `change` | 来源变更 |
| `manifest`, `manifestDigest` | 文件路径与摘要的规范清单 |
| `binaryDigest`, `webDigest` | 构建产物摘要；定义类发布为空 |
| `toolchain` | go / node / pnpm 版本 |
| `deps` | go.sum、pnpm-lock 的摘要；新依赖在此留证 |
| `previous` | 上一 release，用于回滚 |
| `state` | published → active → superseded / failed |
| `health` | 激活后的健康证据 |

发布不可变；修正只能新建 release。

### system.activation

| 字段 | 含义 |
|---|---|
| `release`, `from` | 目标与来源版本 |
| `requestedBy`, `at` | 触发者与时间 |
| `drainedAt`, `swappedAt` | 排空与切换时刻 |
| `result` | active / rolled-back / aborted |
| `failure`, `evidence` | 失败原因与日志摘要 |

激活本身是一次接受结果：先记录决定与证据，再由 updater 执行；重放只应用保存的映像，不重新构建、不重启进程。

## 内核闸门

`internal/systemscope` 提供**纯函数**：给一组路径，返回拒绝项。保存前与 updater 构建前各执行一次，两道都必须过。

拒绝列表：

```
contract/**                          内核语义、向量与生成类型
capabilities/server/host.go          提交入口与管线持有者
capabilities/server/accepted_*.go    各结果族的接受与封存
capabilities/server/replay*.go       恢复的重放分支
capabilities/server/snapshot*.go     快照与检查点映像
capabilities/server/journal/**       日志、SQL 状态与读取视图
capabilities/server/internal/acceptance/**  共同格式与变化计划
capabilities/server/internal/records/**     冻结映像、历史与索引
capabilities/server/internal/host/**        宿主内部窄角色
capabilities/server/platform/authz/**       授权判定
capabilities/server/idp/**                  身份提供方
.git/**, *.key, .env*                版本库与凭据
```

其余路径（平台应用、`platform/` 应用 API、`cmd/`、Web、脚本、部署、文档、Makefile）可改。生成物（`web/packages/kernel/src/gen/host.ts`、`web/apps/catalog/src/gen`）的生成器保持唯一来源；删除 Catalog 后只剩宿主类型生成。

闸门是**拒绝**而不是警告：越界路径使整个 change 无效，并记录审计。

## 变更生命周期

1. **产生**：人在 Platform 应用里编辑元对象/文件；或 AI 在提示词运行中写入临时工作树。
2. **固化**：change 保存时冻结文件清单（摘要 + 删除集）与关联草稿；校验路径闸门、大小上限（单文件 2 MB、整批 20 MB）、二进制文件拒绝。
3. **校验**：静态检查（闸门、清单完整性、依赖文件存在）；可选快速构建（Go 编译 + tsc）产出诊断。
4. **批准**：`system.change.approve` 需要 `publisher`（system.publisher）；提案主体不能批准自己的提案。
5. **发布**：
   - 只有元对象 → 生成定义类 `system.release`，立即安装。
   - 含文件 → updater 物化并构建；成功后写入 `system.release`（二进制 + dist 摘要）。
6. **激活**：定义类热激活；代码类进入排空窗口，窗口内可取消。
7. **收尾**：写 `system.activation`；失败保留证据并回滚。

## 构建与激活

**物化**：从 blob 重建到 `.build/system/work/<change>/`，按记录的 `go.mod/go.sum`、`package.json/pnpm-lock.yaml` 还原依赖；工作树是干净目录，不含 `.git`、密钥或前一次构建残留。

**构建命令固定**，不执行仓库脚本：

```
go build -trimpath -o build/platform-server ./capabilities/server/cmd/platform-server
pnpm --dir web install --frozen-lockfile      # 仅当 lockfile 变化
pnpm --dir web/apps/workspace build
```

`make`、`scripts/*` 与 `deploy/*` 永不作为构建入口——否则 AI 写入的脚本会在构建期执行，闸门形同虚设。

**体检**：

| 级别 | 内容 | 失败后果 |
|---|---|---|
| L0 | 闸门与清单摘要 | 拒绝，不构建 |
| L1 | Go 编译 + `go vet` + 全包 tsc | 返回诊断，release 不产生 |
| L2 | 新二进制以临时端口与临时数据目录启动，`/healthz` 200 且租户隔离为 0 | 回滚，不切换 |
| L3 | 切换后真实端口 `/healthz` 与一条冒烟读取 | 自动回滚到 `previous` |

**切换**：向旧宿主发 SIGTERM → 等在途请求结束、日志检查点落盘（有上限，超时中止）→ 原子替换 `current` 链接 → 启动新二进制（相同 `-data`、`-tenants`、`-web`）→ L3 → 成功则旧版本进入保留集。

**回滚**：保留最近 5 个 release 的二进制与 dist；回滚是"启动上一版本"，不重建、不改日志。

**前端**：dist 与二进制来自同一工作树；切换后客户端检测到 release id 变化，提示刷新。离线快照不跨 release 复用。

## AI 入口

- **主体**：`system.architect` 是 system 租户声明的一个普通 Agent，主体为 `agent:build.system-architect`，没有额外特权，不能批准。
- **工具**：`list`、`read`、`search`、`write`、`delete`、`build`、`diff`。全部作用于临时工作树并受同一闸门约束；`write` 携带期望修订。
- **预算**：最多 20 个模型轮次、单次构建 10 分钟、2 次构建失败即停；令牌与费用走现有 Budget 与计量。
- **上下文**：先 `Architecture` 与目录，再按需搜索；不把整仓塞入提示词。
- **产物**：一个 `system.change`（origin=prompt），含提示词、理由、文件清单、诊断；AI 的每次读写都进入它的运行日志，可回放。
- **禁区**：不联网（依赖解析由 updater 构建阶段完成）、不读凭据、不写内核路径、不能批准或激活。

## 人工入口

**第一阶段（元对象）**

- Studio 的应用切换器出现 **Platform**；进入后是 system 租户的应用上下文。
- 复用现有编辑器：对象、字段、页面、导航、动作、函数、智能体；保存即草稿，发布即定义类 release。
- **System changes** 视图：变更列表、文件 diff、受影响的元对象/页面/权限清单、构建日志、批准、可取消窗口。

**第二阶段（源码）**

- 同一视图内增加文件树与编辑器：打开、编辑、保存（修订冲突拒绝）、与元对象同一个 change。
- 不提供终端，不执行任意命令；预览走已构建的工作区。

## 删除与保留

每批删除以"可达性清单"开始：从保留入口出发反查引用，确认无消费者后再删。预期删除集：

| 区域 | 处理 | 规模（约） |
|---|---|---|
| 行业包与 solution 宿主：`deploy/packages/{heating,hospitality,manufacturing}.json`、`deploy/local/{hospitality,manufacturing}`、行业 e2e | 删 | 1.6 MB + 配置 |
| `build` 的集成作者：source / connection / dataset / pipeline / writeback / match / observation / dispatch / confirmation 及测试 | 删 | 9k 行 |
| 外部协同：`gateway/`、`drivers/`、`driver/`、`cmd/{gateway,driver-modbus,driver-s7,driver-simulator,webhook-sink}`、协议包 | 删 | 5k 行 |
| 应用：`knowledge`、`files`、`relations` 及其 UI | 删 | 1k 行 |
| 企业建模：UAF 元素/关系/视图/租户链接、`cmd/uaf-gen`、`docs/standards/uaf` | 删 | 3k 行 |
| 形式化：`contract/lean`、`scripts/formal.sh`、Makefile 的 formal 目标 | 删 | 29 KB 模型 + 62 行脚本 |
| 专业可视化：`ui/src/spatial`、三维/地图/标注组件、`experience/professional-visualization` 规范 | 删 | 3k 行 |
| 组件 Catalog：`web/apps/catalog`、`web/packages/catalog`、`scripts/catalog*`、生成物 | 删 | 1k 行 + 生成物 |
| 跟随测试：上述模块的单测、e2e、fixture | 删 | 50k 行 |
| 保留 | `contract/{proto,spec,vectors,go}`、宿主提交/日志/存储、`build` 的模型/页面/动作/函数/智能体/发布、`core`、`ai`、`work`、`flow`、最小工作区与 Studio 编辑器、Playwright `kit.ts` | |

`enterprise` 收敛为最小组织目录：保留单位、结构、任职与 `/v1/organization`；删除元素、关系、视图、租户链接与专业建模 UI。`platform/ai-knowledge` 的模块名保留（它仍主责模型治理、函数与智能体），只移除知识部分，避免为改名做一次无收益的全仓引用整理。

## 恢复、重放与导出

- 系统写入与其他租户完全同构：先私有决定，再接受结果，再应用；恢复只应用已保存映像。
- 激活决定是接受结果；重放不构建、不重启、不重新授权。
- **导出**：`make system-export VERSION=<n>` 从 journal 生成干净工作树与补丁，供评审或灾难恢复；导出不是权威，运行时永不读 git。
- **首次 seed**：当前 HEAD 的已删减树构建一次，登记为 release #1（记录工具链与两侧摘要）；此后 journal 为准。

## 风险与取舍

| 风险 | 处理 |
|---|---|
| 自编译失败导致无法更新 | updater 与宿主分离；构建失败不触碰运行版本；保留 5 个版本与工具链；保留一条命令行人工发布路径 |
| AI 通过构建脚本绕过闸门 | 构建只执行固定命令，不运行 `make`/`scripts`；工作树无凭据、无 `.git`；构建进程最小权限 |
| 新依赖供应链 | 允许新增但把 lockfile 与解析结果写入 release；构建日志留证；失败/异常可回滚 |
| 共享 dist 的并发生效 | 切换是全租户同时生效；客户端按 release id 提示刷新；不承诺新旧前端并存 |
| 删除破坏 flow/work/ai 引用 | 每批以编译、vet、tsc 为门；保留集回归；删除跟随规范同步 |
| 测试证据下降 | 保留内核向量、管线与存储测试；新增自举验收；未观察项明确标注 |
| 双权威漂移（journal 与 git） | journal 权威；仓库只作 seed 与导出；部署核对 release id 与实际二进制摘要 |
| 元对象膨胀成第二套平台 | 只允许 system 租户写；只读投影；不为每个新界面新建模型 |

## 迁移计划

1. 第一块删除完成后，树保持可编译、可启动。
2. 建 system 租户与四个记录，导入 seed release #1（此时还没有自举入口，用命令行完成）。
3. 上线 updater，先只做"构建 + 体检 + 回滚"演练，不开放写入口。
4. 开放人工元对象入口；再开放 AI 入口；最后开放源码编辑器。
5. 每步结束后重建本地两台宿主（`deploy/local/update.sh`），保留数据卷、密钥与租户配置，核对实际 release id 与页面资源。

## 验收

| # | 演练 | 判据 |
|---|---|---|
| 1 | 人在 Platform 里新增一个系统对象字段/页面并发布 | 各租户刷新后可见；无重启；definitions 带 `source: system` |
| 2 | 一句提示词让 AI 改一处平台自身行为，publisher 批准 | 生成 change、构建、重启、生效；`/healthz` 报新 release；运行日志可回放 |
| 3 | 一次故意写错的构建 | 构建失败、release 不产生、运行版本与数据不变；再次注入启动失败，自动回滚到上一版本 |
| 4 | 全程核对数据卷与修订 | 租户日志、记录、文件与在途工作不变；`git` 导出与 release 清单一致 |

四次演练都要留下实际观察（浏览器路线、构建日志、healthz、release 摘要），未观察项如实标注。
