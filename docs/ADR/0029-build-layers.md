# ADR-0029 构建分层：本地开发、提交前检查、发布

<a id="adr-0029"></a>

**状态：** 现行 · 2026-10-08 · 本地 / 提交前 / 发布三层；环境 profile 与测试取舍在层内（并入原 0017、0030）。

## 背景

`scripts/verify.sh all` 把契约、Lean 证明、宿主、应用、Web 构建和浏览器路线串成一条几十分钟的链；GitHub Actions 在每次 push 上跑同一条链，五六百次运行几乎全部失败，没有人看。本地看代码要重打 Docker 镜像；e2e 有 140 个单行压缩的 spec，每个各自造对象、页面、发布与运行时。

## 决定

1. **一个入口，三层。** 仓库根 `Makefile` 是唯一入口（`make help`）；`scripts/verify.sh` 只保留具名步骤体，不再有 `all`/`ci`。
   - **一键切换**：`make local` 自动停容器宿主并在后台启动两套 Air/Vite 与基础设施，`make docker` 自动停本地进程并重建校验容器宿主。两个模式的浏览器入口都为 `8495/8490`，保留数据卷；本地模式的 Go API 为内部 `18495/18490`。
   - **dev**：`make infra`（PostgreSQL、Rauthy、RustFS、webhook-sink 容器，复用 `deploy/local/compose.yaml` 与 `platform_*` 数据卷）→ `make dev SOLUTION=hospitality`（宿主在本机，`air` 热重启，配置 `deploy/dev/`）→ `make web`（Vite HMR，`/v1` 代理到宿主）。不需要容器时 `make dev-light`（内存日志 + 开发令牌）。需要代码函数编译时 `make infra-compute`，socket 落在 `.build/dev/compute`。
   - **check**：`make check`（宿主 build/vet/gofmt、应用边界、逃逸清单、生成类型、Catalog、全部 Web 包 tsc，分钟级，无浏览器无容器）、`make test`（宿主与 Web 单元测试）、`make test-go PKG= RUN=`、`make e2e SPEC=`。
   - **release**：`make verify`（契约、证明、宿主、应用、Web 构建——CI 跑的同一条）、`make build`（工作台 + 两个方案宿主二进制）、`make images`、`make rehearse`、`make tag VERSION=vX.Y.Z`。
2. **CI 只在版本固定时跑。** `.github/workflows/release.yml` 由 `v*` 标签（或手动）触发：`make verify`，再把工作台 tar、linux amd64/arm64 宿主二进制、SHA256SUMS 挂到 GitHub Release，镜像推到 GHCR。日常 push 不触发任何工作流。
3. **测试规范**（见 `docs/Testing.md`「测试规范」）：浏览器 spec 从 `web/e2e/tests/kit.ts` 取 `test`/`builder`/`operator`/`editor`，不再手写登录、对象、页面、发布、运行时；格式化、一个 spec 一段叙述。宿主测试用 `testkit_test.go` 的 `composeTenant`/`builderTenant`/`decide`/`refuse`/`publishObject`，不再手拼 `pb.Submission` 与幂等键。旧 spec/测试在触碰到时迁移，不单独立项。

## 删除

- `.github/workflows/verify.yml`（已整体注释）、`deploy/local/rehearse-lite.sh`（其路线是 `rehearse.sh` 的子集）、`web/package.json` 的单一 `check` 拆为 `typecheck`/`test`/`build`。

## 后果

- 日常：改 Go 保存即重启、改 Web 即热更新；提交前 `make check`，改到的 owner 测试按名跑。
- 镜像只在 `make images`/发布时构建；本地两台验收宿主仍由 `make local-update`（`deploy/local/update.sh`）更新。
- `make verify` 在发布前跑一次完整证据；CI 只是它在 Linux 上的复跑。

## As built 与本地核对

