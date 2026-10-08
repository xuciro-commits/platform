# ADR-0049：两种基础设施 profile——交付环境与轻量环境（已实施）

**状态：** 接受并实施，2026-10-05。负责人已给出方向：平台基础设施做两种 profile——(a) 交付/客户：Docker + PostgreSQL + RustFS(S3) + Wasm Worker（wazero 进程内）；(b) 轻量/边缘小设备：SQLite + Wasm/Rust/Go 单二进制 + 自研轻量 IdP 身份服务。本文按“先设计再实施”的要求给出设计、边界与实施切片；D1–D7 按推荐落地，S1–S3 已实施并验证（同一套契约测试在文件与真实 PostgreSQL 上通过，真实二进制在单个数据目录上完成停止/重启续接），S4（SQLite）按 D2 明确为**后继后端**：接口已就绪，等依赖方案，不阻塞本稿结论。
**范围：** 运行环境的存储、身份、文件与 worker 选择。不改变 M1–M3 的入口、发布主人与恢复语义（ADR-0047/0048），不引入第二套部署状态机。
**设计日期：** 2026-10-05。代码事实基线：`a540d93e`（D6 第一批）与其上的 `main`；实施基线 `8f1a663`。

## 1. 背景：今天的交付环境靠什么运行

- 一个 host 二进制（`capabilities/server`，各 solution 的 `cmd/*-server`）通过 `Deployment` 参数运行（`deploy.go`）：`-database` 是 PostgreSQL 日志（空 = 内存），`-files` 是 S3 兼容存储（空 = 内存），`-oidc-issuer/-oidc-keys` 是外部 OIDC（空 = 开发 token），快照按 `-snapshot-every` 写入同一存储（ADR-0019）。Wasm 由进程内 wazero 执行（ADR-0044），没有外部 worker 进程。
- 因此“交付/客户”profile 就是今天的路径：Docker Compose 起 PostgreSQL 与 RustFS，OIDC 由 Rauthy 提供（`deploy/local/compose.yaml`），host 进程连接它们。
- 小设备上这套组合太重：需要 Docker/Compose、一个 PostgreSQL 实例、一个对象存储与一个外部 IdP。轻量 profile 的目标是**同一语义的单二进制**：一个进程 + 一个本地数据目录。

## 2. 决定与落地

| 决定 | 落地结果 |
|---|---|
| D1 profile 是显式声明，不是隐式降级 | `-profile delivery\|lightweight`（默认 `delivery`）。`Deployment.validate()` 在打开任何存储前结算：轻量必须有 `-data`，且拒绝 `-database/-files/-oidc-issuer/-oidc-keys/-project`；交付拒绝 `-data/-idp-key`；未知 profile 直接失败。不回退、不混用 |
| D2 轻量持久化 | `Journals` 接口（`journals.go`）抽出读/追加/快照/幂等索引，PostgreSQL 是既有实现；`journal_file.go` 是同语义的单文件后端（`journal.jsonl` + `snapshots/` + `derived/`，纯标准库，无新依赖）。SQLite 作为后继后端（同一接口）暂缓，等依赖方案 |
| D3 身份 | 交付沿用外部 OIDC/开发 token；轻量用内置 `LocalIdP`（HS256，密钥 `-idp-key`，默认 `<data>/idp.key`）：`-idp-new-key` 只生成一次、绝不替换，`-mint-token <subject>` 打印 token（`<user:email>`/`<client:id>`），`-token-ttl` 默认 12h。席位目录仍是 `-directory`/内置 `Seat`，角色模型与审计不变 |
| D4 文件字节 | `FileStore` 接口不变（ADR-0028）：交付用 S3（`-files`），轻量用 `localFiles`（`<data>/files/<tenant>/<hash>`，内容寻址、原子写、拒绝越界 key）。日志里仍只记哈希 |
| D5 Worker | 两个 profile 都用进程内 wazero（ADR-0044），不引入外部 worker |
| D6 诊断与恢复 | 两种 profile 走**同一个** `restoreTenants`：读本代码的最新快照、重放其后的条目、损坏日志隔离、启动即 `attachJournal`。profile 差异只在存储/身份/文件的位置 |
| D7 不做 | 没有第二个发布系统/激活指针、没有应用私有恢复、没有“轻量版少一半语义” |

## 3. 实施切片与验收

1. **S1 存储接口 + 单文件后端（已实施）**：`Journals` 接口 + `FileJournal`。追加前必读、按 seq 单调（文件损坏即 `errTenantJournal`）、同租户单写者、接受结果按 `app|scope|key` 提交一次（重放同一请求返回同一答案，换请求即冲突）、快照只留本代码的两份并支持 `RepairSnapshot`、向量与转写随日志重启存活。
   验收：`capabilities/server/journal_file_test.go` 的同一套契约测试在**文件**与**真实 PostgreSQL**（`PLATFORM_TEST_DATABASE`）两种后端上通过。
