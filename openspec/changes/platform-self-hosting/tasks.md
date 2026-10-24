# Tasks

> 落实情况见 [WorkQueue](../../../docs/WorkQueue.md) 与 `docs/Architecture.md` 第 9 节。未勾选项是未做，不是"待补勾"。

## 1. 删除无关设计与代码

- [x] 1.1 删除 catalog（`web/apps/catalog`、`web/packages/catalog`、`scripts/catalog*`、`build/src/catalog*`）、Lean 形式化（`contract/lean`、`scripts/formal.sh`、`cmd/uaf-gen`、`docs/standards/uaf`）、行业包与 solution 宿主（`deploy/packages`、行业 e2e 与 fixture）、服务端与 Web 的跟随测试、`deploy/templates` 中的行业模板。
- [x] 1.2 `deploy/local/lite.sh` 与 `make lite` 改为单一宿主，不再传已删除的 `-packages deploy/packages`，也不再按 solution 名分支；`deploy/dev/run.sh` 同步去掉该参数。
- [x] 1.3a 删除协议网关与驱动执行栈：`gateway/`、`drivers/{modbus,s7,simulator}`、`driver/`、`cmd/{gateway,driver-modbus,driver-s7,driver-simulator,webhook-sink}`、`scripts/package-artifacts.py`；包索引改为自己校验制品摘要。删除后重建并换版成功（release 0006）。
- [x] 1.3b 删除 `build` 的 source、connection、dataset、pipeline、writeback、match、observation、dispatch、confirmation 及对应 Web 编辑器。**2026-10-24 完成**：9 个 build 文件、6 个根包文件与整套装配（`build.go`、`activation.go`、`release*.go`、`joint_release.go`、事务引擎的 effect/dispatch 步骤）已删除；`go build ./... && go vet ./...` 绿，`go test .`（自举验收）通过。
- [ ] 1.4 删除知识、文件、关系、企业建模与专业可视化：`apps/{knowledge,files,relations}`、UAF 元素/关系/视图/租户链接、`ui/src/spatial`、三维/地图/标注组件。**实测卡点（2026-10-24）**：删 `ui/src/spatial` 后 typecheck 只有 8 个错，但都落在 `widgets/RecordCollaboration.tsx` —— 这个渲染器同时承载评论、上传、媒体预览、PDF 与标注/三维（`collaboration-plugins.ts` 六个 widget 共用一个组件），所以删空间组件要先把它拆开；`surface-plugins.ts` 的 `record-map` 与页面模型的 `map`/`scene` 段来自契约（`packages/kernel/src/gen/host.ts`），删字段要重生成。`apps/{knowledge,files,relations}` 另有 7.5c 的重放阻断。三件事都是重构，不是删除。
- [x] 1.5 精简树通过检查：`cd capabilities/server && go build ./... && go vet ./...` 全绿，`go test ./...` 只剩保留集（`platformserver` 自举验收 0.84s 通过、`internal/systemscope`）。Web 的 `pnpm -r typecheck` 待前端重构完成后重跑。

## 2. system 作用域与角色

- [x] 2.1 宿主级唯一 `system` 租户：`tenants.go` 的 `systemSpec` 装配，不可由 Console 创建或删除；启动、恢复与健康检查按普通租户处理。
- [x] 2.2 非 system 作用域提交系统资产被拒绝（`Only the system tenant`），并有回归用例。
- [x] 2.3 角色：`system` 沿用 builder/publisher 语义；智能体 `system.architect` 只有 `file.save` 与 `change.propose`，不能自批。
- 2.4 删除（不做）：与 `specs/platform/self-hosting` 的「非 system 作用域的界面不出现该应用入口」冲突；业务租户按设计看不到 system 元对象，投影会制造第二套可见性规则。

## 3. 系统记录与内核闸门

- [x] 3.1 `system.file` + `system.blob`：路径唯一、内容寻址、修订检查、删除墓碑；复用日志与文件存储。
- [x] 3.2 `system.change`：来源、提示词、冻结文件清单、批准、诊断、失败状态；批准后清单不可改。
- [x] 3.3 `system.release` 与 `system.activation`：不可变发布、二进制/前端/工具链摘要、上一版本、可取消窗口；同一时刻只有一个未过期的 `requested` 激活（过期的请求不再阻塞下一次决定），激活成功时旧激活标 `superseded`。
- [x] 3.4 `internal/systemscope`：保存前与构建前各执行一次，冻结契约、提交入口、接受结果、重放、快照、日志、内部格式、授权与身份提供方，拒绝 `.git`、绝对路径、`..`、密钥。
- [x] 3.5 表驱动用例：内核闸门、修订冲突、同内容单副本、重放不重建、窗口取消、自批拒绝。

## 4. updater 与构建激活

- [x] 4.1 `cmd/platform-updater`：`build`、`activate`、`swap`、`finish`、`serve`、`watch`。用 publisher 身份与宿主既有提交接口读写 system 作用域，不另建旁路。
- [x] 4.2 物化与固定构建：从运行中的代码树播种到 `.build/system/work/<时间>/`，叠加变更文件，执行固定的 `go build ./cmd/platform-server`、`vite build` 与规则引擎编译；不执行 Makefile 或 `scripts/`。
- [x] 4.3 体检、排空与切换：候选在临时数据目录探测 `/healthz.release`，通过才写 `current.json`；监督进程探活、排空、换版，失败回滚并记录 `rolled-back`。
- [x] 4.4 版本保留：`.build/system/{bin,web,releases}` 只保留最近 5 个，正在运行与可回滚的发布永不清理；release 0009 构建时 0001–0004 被清掉，0005–0009 留下。
- [x] 4.5 `/healthz` 报告 `release` 与 `releaseDigest`；工作区资源随发布目录切换。
- [x] 4.6 从发布清单导出完整工作树：`platform-updater export` / `make system-export RELEASE=<n>` 读 `releases/<n>.json` 记录的构建工作树，逐个校验冻结文件的 sha256 后复制到 `.build/system/exports/<n>/`；release 0015 导出 1443 个文件、2 个冻结文件校验通过，导出树可独立编译出宿主。

