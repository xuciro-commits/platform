# AGENTS.md

给在本仓库工作的 AI 与人的工作原则。**这里只放不会过期的约束与入口**；系统结构归 [Architecture](docs/Architecture.md)，模块行为与不变量归 [OpenSpec 规范目录](openspec/README.md)，产品宗旨归 [Intent](docs/Intent.md)，当前任务归 [WorkQueue](docs/WorkQueue.md)，内核语义归 [contract/spec](contract/spec/README.md)。`CLAUDE.md` 只引用本文件。

本仓库使用 **OpenSpec** 管理规范及其变更，不恢复编号决策日志。稳定约定在 `openspec/specs`，本次变化的差量与必要设计在 `openspec/changes`；不要另建过程报告、状态队列或检查点日记。

## 项目

面向 FDE 与客户构建者的业务应用构建平台：交付的是**构建业务系统的能力**，不是我们预先写好的应用。内核契约 → Go 宿主 → Web 工作区 → 参考应用验证。

系统自身的源码、变更、发布与激活沿原接受结果和独立 updater 完成，详见 Architecture 的自举章节。本地组合只提供一个 Platform 入口，默认 8495；应用、API 与登录共用该入口。账号、地址及私有配置的唯一说明在 deploy/local/README.md。

## 工作原则

1. **独立判断。** 代码、注释与旧文档都是待核实的陈述，不是前提。发现结构不合理，改结构；不要为了兼容旧设计在它上面叠补丁。
2. **直接修改。** 需要改的代码直接改到位，不写"建议""待办"或迁移垫片。被替换的路径同批删除。
3. **必要验证。** 一个完整模块做完后统一运行适用检查；编码期间只做必要的编译检查，已通过且未受影响的结果直接复用。没运行、没观察的不能报为通过。
4. **允许替换旧设计。** 系统应该随理解加深而更合理，而不是更复杂。重复路径、兼容层和"以后可能用到"的抽象都算债务。
5. **先读后改。** 改一块之前读它和它的调用方；沿用现有命名、结构与注释密度。
6. **最小范围。** 不多做邻接功能，不顺手重构无关代码，不为假想的未来设计。
7. **不写代码注释。** 源码、脚本与配置不留注释（`//go:` 等工具指令、`@ts-`、shebang 除外）；原因与约束写进 OpenSpec、Architecture 或提交信息，避免污染后续开发上下文。

## 判断标准

做完一件工作后，用这三条自检：

- **更简单了吗？** 行数、文件数、概念数是增加还是减少？只是把复杂度搬到别处，不算完成。
- **变得可用了吗？** 用户能通过产品入口完成一件事，而不是只多了一个接口或测试。
- **能被理解吗？** 新人（或下一次会话的 AI）读完代码与 `Architecture` 能不能说清它为什么这样组织。

## 硬规则

1. **内核是契约。** `contract/` 不含领域词汇。内核语义变更先改规范与向量，再改 Go 与 Rust/TypeScript 边缘；三端跑同一套向量。`v1` 语义变更在提交信息里写清理由。
2. **一项决策只改一个权威。** 跨应用只走版本化协议声明，用请求—应答与补偿，不做跨权威原子性。
3. **写入只走一条路径。** 权威状态的新写入必须经接受结果；新封存结果恢复只应用已保存映像，不重跑规则、不重新授权、不发起外部调用；旧输入的兼容读取责任见 Architecture。历史字节永不重写。
4. **单一归属，先复用。** 应用只导入 `platformserver/platform`；客户端只用 `@platform/ui` 与 `@platform/app`。缺的能力在归属方扩展；`scripts/boundaries.sh`、`scripts/escapes.sh` 的已知逃逸只减不增。UI 变更前先 `node scripts/catalog.mjs search <关键词>`。
5. **生成文件不手改。** `contract/go/gen`、`web/packages/kernel/src/gen/host.ts`（宿主 API 类型变了就在 `capabilities/server` 跑 `go run ./cmd/api-types`）、`web/apps/catalog/src/gen`（`node scripts/catalog.mjs generate`）。
6. **一个语义一个实现。** 规则求值、约束检查、授权判定、依赖解析各一份。替换不是增量：同批删除被替代的路径，不保留两套规则。
7. **租户扩展只走受控路径。** 受控编译、隔离执行、版本化制品；不在宿主直接执行租户脚本。
8. **业务验证走声明式应用包。** 原生参考应用与专用 Solution 宿主已退役：业务任务由部署提供的声明式应用包表达，不定义平台真理，也不把行业规则写进宿主、平台应用或共享 UI。撞墙时登记为通用缺口并在归属方补齐，不在应用包里绕过。
9. **双语文案。** 产品界面英文原文 + 简体中文：应用 `i18n/zh-CN.json`，Web 用 `t()` + 包内 `i18n.ts`。业务数据不翻译。
10. **提交信息**单句祈使句；提交与推送按负责人授权。
11. **安全。** 不提交密钥与凭据；应用和包只保存凭据引用，实际秘密走平台受控加密托管或部署注入，加密主密钥留在部署环境。