2. **S2 本地文件字节（已实施）**：`localFiles` 与 `memoryFiles`/S3 同契约（`files_local.go`）：同一内容寻址 key、同一字节、重复写同一内容不是错误、越界 key 被拒绝而不是被清洗。
   验收：`lightweight_test.go` 的 `TestLocalFilesKeepsTheSameContract`。
3. **S3 内置轻量 IdP（已实施）**：`LocalIdP` 签发/校验（常量时间比较、拒绝非 HS256、校验 `iss`/`exp`/未来 `iat`），`Host.SignWith(idp, ttl)` 一次替换认证函数并签 `/v1/sign-in` 的服务席位，`Deployment.lightweightState()` 把日志、文件字节与密钥都开在 `-data` 里。
   验收：`TestLocalIdPSignsAndVerifies`、`TestLightweightProfileServesSignedSeats`、`TestLightweightProfileIsDeclaredNotInferred`（三种身份来源下 `/v1/me`、拒绝与席位一致：轻量宿主只收自己签的 token，交付宿主的开发 token 在这里 401，另一台宿主的 token 也 401）。
4. **S4 SQLite 后端（暂缓）**：D2 的推荐是“先落地不引入依赖即可运行的轻量后端；SQLite 作为后继（同一接口）”。本批没有网络/模块缓存可取回驱动，故不引入；接口已就绪，接入时跑同一套契约测试即可。
5. **轻量重启与身份契约**：文件日志、快照恢复、幂等和自签登录由 `lightweight_test.go` 与日志契约测试守住；重复演练脚本按 ADR-0082 删除。检查入口：`make test-go RUN=Lightweight`。

## 4. 证据与检查（2026-10-05，本沙箱）

- 真实 PostgreSQL：`pgserver`（PyPI，PostgreSQL 16.2，Unix socket）上跑契约测试，`file` 与 `postgres` 两个子测试全绿；快照、幂等冲突、派生数据与重启续接都在真实日志上验证。
- 真实二进制：`hospitality-server`（`-profile lightweight -data <dir>`）在同一台沙箱上完成两次启动的走查，日志给出 `restored hotel-a from the snapshot at N, then replayed M entries`，`/v1/me` 的 `preferred` 与决策身份跨重启不变。
- 轻量 profile 的操作路径（`deploy/local/README.md` 有完整段落）：
  ```bash
  go build -o /tmp/hospitality-server ./solutions/hospitality/cmd/hospitality-server
  /tmp/hospitality-server -profile lightweight -data /var/lib/platform -idp-new-key   # 只做一次
  TOKEN=$(/tmp/hospitality-server -profile lightweight -data /var/lib/platform -mint-token user:ops@example.com)
  /tmp/hospitality-server -profile lightweight -data /var/lib/platform -addr 127.0.0.1:8496
  ```
- 检查按 WorkQueue：编码中只查类型/构建；整块完成后一次 Go 检查与重启走查；不把 CI 作为门槛。

## 5. 实施中发现并修掉的问题（供后续 profile 边界参考）

- **恢复路径曾被 profile 分叉**：`Serve` 最初只在 `-database` 分支里做快照恢复与重放，轻量分支只打开日志就继续——重启会得到空租户。现已抽成 `restoreTenants`，两种 profile 共用（D6）。
- **认证函数曾在 `NewHost` 之后才替换**：轻量宿主因此仍收开发 token（subject 原样当凭据），却拒绝自己签发的 token。现由 `Host.SignWith(idp, ttl)` 同时设置认证与签发，二者不可能再错位；`TestLightweightProfileServesSignedSeats` 断言未签名 token 必须返回 401。
- **短命 CLI 的退出码**：`-idp-new-key`/`-mint-token` 走的是 host 的 `main`，原先 `log.Fatal(err)` 会把成功当失败（打印 `<nil>` 并退出 1）。已改为主程序只在 `err != nil` 时 `log.Fatal`（9 个 `cmd/*-server` 同改），并把 token 打给 stdout、日志给 stderr，便于脚本取用。

## 6. 关系

本稿只决定运行环境的存储/身份/文件/worker 选择；ADR-0007/0010（部署与席位）、ADR-0018（Web 构建）、ADR-0019（日志与快照）、ADR-0027（遥测）、ADR-0038（接受结果与恢复）、ADR-0044（能力装配与 wazero）与 ADR-0047/0048（入口、组织、发布）保持不变。轻量 profile 是同一语义的另一组后端，不是第二个平台。
