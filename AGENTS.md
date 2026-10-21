# AGENTS.md

给在本仓库工作的 AI 与人的工作原则。**这里只放不会过期的约束与入口**；系统结构归 [docs/Architecture.md](docs/Architecture.md)，产品宗旨归 [docs/Intent.md](docs/Intent.md)，当前任务归 [docs/WorkQueue.md](docs/WorkQueue.md)，内核语义归 [contract/spec](contract/spec/README.md)。`CLAUDE.md` 只引用本文件。

本仓库没有决策日志。决定写在代码、契约规范与提交信息里。**不要为工作过程新增文档、队列、检查点日记或"实施记录"。**

## 项目

面向 FDE 与客户构建者的业务应用构建平台：交付的是**构建业务系统的能力**，不是我们预先写好的应用。内核契约 → Go 宿主 → Web 工作区 → 参考应用验证。

## 工作原则

1. **独立判断。** 代码、注释与旧文档都是待核实的陈述，不是前提。发现结构不合理，改结构；不要为了兼容旧设计在它上面叠补丁。
2. **直接修改。** 需要改的代码直接改到位，不写"建议""待办"或迁移垫片。被替换的路径同批删除。
3. **必要验证。** 一个完整模块做完后统一运行适用检查；编码期间只做必要的编译检查，已通过且未受影响的结果直接复用。没运行、没观察的不能报为通过。
4. **允许替换旧设计。** 系统应该随理解加深而更合理，而不是更复杂。重复路径、兼容层和"以后可能用到"的抽象都算债务。
5. **先读后改。** 改一块之前读它和它的调用方；沿用现有命名、结构与注释密度。
6. **最小范围。** 不多做邻接功能，不顺手重构无关代码，不为假想的未来设计。

## 判断标准

做完一件工作后，用这三条自检：

- **更简单了吗？** 行数、文件数、概念数是增加还是减少？只是把复杂度搬到别处，不算完成。
- **变得可用了吗？** 用户能通过产品入口完成一件事，而不是只多了一个接口或测试。
- **能被理解吗？** 新人（或下一次会话的 AI）读完代码与 `Architecture` 能不能说清它为什么这样组织。

## 硬规则

1. **内核是契约。** `contract/` 不含领域词汇。内核语义变更先改规范与向量，再改 Go 与 Rust/TypeScript 边缘；三端跑同一套向量。`v1` 语义变更在提交信息里写清理由。
2. **一项决策只改一个权威。** 跨应用只走 `protocols/`，用请求—应答与补偿，不做跨权威原子性。
3. **写入只走一条路径。** 权威状态的新写入必须经接受结果；新封存结果恢复只应用已保存映像，不重跑规则、不重新授权、不发起外部调用；旧输入的兼容读取责任见 Architecture。历史字节永不重写。
4. **单一归属，先复用。** 应用只导入 `platformserver/platform`；客户端只用 `@platform/ui` 与 `@platform/app`。缺的能力在归属方扩展。UI 变更前先 `node scripts/catalog.mjs search <关键词>`。
5. **生成文件不手改。** `contract/go/gen`、`web/packages/kernel/src/gen/host.ts`（宿主 API 类型变了就在 `capabilities/server` 跑 `go run ./cmd/api-types`）、`web/apps/catalog/src/gen`（`node scripts/catalog.mjs generate`）。
6. **一个语义一个实现。** 规则求值、约束检查、授权判定、依赖解析各一份。替换不是增量：同批删除被替代的路径，不保留两套规则。
7. **租户扩展只走受控路径。** 受控编译、隔离执行、版本化制品；不在宿主直接执行租户脚本。
8. **参考应用是验证载体。** `apps/*` 不定义平台真理；行业规则在用或已承诺的能力由构建者声明替代后才退出。撞墙时登记为通用缺口并在归属方补齐，不在探针里绕过。
9. **双语文案。** 产品界面英文原文 + 简体中文：应用 `i18n/zh-CN.json`，Web 用 `t()` + 包内 `i18n.ts`。业务数据不翻译。
10. **提交信息**单句祈使句；提交与推送按负责人授权。
11. **安全。** 不提交密钥与凭据；密钥只按名字从部署环境取。