开工前从 [规范目录](openspec/README.md#层级与模块) 选择相关模块，读完整需求和场景，再核对实现；不用每次审计全仓。内核精确条款仍以 `contract/spec` 为唯一语义来源，OpenSpec 不复制一份。

## OpenSpec 工作方式

- **按层级和模块维护。** `kernel / runtime / platform / experience` 下聚合完整职责。小功能、接口和组件进入现有模块；只有独立稳定的职责才拆分，不按任务新建 spec。同一规则只由一个主责模块定义，其他模块链接引用。
- **使用官方 Skill。** `.agents/skills/openspec-*/SKILL.md` 由 CLI 生成。查明问题用 `openspec-explore`；需要改变行为、公开契约、存储、授权或发布边界时用 `openspec-propose`，落实用 `openspec-apply-change`，变化调整用 `openspec-update-change`，完成后用 `openspec-sync-specs` 同步并 `openspec-archive-change` 归档。用户仅要求提案时不实施；已有明确实施授权时继续完成该范围，不重复索取同一授权。
- **保持轻量。** 修复既有约定下的回归、纯搬迁、措辞与来源定位，直接改实现或文档，不为凑流程创建需求。WorkQueue 仍是唯一项目顺序，change 的 tasks 只拆本次工作。当前 spec 不承接未实施设想，也不把场景当成已跑测试。
- **统一命令。** 不要求全局安装；Skill 中的 `openspec <参数>` 在本仓库使用 `make openspec ARGS='<参数>'`，版本固定在 Makefile。先 `list --specs` / `show <完整模块路径> --type spec` 找现有约定；只读 `list` 看不到规范。细则见 [规范维护入口](openspec/README.md#后续怎样使用)。

## 检查入口

唯一入口是根目录 `Makefile`（`make help`）。按影响面选：

| 改了 | 跑 |
|---|---|
| 文档、规则、注释 | 链接与引用一致、`git diff --check`；涉及 OpenSpec 时加 `make check-specs`，格式校验不等于业务测试 |
| 内核规范 / 向量 / 实现 | `make verify`（含 `scripts/verify.sh contract`）；受影响的 Rust/TypeScript 边缘 |
| Lean 模型 | `scripts/verify.sh formal` |
| Go 宿主 | `make check`；改应用 API 或组合时加 `scripts/verify.sh composition` |
| 应用 / 协议 | 应用通过原包内验收计划维护；平台机制修改运行对应 owner 检查与适用的通用浏览器路线，另跑 `scripts/verify.sh composition` |
| Web | `make check-web`（生成一致性、Catalog、全部包 tsc） |
| 成组交付或发布节点 | 发布构建与契约检查用 `make verify` |
| 结构整理（搬文件、拆函数） | 整块完成后按受影响语言运行构建、类型与原边界检查 |
| 部署、恢复、存储 | 本仓库已删除全部测试代码与演练脚本；没有运行或观察的行为不能报为通过，需要时明说未验证 |

仓库不含测试代码与浏览器路线（`*_test.go`、`*.test.*`、`web/e2e` 均已删除），也不自动新增；契约向量 `contract/vectors` 与形式化证明保留。外观与手感靠截图和负责人走查。

CI 只在 `v*` 标签上运行，不要依赖它发现问题。

## 分支同步与本地交付

- 开始前看 `git status`，保留他人改动。提交、推送和部署沿用本次授权。
- 跨会话分支只按路径合成内容，**不 merge、cherry-pick、rebase 或 reset 到对方分支**。每轮使用双方上次确认的内容基点 B，分别计算 `git diff --name-only B <对方冻结HEAD>` 与 `git diff --name-only B HEAD`；不能用 `merge-base` 猜内容同步点。
- 交付方先提交并推送，提供冻结 HEAD、B 和变化路径。集成方只取该冻结版本：只有一方变化的文件逐字取该方；双方变化的文件以 B 为共同内容做三方合成，保留两边的修复。生成物用原工具重新生成。
- 取回集成结果时按同样的路径比较与三方合成；本分支已有新工作时不整树覆盖。同步确认是与集成提交的差异恰好等于本轮未交付的新工作路径，并报告新冻结 HEAD；只有没有新工作时才要求两树相同。`fetch` 不代表工作树已同步。
- **每次 main 代码更新后运行 `deploy/local/update.sh`**，更新当前单一 Platform 入口，核对实际 Git 版本、健康和页面资源；**保留原数据卷、密钥与租户配置**。超时或重启不算交付完成。已有工作台应用共用所属宿主，不另建应用专用容器或固定端口。
- 交付说明提供集成提交、冲突处理和实际检查范围；完整启动、更新与恢复操作见 [本地环境指南](deploy/local/README.md)。

## 开发环境

- 支持 Go 1.27.1、Node ≥ 22.18.0；Web 固定 pnpm 12.5.1。
- `make lite` 无 Docker 运行（平台默认 8495，数据在 `.build/lite/platform/`）；`make local` 热更新；`make docker` 切回容器宿主。账号、地址、密钥与存储的完整说明只在 [deploy/local/README.md](deploy/local/README.md)。

## 沙箱（无外网、Go 代理被封时）

1. `bash scripts/sandbox-go.sh`，执行它打印的 `export`（`PATH`、`GOTOOLCHAIN=local`、`GOWORK`、`GOPROXY=direct`）。Go 工具链从 PyPI 取，外部工具从 GitHub Releases 或 npm/PyPI 按需取，不入库。新增模块后重跑脚本。
2. Go 模块依赖：`bash scripts/restore-deps.sh go` 从 GitHub tag 字节级还原 vendor 并按 go.sum 逐 entry 校验（约 1.5 分钟；重跑前 `rm -rf ~/.gopath/pkg/mod/cache/download`）。已知余量：otel 家族 / genproto / proto-otlp 共 10 个模块的 tag 字节与当年代理记录不符，无法在纯 GitHub 环境校验还原——这些模块参与编译的宿主程序在沙箱内不可完整构建；纯 stdlib 包（如 `internal/sink`、`cmd/webhook-sink`）不受影响。有可达 Go 代理的环境直接 `go mod vendor` 即可。
3. Web：`cd web && corepack pnpm install --frozen-lockfile`，再从根目录 `npm_config_workspace_concurrency=1 make check PNPM="corepack pnpm"`。没装浏览器时写明"未在浏览器观察"，不能拿 tsc 当浏览器证据。
4. 轻量验证：`go build ./... && go vet ./...`（受第 2 条余量限制时只验证受影响包）；`go test ./apps/<包>/... ./platform/...`；根包按名跑 `go test . -run 'TestLanguages|TestAPIContract'`。
5. 沙箱会被重置：每个整块做完立即 `git commit && git push origin <会话分支>`；重置后 `git fetch && git reset --hard origin/<分支> && git clean -fd`，再回到第 1 步。