- 开发使用已验证的 air `v1.67.4`；Makefile 可从 GOPATH/bin 找到安装结果，配置用 `entrypoint`。启动前拒绝已占用的端口；切换模式先停止同端口的验收宿主，保留所有数据卷。计算服务在独立 `platform-dev-compute` 项目，socket 绑定到 `.build/dev/compute`。
- 新引用选择器已登记到 Catalog（含本地示例、双语元数据）；`make help` 包含 e2e。浏览器 kit 使用 `/module`、Page structure/Inspector、菜单导入、现行保存与发布入口；notice 的非法文案不落盘、冻结及门控断言保留。
- 手动发布需指定已有版本标签；验证和制品步骤固定到同一提交，镜像 revision 与被验证提交一致。`make build` 在任一宿主构建失败时退出。
- 2026-10-08 本地验证：setup、infra、infra-compute、check、test、verify、build、page-notice e2e 通过；内存开发模式、酒店/制造数据库开发模式、Rauthy 登录、三目录 air 重启及 Vite HMR 已观察。本机与 Linux amd64/arm64 二进制构建、工作流 actionlint 通过。GitHub 发布/GHCR 推送与 Docker 多架构镜像发布未执行；不将本地检查等同于发布实跑。

## 并入：环境 profile 与测试取舍（原 0017、0030）

本地/提交前/发布三层之外不再有第四层：轻量 profile 是交付形态的一种，测试规则是这三层里"跑什么"的取舍。

### 2. 决定与落地

| 决定 | 落地结果 |
|---|---|
| D1 profile 是显式声明，不是隐式降级 | `-profile delivery\|lightweight`（默认 `delivery`）。`Deployment.validate()` 在打开任何存储前结算：轻量必须有 `-data`，且拒绝 `-database/-files/-oidc-issuer/-oidc-keys/-project`；交付拒绝 `-data/-idp-key`；未知 profile 直接失败。不回退、不混用 |
| D2 轻量持久化 | `Journals` 接口（`journals.go`）抽出读/追加/快照/幂等索引，PostgreSQL 是既有实现；`journal_file.go` 是同语义的单文件后端（`journal.jsonl` + `snapshots/` + `derived/`，纯标准库，无新依赖）。不再为第二种嵌入式数据库预留施工要求 |
| D3 身份 | 交付沿用外部 OIDC/开发 token；轻量用内置 `LocalIdP`（HS256，密钥 `-idp-key`，默认 `<data>/idp.key`）：`-idp-new-key` 只生成一次、绝不替换，`-mint-token <subject>` 打印 token（`<user:email>`/`<client:id>`），`-token-ttl` 默认 12h。席位目录仍是 `-directory`/内置 `Seat`，角色模型与审计不变 |
| D4 文件字节 | `FileStore` 接口不变（ADR-0038）：交付用 S3（`-files`），轻量用 `localFiles`（`<data>/files/<tenant>/<hash>`，内容寻址、原子写、拒绝越界 key）。日志里仍只记哈希 |
| D5 Worker | 两个 profile 都复用 ADR-0057 的独立 Go/wazero worker 与原 Work/结果生命周期；轻量宿主未配置执行 socket 时不宣称计算能力可用 |
| D6 诊断与恢复 | 两种 profile 走**同一个** `restoreTenants`：读本代码的最新快照、重放其后的条目、损坏日志隔离、启动即 `attachJournal`。profile 差异只在存储/身份/文件的位置 |
| D7 不做 | 没有第二个发布系统/激活指针、没有应用私有恢复、没有“轻量版少一半语义” |

### 6. 关系

本稿只决定运行环境的存储/身份/文件/worker 选择；ADR-0010、ADR-0018（部署与席位）、ADR-0018（Web 构建）、ADR-0024（日志与快照）、ADR-0007（遥测）、ADR-0010（接受结果与恢复）、ADR-0057（能力装配与 wazero）与 ADR-0018、ADR-0045（入口、组织、发布）保持不变。轻量 profile 是同一语义的另一组后端，不是第二个平台。

### 决定

只有一条原则：**它为什么存在？删掉它会不会让一个长期不变量失去证据？** 不能回答的删掉。长期不变量是：契约（API/类型/翻译）、原子提交与崩溃恢复、重放兼容、租户隔离与授权、发布生命周期、身份与会话、外部契约（OIDC/MCP/A2A/协议）。按部件、按功能、按一次 ADR 的"冻结-激活-重放"复制品，不是。

