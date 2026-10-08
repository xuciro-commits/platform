# ADR-0081 构建分层：本地开发、提交前检查、发布

状态：构建分层与本地模式已实施（2026-10-08）；GitHub Release/GHCR 发布仍未实跑。

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
