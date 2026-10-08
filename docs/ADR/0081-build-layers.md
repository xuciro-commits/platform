# ADR-0081 构建分层：本地开发、提交前检查、发布

状态：已接受（2026-10-08）。

## 背景

`scripts/verify.sh all` 把契约、Lean 证明、宿主、应用、Web 构建和浏览器路线串成一条几十分钟的链；GitHub Actions 在每次 push 上跑同一条链，五六百次运行几乎全部失败，没有人看。本地看代码要重打 Docker 镜像；e2e 有 140 个单行压缩的 spec，每个各自造对象、页面、发布与运行时。

## 决定

1. **一个入口，三层。** 仓库根 `Makefile` 是唯一入口（`make help`）；`scripts/verify.sh` 只保留具名步骤体，不再有 `all`/`ci`。
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