| 处 | 之前 | 之后 | 删了什么 |
|---|---|---|---|
| `web/e2e/tests` | 136 spec | 8 spec + kit | 全部 `page-*`（除 kit 样板 `page-notice`）、`application-*`、`link-*`、function/model/catalog/record-work 等按功能路线；`fixtures/`、`workshop-compile.mjs`、`nested-loop-fixture.ts` |
| `capabilities/server/*_test.go` | 158 文件 | 86 文件 | 全部 `page_*`、`application_*_release`、`decimal_*`、`property_*`、按部件聚合、`build_{action_rules,conditions,creates,inventory}`、`process_{candidate,function}`、`typed_process`、`simulate_{candidate,function}`、`release_{binding,candidate}`、`testplan`、`work`、`ops`、`meaning`、`chart_marks`、`pivot`、`projection`、`files_regions`、`flow_query_version`、`function_{conversation,evaluation_input}`、`navigation_release`、`capability_page`、`link_{archive,cardinality}`、`pipelines_enterprise`；`TestRecordsAtScale`/`TestKnowledgeAtScale` 两条计时用例（抖动来源） |
| Web 单元测试 | 150 | 37 | 45 行以下的组件渲染冒烟与导入片段测试（kernel 与 i18n 除外） |
| 脚本 | `scripts/` 9 + `deploy/local/` 6 | 7 + 4 | `cleanup-inventory.sh`（一行 shell 即可）、`rehearse-lightweight.sh`（`lightweight_test.go` 已证明） |

保留的宿主测试族：`accepted_*`（原子提交/崩溃）、`quarantine`/`deploy_recovery`/`journal_*`/`lightweight`/`oidc`/`deploy_admin`（持久与身份）、`tenancy_*`/`tenants`/`upgrade_authorization`/`submit_tenant`/`build_access`（隔离与授权）、`environment_*`/`release_install`/`direct_install`/`joint_draft`（生命周期）、`api`/`languages`/`definitions`/`host`（契约）、`console_compat`/`core_seed_replay`/`legacy_agent_context`（重放兼容）、`build`/`build_actions`/`build_approval`/`build_function`/`build_query`/`process`/`flow`/`function`/`agent*`/`ai`/`review_ai`（租户定义能力的核心行为）、外部契约与织物（`effects`/`writebacks`/`requests`/`sources_profiles`/`mcp`/`a2a`/`mail`/`io`/`files`）。

独立模块保留协议契约、授权/只读预览、发布隔离与 `CheckReplay`；酒店删除查询/关系页面装配/代码页面候选三份重复测试，制造删除查询测试及代码页面候选用例。ERP 的五份测试均保留，其证据涉及平衡记账、连续编号、原子接受与协议恢复。

浏览器 `routes` 中四条旧逐功能流程（29/30/31/34）随本轮删除：对象表单权限与状态由 `business-access`、页面编辑和冻结由 `page-notice`、整应用交付由 `workspaces`、原生流程版本绑定由 `workflow` 守住。现行路线不再断言已退场的 Studio 或 Direct install 控件。保留流程路线发现并修复了共享 Workbench 的真实断路：dock 页签变化须通知 owner，owner 主动切换 Test/Release 时须同步打开对应面板；隔离样本不进入生产、候选绑定及普通成员答复仍由这条路线验证。

### 规则（并入 docs/Testing.md）

- 新功能不自带新测试，除非它改变了上面某个不变量；改了不变量，改那条已有测试。
- 浏览器路线按实际任务保留，重叠责任合并，临时过程检查删除；不为固定数量删除仍有独立责任的路线。原“8 条封顶、加一删一”已由 ADR-0064 的按变更选择检查原则取代。
- 过程中的探针写在本地，不提交。

### 后果

宿主根包沙箱实测 76 秒 → 22 秒；`make e2e` 保留 8 个 spec（13 个测试用例（含两行业参数化路线））。完整发布验证的耗时仍取决于契约、形式化证明与部署演练，不由本轮删除推断。删掉的测试在 Git 历史里，需要重现某个旧场景时按文件名找回，而不是常驻。