## 5. AI 入口

- [x] 5.1 `system.architect` 智能体：只允许 `system.file.save` 与 `system.change.propose`，模型调用、预算与运行日志沿用 `apps/ai` 与 `agent.run`。
- [x] 5.2 提示词运行产出 `origin=prompt` 的变更，记录提示词、理由与文件清单。
- [x] 5.3 只有 publisher 可批准，提案主体不能自批（有回归用例）。
- [x] 5.4 禁区：内核路径在保存与构建两处被拒；智能体没有构建、激活或读取凭据的动作。
- [ ] 5.5 在真实模型提供方上跑一次提示词演练（本机沙箱只放行 github/npm/pypi，没有模型凭据，未观察）。AI 入口的代码路径由 `system.architect` 与 `system_test.go` 覆盖；缺的是真模型那一次的观测记录，不是实现。

## 6. 人工入口

- [x] 6.1 `web/packages/platform/src/system.tsx`：changes、files、releases 三页，只在成员持有 `system` 角色时出现。
- [x] 6.2 变更视图：来源、冻结文件、诊断、批准、激活、窗口内取消。
- [x] 6.3 源码编辑：文件列表 + 修订号 + 保存/删除，冲突由宿主拒绝并在界面上报。
- [x] 6.4 双语键（`packages/platform/src/i18n.ts`）。
- [x] 6.5 浏览器入口可用：宿主以 `-tenant system -admin user:admin@example.test` 启动时，开发席位持有 `system.publisher`，工作区列出 Platform 的三页；构建产物 `web/0013` 里能找到这些页面。完整点击演练仍待负责人走查。
- 6.6 删除（不做）：同上，切换入口等于在业务租户里露出 system 作用域；需要时以 system 席位另开会话即可。

## 7. 验收与文档

- [x] 7.1 演练一（人改代码）：`chg-drill-6` 冻结两个 TypeScript 文件 → release 0004 → 激活 → `/healthz` 报 4，工作区服务 `web/0004`。
- [x] 7.2 演练二（提示词来源）：`chg-drill-8/10` 以 `origin=prompt` 提出 → 批准 → `watch` 自动构建 release 0005 → 激活 → `/healthz` 报 5 与二进制 sha256。
- [x] 7.3 演练三（失败不发布）：`chg-broken` 编译失败 → 变更 `failed`、无 release；`chg-drill-4` 因缺少工具链失败并记录原因。
- [x] 7.3a 无终端演练：批准 `chg-drill-26`（origin=prompt）后只运行 `platform-updater watch -once`，它完成构建（release 0017）与换版，监督进程把激活记成 `active`、0016 记成 `superseded`，`/healthz` 报 `release:17`。
- [x] 7.3b 闭环演练：监督进程用 `serve -watch` 启动后，批准 `chg-drill-29` 五分钟内无人干预地构建出 release 0019；随后发出 `system.release.activate 0019`，监督进程自行探活、排空、换版并回报，`/healthz` 报 `release:19`，激活 0019 = `active`、0018 = `superseded`。
- [x] 7.4 演练四（取消窗口）：`chg-drill-3` 激活后取消 → 激活 `aborted`、发布回到 `published`，运行版本不变。
- [x] 7.5 注入启动失败：`chg-drill-12` 构建出启动即退出的 release 0007，`swap` 探活失败拒绝安装，`current.json` 与 `/healthz` 仍是 6；随后取消激活，0007 回到 `published`。
- [x] 7.5a 监督进程把三处缺口补上：候选必须 `status=ok` 且没有隔离租户才算健康；回滚时把 `current.json` 改回真正在服务的发布，避免每秒重试坏版本；换版前等待旧监听退出，避免新旧进程抢端口。
- [x] 7.5b 注入数据不兼容：release 0010（删掉 files/relations 的树）能通过临时目录探活，但在真实数据上隔离了 system 租户；监督进程按 `status != ok` 判定失败、回滚 0009，并把 `current.json` 改回 0009，健康恢复且不再反复重试。
- 7.5c（记录，不阻塞）删除 `files`/`relations`/`knowledge` 三个应用会让已有租户无法重放（`accepted observation needs missing projection relations`），必须先有退役与迁移路径；本次已回退这三处删除，仅保留 gateway/驱动/协议二进制的删除。
- [x] 7.6 文档：`docs/Architecture.md` 第 9 节、`docs/WorkQueue.md`、`AGENTS.md` 硬规则 8 与检查入口、`openspec/README.md`。
- [ ] 7.7 归档：`make openspec ARGS='validate platform-self-hosting --strict'` 已通过（2026-10-24）；归档等 1.3/1.4 要么完成、要么作为独立 change 移出本变更的范围内。

## Workflow follow-up

- 删除过程中 `scripts/boundaries.sh`、`scripts/escapes.sh` 的允许清单只减不增。
- 自举上线后不以测试数量或代码行数作为交付证据；以演练与真实操作结果判断。
