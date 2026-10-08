# 交接：引用字段选择器 + 构建分层（2026-10-08）

给集成代理（GPT）。按 AGENTS.md 规则 6 按路径集成到 main，不 merge。

- **基点 B** = main `8121634`（分支 `c873645` 与之同内容）
- **冻结 HEAD** = `8ff18f7`（分支 `arena/0af55203-platform`）
- 路径清单 = `git diff --name-only 8121634 8ff18f7`（两波共 33 个文件，全部只在分支侧改动；无 `host.ts`/Catalog 生成物变化）

## 第一波 `33c6f8f`：引用字段变成选择器

`web/packages/app/src/semantic/References.tsx`（新，已从 `@platform/app` 导出）：`AppSelect`、`RoleSelect({app})`、`ProtocolSelect`、`MemberSelect({agents})`、`useTenantApps()`；数据来自 `/v1/apps`、`/v1/protocols`、`/v1/members`+`/v1/agents`，当前值不在候选中时保留为额外选项。替换点：

| 文件 | 字段 |
|---|---|
| `platform/src/account.tsx` | Home page → 可打开应用的 Select（自己：`me.apps`；管理员改他人：该成员角色键）；表单对齐 `content-start`/`self-start` |
| `platform/src/ai.tsx` | 限额 Member ID → `MemberSelect agents` |
| `build/src/automate/workflow-inspector.tsx` | Protocol → `ProtocolSelect`；Recipient role → `RoleSelect app="build"` |
| `build/src/ontology/process.tsx` | Role in the builder app → `RoleSelect app="build"` |
| `build/src/releases/simulate.tsx` | Capability owner → `AppSelect` |
| `build/src/ontology/pipeline.tsx` | Root element / Placed in kind → `/v1/enterprise` 的元素与关系类型 Select |
| `platform/src/enterprise/index.tsx` | 元素 Kind → 该构型 profile `kinds` 的 datalist |
| `build/src/functions/agent.tsx` | About record → 对象 Select + 记录 id |

三包 i18n 已补 zh-CN。本地请做：`make check-web`，再在 8495 浏览器看 My account 的 Home page 下拉、AI 限额的成员下拉、流程检查器的协议/角色下拉。

## 第二波 `8ff18f7`：构建分层（ADR-0081）

1. **`Makefile` 是唯一入口**（`make help`）。三层：
   - dev：`make setup`（一次）→ `make infra` → `make dev SOLUTION=hospitality|manufacturing`（air 热重启，配置 `deploy/dev/air.toml`，启动参数 `deploy/dev/run.sh`，与 Compose 宿主同一 PostgreSQL/Rauthy/RustFS/tenants.json）→ `make web`（Vite，`/v1` 代理）。`make dev-light` 不要容器。`make infra-compute` 把 worker/builder socket 绑到 `.build/dev/compute`。
   - check：`make check`（分钟级，无浏览器无容器）、`make test`、`make test-go PKG= RUN=`、`make e2e SPEC=`。
   - release：`make verify`（原 `verify.sh ci`+web-check）、`make build`、`make images`、`make rehearse`、`make tag VERSION=vX.Y.Z`。
2. **CI**：`.github/workflows/verify.yml`（已整体注释的死文件）删除；新 `release.yml` 只在 `v*` 标签/手动触发：`make verify` → 工作台 tar + linux amd64/arm64 两个宿主二进制 + SHA256SUMS 挂到 GitHub Release，镜像推 GHCR。日常 push 不再跑任何工作流。
3. **删除**：`deploy/local/rehearse-lite.sh`（`rehearse.sh` 子集）；`scripts/verify.sh` 去掉 `all`/`ci`，新增 `web-types`、`composition-static` 步骤供 Makefile 组合。
4. **测试规范**（`docs/Testing.md`「测试规范」）：
   - 浏览器：`web/e2e/tests/kit.ts`（`test` 扩展出 `builder`/`operator`/`editor`；`builder.object/records/page/edit`，`editor.importModule/select/saveUntil/release`，`runtime()`，`shots()`）。样板 `page-notice.spec.ts` 已按规范改写。
   - 宿主：`capabilities/server/testkit_test.go`（`seatOf`、`composeTenant`、`builderTenant`、`memberOf`、`decide`、`refuse`、`publishObject`）。样板 `build_access_test.go` 已迁移并通过。

### 请在本地落地与核对

- `make setup && make infra && make dev` + `make web`：确认 air 监听 `capabilities/server`/`apps`/`solutions` 改动后重启、Rauthy 登录通过（issuer `http://localhost:8480/auth/v1/`）、工作台 5176 正常；`make dev-light` 用 `manager` 令牌进入。沙箱里无 Docker/air，这两条**未实跑**，参数照抄自 `compose.yaml`，有偏差直接改 `deploy/dev/run.sh`。
- `make check`、`make test`、`make e2e SPEC=page-notice`（样板 spec 未在浏览器跑过）。
- `release.yml` 未在 GitHub 上跑过：首个 `make tag VERSION=v0.1.0`（或 `workflow_dispatch`）后看一遍；GHCR 推送需要仓库开启 packages 权限。
- 旧的 ~140 个单行压缩 e2e spec 和各 `*_test.go` 里的 `submit := func` 闭包：**触碰时迁移**到 kit，不单独立项（规范已写明）。若你愿意批量做，优先 `page-*.spec.ts`（模式与 `page-notice` 完全一致：object → sampleModule → page → importModule → inspector → saveUntil → release → runtime）。
- 两台验收宿主更新仍是 `make local-update`（`deploy/local/update.sh`）。

### 已知与边界

- 沙箱里 `scripts/boundaries.sh` 因 `apps/*/server` 未 vendor 而失败（基线即如此），本地正常。
- `docs/Testing.md:104` 关于全量 e2e 未通过的记录保留，未动。
- 历史 ADR 中的 `scripts/verify.sh ci` 字样是记录，不改。