不可动边界的完整清单在 [Architecture §8](docs/Architecture.md#8-不可动的边界)，开工前看一遍。

## 检查入口

唯一入口是根目录 `Makefile`（`make help`）。按影响面选：

| 改了 | 跑 |
|---|---|
| 文档、规则、注释 | 链接与引用一致、`git diff --check` |
| 内核规范 / 向量 / 实现 | `make verify`（含 `scripts/verify.sh contract`）；受影响的 Rust/TypeScript 边缘 |
| Lean 模型 | `scripts/verify.sh formal` |
| Go 宿主 | `make check` + `make test-go PKG=<改动包> RUN=<owner 测试>`；改应用 API 或组合时加 `scripts/verify.sh composition` |
| 应用 / 协议 | `scripts/verify.sh composition`；MES 规则加 `mes`，PMS 桌面链加 `pms` |
| Web | `make check-web`（生成一致性、Catalog、全部包 tsc）；只在对应包运行受影响的单元测试，`make test` 是完整测试入口 |
| 成组交付或发布节点 | 选一条浏览器路线 `make e2e SPEC=<name>`；完整浏览器任务用 `make e2e`，发布构建与契约检查用 `make verify` |
| 结构整理（搬文件、拆函数） | 整块完成后按受影响语言运行构建、类型与原边界检查；有实际行为影响时再选对应测试，不默认跑整个宿主测试、不为搬迁新增测试 |
| 部署、恢复、存储 | `make rehearse`；需要真实 PostgreSQL 的用例设 `PLATFORM_TEST_DATABASE`，未设置即跳过，**跳过的不能报为通过** |

测试写法：浏览器 spec 用 `web/e2e/tests/kit.ts`（`builder`/`editor`/`runtime`/`shots`），一个 spec 一条路线；宿主测试用 `capabilities/server/testkit_test.go`（`composeTenant`/`builderTenant`/`seatOf`/`decide`/`refuse`/`publishObject`）。不手写登录、对象、页面、发布或 `pb.Submission`。不写像素断言、为每种设备复制的路线、或服务端已证明规则的浏览器重测。外观与手感靠截图和负责人走查。

CI 只在 `v*` 标签上运行，不要依赖它发现问题。

## 分支同步与本地交付

- 开始前看 `git status`，保留他人改动。提交、推送和部署沿用本次授权。
- 跨会话分支只按路径合成内容，**不 merge、cherry-pick、rebase 或 reset 到对方分支**。每轮使用双方上次确认的内容基点 B，分别计算 `git diff --name-only B <对方冻结HEAD>` 与 `git diff --name-only B HEAD`；不能用 `merge-base` 猜内容同步点。
- 交付方先提交并推送，提供冻结 HEAD、B 和变化路径。集成方只取该冻结版本：只有一方变化的文件逐字取该方；双方变化的文件以 B 为共同内容做三方合成，保留两边的修复。生成物用原工具重新生成。
- 取回集成结果时按同样的路径比较与三方合成；本分支已有新工作时不整树覆盖。同步确认是与集成提交的差异恰好等于本轮未交付的新工作路径，并报告新冻结 HEAD；只有没有新工作时才要求两树相同。`fetch` 不代表工作树已同步。
- **每次 main 代码更新后运行 `deploy/local/update.sh`**，更新酒店 `8495` 与制造 `8490` 两宿主，核对实际 Git 版本、健康和页面资源；**保留原数据卷、密钥与租户配置**。超时或重启不算交付完成。已有工作台应用共用所属宿主，不另建应用专用容器或固定端口。
- 交付说明提供集成提交、冲突处理和实际检查范围；完整启动、更新与恢复操作见 [本地环境指南](deploy/local/README.md)。

## 开发环境

- 支持 Go 1.27.1、Node ≥ 22.18.0；Web 固定 pnpm 12.5.1。
- `make lite` 无 Docker 运行（酒店 8495、制造 8490，数据在 `.build/lite/<solution>/`）；`make local` 热更新；`make docker` 切回容器宿主。账号、地址、密钥与存储的完整说明只在 [deploy/local/README.md](deploy/local/README.md)。
- 应用骨架：`cd capabilities/server && go run ./cmd/new-app -id <id> -entity <name>`。

## 沙箱（无外网、Go 代理被封时）

1. `bash scripts/sandbox-go.sh`，执行它打印的 `export`（`PATH`、`GOTOOLCHAIN=local`、`GOWORK`、`GOPROXY=direct`）。Go 工具链从 PyPI 取，外部工具从 GitHub Releases 或 npm/PyPI 按需取，不入库。新增模块后重跑脚本。
2. Web：`cd web && corepack pnpm install --frozen-lockfile`，再从根目录 `npm_config_workspace_concurrency=1 make check PNPM="corepack pnpm"`。没装浏览器时写明"未在浏览器观察"，不能拿 tsc 当浏览器证据。
3. 轻量验证：`go build ./... && go vet ./...`；`go test ./apps/<包>/... ./platform/...`；根包按名跑 `go test . -run 'TestLanguages|TestAPIContract'`。
4. 沙箱会被重置：每个整块做完立即 `git commit && git push origin <会话分支>`；重置后 `git fetch && git reset --hard origin/<分支> && git clean -fd`，再回到第 1 步。
