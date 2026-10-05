# ADR-0049：两种基础设施 profile——交付环境与轻量环境（提案）

**状态：** 提案，2026-10-05。负责人已给出方向：平台基础设施做两种 profile——(a) 交付/客户：Docker + PostgreSQL + RustFS(S3) + Wasm Worker（wazero 进程内）；(b) 轻量/边缘小设备：SQLite + Wasm/Rust/Go 单二进制 + 自研轻量 IdP 身份服务。本文按“先设计再实施”的要求给出设计、边界与实施切片。
**范围：** 运行环境的存储、身份、文件与 worker 选择。不改变 M1–M3 的入口、发布主人与恢复语义（ADR-0047/0048），不引入第二套部署状态机。
**设计日期：** 2026-10-05。代码事实基线：`a540d93e`（D6 第一批）与其上的 `main`。

## 1. 背景：今天的交付环境靠什么运行

- 一个 host 二进制（`capabilities/server`，各 solution 的 `cmd/*-server`）通过 `Deployment` 参数运行（`deploy.go`）：`-database` 是 PostgreSQL 日志（空 = 内存），`-files` 是 S3 兼容存储（空 = 内存），`-oidc-issuer/-oidc-keys` 是外部 OIDC（空 = 开发 token），快照按 `-snapshot-every` 写入同一 PostgreSQL（ADR-0019）。Wasm 由进程内 wazero 执行（ADR-0044），没有外部 worker 进程。
- 因此“交付/客户”profile 就是今天的路径：Docker Compose 起 PostgreSQL 与 RustFS，OIDC 由 Rauthy 提供（`deploy/local/compose.yaml`），host 进程连接它们。
- 小设备上这套组合太重：需要 Docker/Compose、一个 PostgreSQL 实例、一个对象存储与一个外部 IdP。轻量 profile 的目标是**同一语义的单二进制**：一个进程 + 一个本地数据目录。

## 2. 待决决定（建议逐条答复“同意 / 改为 …”）

| 决定 | 推荐 | 理由与边界 |
|---|---|---|
| D1 profile 是显式声明，不是隐式降级 | 由启动参数声明 profile（如 `-profile delivery|lightweight`），两个 profile 的默认值分别固定；缺少依赖时失败退出，不回退到另一个 profile 的存储或身份 | 与 ADR-0019/0038 的“fail-stop、一个权威”一致；避免“看起来在跑、其实没有持久化” |
| D2 轻量持久化 | 顺序：先做**交付必需的存储接口抽象 + 单文件日志后端**（纯标准库：追加日志 + 快照，保持 `tenant/seq/kind/body/at` 与“先读后追加”、幂等键唯一索引等既有 journal 语义），SQLite 作为后继后端（同一接口） | 本沙箱与无网环境无法取回新的 Go 依赖（无模块缓存），SQLite 驱动需先定 vendor/引入方式；先落地不引入依赖即可运行的轻量后端 |
| D3 身份 | 交付沿用外部 OIDC；轻量用**内置轻量 IdP**：签名 token（HMAC/EdDSA，密钥存数据目录）+ 租户席位目录文件（现状 `Seat`/成员角色不变），开发 token 仍只在开发 profile | M3 不新增角色（ADR-0048 D7）；身份来源不同，权限模型与审计不变 |
| D4 文件字节 | 交付用 S3 兼容（现状）；轻量用本地目录（同一 `FileStore` 接口与校验和/内容寻址语义） | 文件的可读性与版本不变，只换位置 |
| D5 Worker | 两个 profile 都用进程内 wazero（ADR-0044），不引入外部 worker | 编译/运行契约不变（ADR-0048 D8） |
| D6 诊断与恢复 | 两种 profile 都必须满足 M4：重启后按原边界继续、失败任务可定位到原运行/原因、只执行获权恢复。profile 差异只允许出现在存储/身份/文件的位置 | 不新建运维系统或全入口故障矩阵（ADR-0047 §10.2） |
| D7 不做 | 不做第二个发布系统/激活指针、不做应用私有恢复、不做“轻量版少一半语义” | 与 ADR-0048 §3 一致 |

## 3. 实施切片

1. **S1 存储接口 + 单文件后端**：把 `Journal` 的读/追加/快照/幂等索引抽成接口，PostgreSQL 为现有实现；新增本地目录后端（`journal.jsonl` + `snapshots/`，追加前必读、按 seq 单调、同租户单写者）。验收：同一套 Go 测试在两种后端上通过（含 `PLATFORM_TEST_DATABASE` 的 PostgreSQL 路径）。
2. **S2 本地文件字节**：本地目录实现的 `FileStore`（或确认现有内存实现是否足够），与 S3 实现共用测试。
3. **S3 内置轻量 IdP**：签发/校验 token + 席位目录，替换 `-oidc-issuer`；验收：开发 token、OIDC 与轻量 IdP 三种来源下，`/v1/me`、权限拒绝与审计一致。
4. **S4 SQLite 后端（待依赖方案）**：若负责人接受引入驱动（vendor 或替换为仓库内引擎），在 S1 接口上实现并跑同一套测试。
5. **每个切片都要有一次重启走查**：固定数据目录 → 启动 → 做工作 → 停止 → 启动 → 记录仍可读、可继续写入（M4）。

## 4. 证据与检查

- 现已具备的固定持久环境证据（2026-10-05，本沙箱）：
  - 用 `pgserver`（PyPI，PostgreSQL 16.2，Unix socket）建立真实日志，`hospitality-server -database …` 启动、写入对象/记录、停止（快照写入）、重启（`restored hotel-a from the snapshot at N, then replayed 0 entries`），记录与定义原样可读；
  - 整个 `capabilities/server` 测试套件带 `PLATFORM_TEST_DATABASE` 在真实 PostgreSQL 上全绿（含联合草稿、production profile 拒绝、回放恢复），只余一处测试精度修正（见 §5）。
- 检查按 WorkQueue：编码中只查类型/构建；整块完成后一次 web 检查、Go 检查与重启走查；不把 CI 作为门槛。

## 5. 已知边界与发现（2026-10-05）

- `deploy/local/rehearse.sh` 仍需要 Docker（本沙箱没有），其“delivery profile 断言”未执行；S1–S3 落地后应能在无 Docker 的轻量 profile 上重复同一走查。
- 真实 PostgreSQL 暴露了两处测试侧问题，已在本批修正：直接追加前必须先读日志（与部署一致），以及测试决策时间需取日志精度（`timestamptz` 微秒），否则“重放租户”与在线租户的序列化状态差在亚微秒。
- 平台各处的时间比较已有 `sameJournalTime` 先例；本 ADR 不改变该语义，只要求轻量后端与 PostgreSQL 精度一致。

## 6. 关系

本稿只决定运行环境的存储/身份/文件/worker 选择；ADR-0007/0010（部署与席位）、ADR-0018（Web 构建）、ADR-0019（日志与快照）、ADR-0027（遥测）、ADR-0038（接受结果与恢复）、ADR-0044（能力装配与 wazero）与 ADR-0047/0048（入口、组织、发布）保持不变。轻量 profile 是同一语义的另一组后端，不是第二个平台。
