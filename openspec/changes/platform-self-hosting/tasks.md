# Tasks

2026-10-27 完成规划，随后负责人明确授权直接实施。2026-10-28 已落实系统新写冻结、静态维护入口及原组织owner内聚；组织/UAF与运营入口本批已完成代码修改，统一验收前相关项仍保持未勾选；11.3的实际旧请求核对不能以文件缺失代替。 新范围以 [proposal](proposal.md)、[design](design.md) 为准，项目顺序仍仅归 [WorkQueue](../../../docs/WorkQueue.md)。旧全量自举已经实现，冻结和退出是新增工作，不能把规划通过当作代码已关闭。

## 11. 静态禁区与原自举冻结（首批实施）

- [x] 11.1 在原 system owner、AI 工具和直接 API 同批拒绝新的源码保存、提案、批准、构建与定义/代码激活，停止 updater 消费新动态工作；用原动作级回归证明 UI 隐藏之外也不可调用，正常业务应用发布仍可用。
- [x] 11.2 保留静态租户、IdP、凭据、模型配置、基础实现及维护入口，并切断其对动态 Page/App 的依赖；用元数据缺失/损坏和 system 隔离的实际路线验证原登录、健康、获权维护仍可达，更新部署指南与禁区说明。
- [ ] 11.3 核对并交接已有 requested/unknown 激活及 updater 未回报结果；用原 ID/键、实际版本与资源核验终态，验证保留 current.json、卷、密钥、源码、制品及回滚前驱，不以清理代替恢复。
- [x] 11.4 保留 system 原封存格式、定义安装映像和只读诊断，删除已替代的普通写路径；跑针对性 owner 重放/快照及真实 PostgreSQL 恢复验证，记录跳过项，并同步该批 Architecture 与维护文档。

## 12. UAF 外移与原组织权威收敛

- [x] 12.1 按 design 的 19 文件映射迁移同一 enterprise owner 至 engine/organization，合并纯别名和转发；证明 authority ID、记录身份、当前权限和接受状态不变，组织有效期、关闭/重开、Units/Below/Holders 及有界关系测试保持原语义。
- [ ] 12.2 从实际生命周期、端点和属性消费者提取轻量目录必要规则，退出完整 UAF 新写与新切片导入；用相同输入比较接受/拒绝，验证不能因去掉元模型而放宽错误端点、期间、属性或授权。
- [ ] 12.3 保留旧资源/能力/场所/视图/来源命名空间与 legacy grid 的原只读解释，逐项检查旧接受结果和旧输入恢复；不能兼容的旧输入先以保留二进制完成受检交接，否则阻止升级，验证历史字节及业务引用未改变。
- [ ] 12.4 将 design 列出的 13 个 UAF/生成器/标准文件连同来源、摘要、许可证和标准测试交付为独立可选工具，移出核心路径；核对原件逐字一致及默认 go list 依赖不再含完整 UAF/生成器，不虚报未测性能改善。
- [ ] 12.5 改接组织查询、引用、使用检查、来源和治理消费者，合并组织界面并退出完整 metamodel/属性/视图/切片编辑；重生成 host.ts、enterprise-sdk.ts 和 Catalog，执行适用 owner、Web 类型、引用/关闭状态路线及 composition 检查，同步该批主规范和 Architecture。
- [ ] 12.6 从 Makefile 默认检查撤掉 UAF 生成前置，更新命令清单与边界基线；在无标准原件/工具环境构建并验证轻量组织可用，报告实际净文件/行变化，不以减少文件数证明行为等价。

## 13. 有界运营记录

- [ ] 13.1 从原 audit、usage、work owner 选择只读投影和必要受控追加，不建立镜像日志或调度；验证当前权限、来源过滤、分页、追加幂等、失败及重放，任务控制只由静态入口提供。
- [ ] 13.2 将允许的运营动作范围焊入原发布与执行校验，拒绝动态页面、包、代码和工具绑定静态配置；以真实越界候选及直接调用验证拒绝，保证正常业务构建、字段值和权限不受影响。
- [ ] 13.3 在原工作区提供双语、简单的组织与运营入口，保留旧自举记录只读诊断；核对不再暴露 UAF 黑话或新全量源码写控件，并用真实浏览器验证读取失败和静态修复导航。
- [ ] 13.4 仅接入 aigateway 已交付且满足当前来源权限的蒸馏样本读取/追加能力；用原样本 owner 测试证明不泄漏凭据/不可读来源、不重复配对或另建存储；若 AI 战役未交付则本项保持未完成。

## 14. 跨模块验收

- [ ] 14.1 对冻结、组织迁移和运营读取的最终组合集中运行适用 make check、owner 测试、composition、真实 PostgreSQL 恢复及元数据损坏路线；说明自动检查、实际运行、负责人体验和未验证边界，核对原部署身份与数据连续。

## Workflow follow-up

- 所有本次任务完成并经负责人验收后，使用官方同步与归档技能处理规范；本轮规划不得提前同步主规范或归档。
- 全量源码自举的未来恢复不属于本计划；需要重新授权和独立变更。`consolidate-platform-boundaries` 负责其他三场战役和全仓守门基线。

## Historical implementation record

以下完整保留原已勾选记录及未闭合事实，只说明当时实施与演练，不代表当前工作树或新范围已经完成。早期整包删除、极简前端和整体能力退出后来已撤回/合并，当前 UAF、Knowledge、集成及完整 Web 仍有调用；旧文件/字节/性能数字和“下一刀”不作为今天的基线。原 7.7、9.12 的未完成内容保留为历史债务，不再要求继续扩大源码自举来补齐。

## 历史 1. 删除无关设计与代码（原范围已撤回）

- [x] 1.1 删除 catalog（`web/apps/catalog`、`web/packages/catalog`、`scripts/catalog*`、`build/src/catalog*`）、Lean 形式化（`contract/lean`、`scripts/formal.sh`、`cmd/uaf-gen`、`docs/standards/uaf`）、行业包与 solution 宿主（`deploy/packages`、行业 e2e 与 fixture）、服务端与 Web 的跟随测试、`deploy/templates` 中的行业模板。
- [x] 1.2 `deploy/local/lite.sh` 与 `make lite` 改为单一宿主，不再传已删除的 `-packages deploy/packages`，也不再按 solution 名分支；`deploy/dev/run.sh` 同步去掉该参数。
- [x] 1.3a 删除协议网关与驱动执行栈：`gateway/`、`drivers/{modbus,s7,simulator}`、`driver/`、`cmd/{gateway,driver-modbus,driver-s7,driver-simulator,webhook-sink}`、`scripts/package-artifacts.py`；包索引改为自己校验制品摘要。删除后重建并换版成功（release 0006）。
- [x] 1.3b 删除 `build` 的 source、connection、dataset、pipeline、writeback、match、observation、dispatch、confirmation 及对应 Web 编辑器。**2026-10-24 完成**：9 个 build 文件、6 个根包文件与整套装配（`build.go`、`activation.go`、`release*.go`、`joint_release.go`、事务引擎的 effect/dispatch 步骤）已删除；`go build ./... && go vet ./...` 绿，`go test .`（自举验收）通过。
- [x] 1.4 删除知识、企业建模与专业可视化：`apps/knowledge`、UAF 元素/关系/视图/租户链接、`ui/src/spatial`、三维/地图/标注组件。**2026-10-24 完成方式改为推倒重写**：旧前端 `web/packages/{build,app,ui}` 与 `web/apps/workspace/src` 整包删除，`spatial`、`RecordCollaboration`、`surface-plugins`、协作/上传/PDF/标注 widget 随宿主一起消失，不需要再拆分重构；新界面（`web/packages/platform`，见 6.7）不引用页面模型、`map`/`scene` 段或任何 widget 契约。
- 1.4a（移出本变更）`apps/{files,relations}` 保留：7.5b 的实测证明删掉这两个应用会让已有租户在真实数据上被隔离（`accepted observation needs missing projection relations`），退役需要先有迁移路径（7.5c）。两块合计 24 KB，删除收益小于重放破损的代价。
- [x] 1.5 精简树通过检查：`cd capabilities/server && go build ./... && go vet ./...` 全绿，`go test ./...` 只剩保留集（`platformserver` 自举验收 0.84s 通过、`internal/systemscope`）；Web 的 `pnpm -r typecheck` 与 `vite build` 在重写后重跑全绿（2026-10-24）。**体量**：`capabilities/server` Go 3,887,693 B / 545 文件（基线 10,183,233 B，−61.8%；第三轮加入视图/编码缓存、边缘层与静态资源，净增 34 KB）；手写前端 144,930 B / 11 文件（`web/packages/{platform,kernel}/src` + `web/apps/workspace/src`，不含生成的 `gen/`；基线 4,643,732 B，−96.9%）。

## 历史 2. system 作用域与角色

- [x] 2.1 宿主级唯一 `system` 租户：`tenants.go` 的 `systemSpec` 装配，不可由 Console 创建或删除；启动、恢复与健康检查按普通租户处理。
- [x] 2.2 非 system 作用域提交系统资产被拒绝（`Only the system tenant`），并有回归用例。
- [x] 2.3 角色：`system` 沿用 builder/publisher 语义；智能体 `system.architect` 只有 `file.save` 与 `change.propose`，不能自批。
- [x] 2.4 系统作者入口仅在 system 租户显示；已激活的系统定义按原 owner 安装为业务租户只读声明，仍受原数据权限限制，不复制系统元数据行或业务日志。作者入口与系统页面的读取投影是不同责任。

## 历史 3. 系统记录与内核闸门

- [x] 3.1 `system.file` + `system.blob`：路径唯一、内容寻址、修订检查、删除墓碑；复用日志与文件存储。
- [x] 3.2 `system.change`：来源、提示词、冻结文件清单、批准、诊断、失败状态；批准后清单不可改。
- [x] 3.3 `system.release` 与 `system.activation`：不可变发布、二进制/前端/工具链摘要、上一版本、可取消窗口；同一时刻只有一个未完成的 `requested` 激活；窗口结束允许换版，不释放未完成请求，激活成功时旧激活标 `superseded`。
- [x] 3.4 `internal/systemscope`：保存前与构建前各执行一次，冻结契约、提交入口、接受结果、重放、快照、日志、内部格式、授权与身份提供方，拒绝 `.git`、绝对路径、`..`、密钥。
- [x] 3.5 表驱动用例：内核闸门、修订冲突、同内容单副本、重放不重建、窗口取消、自批拒绝。

## 历史 4. updater 与构建激活

- [x] 4.1 `cmd/platform-updater`：`build`、`activate`、`swap`、`finish`、`serve`、`watch`。用 publisher 身份与宿主既有提交接口读写 system 作用域，不另建旁路。
- [x] 4.2 物化与固定构建：从提案绑定的完整封存源码物化，叠加冻结文件；固定执行 Go build/vet、锁定依赖还原、各包 tsc 和 updater 配置的 Vite；不执行仓库脚本、pnpmfile、Vite/PostCSS 配置或依赖生命周期脚本。
- [x] 4.3 体检、排空与切换：swap先封存确切激活与选择；监督进程在临时数据目录探测 `/healthz.release` 后排空、换版，失败回滚并记录 `rolled-back`。
- [x] 4.4 版本保留：`.build/system/{bin,web,releases}` 只保留最近 5 个，正在运行与可回滚的发布永不清理；release 0009 构建时 0001–0004 被清掉，0005–0009 留下。
- [x] 4.5 `/healthz` 报告 `release` 与 `releaseDigest`；工作区资源随发布目录切换。
- [x] 4.6 导出完整封存源码：`platform-updater export` / `make system-export RELEASE=<n>` 校验 release 的完整源树摘要，从 `sources/<digest>/files` 导出；不依赖可清理的构建工作树。旧缺完整源码的发布明确拒绝，不临时拿当前仓库补齐。

## 历史 5. AI 入口

- [x] 5.1 `system.architect` 智能体：只允许 `system.file.save` 与 `system.change.propose`，模型调用、预算与运行日志沿用 `apps/ai` 与 `agent.run`。
- [x] 5.2 提示词运行产出 `origin=prompt` 的变更，记录提示词、理由与文件清单。
- [x] 5.3 只有 publisher 可批准，提案主体不能自批（有回归用例）。
- [x] 5.4 禁区：内核路径在保存与构建两处被拒；智能体没有构建、激活或读取凭据的动作。
- [x] 5.5 真实模型路线：2026-10-25，DeepSeek 的 `audit-ai-verified` 运行完成读取→保存→提案→finish，主体 `agent:system.architect`、日志未遮蔽；人工批准 `chg-35` 后 updater 构建发布 0007，先取消再按新激活身份换版。`/healthz.release=7`，实际 HTML 标题为 `Platform self-hosting verified`，业务记录修订仍为 1。

## 历史 6. 人工入口

- [x] 6.1 `web/packages/platform/src/system.tsx`：changes、files、releases 三页，只在成员持有 `system` 角色时出现。
- [x] 6.2 变更视图：来源、冻结文件、诊断、批准、激活、窗口内取消；提案保留实际源码基线的前后映像，旧提案基线未知时明确说明，AI 运行链接取原 Agent owner 的调用上下文。
- [x] 6.3 源码编辑：文件列表 + 修订号 + 保存/删除，冲突由宿主拒绝并在界面上报。
- [x] 6.4 双语键（`packages/platform/src/i18n.ts`）。
- [x] 6.5 浏览器入口可用：宿主以 `-tenant system -admin user:admin@example.test` 启动时，开发席位持有 `system.publisher`，工作区列出 Platform 的三页；构建产物 `web/0013` 里能找到这些页面。完整点击演练仍待负责人走查。
- [x] 6.7 前端推倒重写（2026-10-24）：`web/packages/platform` 只剩 `api.ts`（宿主读写）、`host.tsx`（一个 client 的加载与刷新）、`ui.tsx`（按 `FieldInfo.type` 渲染字段）、`views.tsx`（记录、记录详情、定义、发布、自举控制台）、`index.tsx`（hash 路由与会话），样式是一张手写 CSS（4.9 kB，无 Tailwind）。自举控制台直接对上宿主既有动作：`agent.run.start`（system.architect）提问、`system.change.approve` 批准、`system.release.activate` 激活、`system.file.save` + `system.change.propose` 人工改代码。**实测**（lightweight 宿主 + 浏览器产物）：`GET /v1/sign-in`、`/v1/me`、`/v1/entities`、`/v1/actions`、`/v1/definitions`、`/v1/system-changes` 与 `POST /v1/records/{type}/query` 可用；用内核客户端对 system 租户提交 `system.change.propose` 成功（`chg-probe-1`），随后 `system.change.approve` 把它推进到 `approved`。
- 6.6 删除（不做）：同上，切换入口等于在业务租户里露出 system 作用域；需要时以 system 席位另开会话即可。

## 历史 7. 验收与文档

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
- 7.7 原归档目标已撤回，保留未闭合债务：本轮严格校验通过、真实模型演练已完成；完整组合页面运行时、全资产退役/旧租户迁移，以及浏览器权限中断后的未观察项目尚未闭合，当前不归档。

## Historical workflow notes

- 删除过程中 `scripts/boundaries.sh` 的允许清单只减不增；`scripts/escapes.sh` 随 `@platform/ui` 一起删除（它守的是已不存在的 kit 与 app 包）。
- 历史下一刀主张（已撤回，其现状需按新审计核对）：`capabilities/server/platform/page_*.go` 78 个文件 347,164 B，加上 `apps/build/page.go`、`page_composite.go` 与 `Section` 契约字段，是剩余最大的一块死重；新界面不读页面模型，删它是契约变更（需重生成 `host.ts`）而不是纯删除，单独一个 change 做。
- 自举上线后不以测试数量或代码行数作为交付证据；以演练与真实操作结果判断。

## 历史 8. SaaS 完备性与性能（第三轮，2026-10-24）

> 判据：一个人只拿浏览器，能不能把宿主的全套读写做完；以及同一台机器上每个读的代价。

- [x] 8.1 操作面按宿主已有的读铺开：`web/packages/platform/src/operate.tsx` 用一个通用表渲染 34 个 `GET /v1/{read}`（成员、权限、访问配置、设置、租户、账户、收件箱、请求、通知、审计、投递、工作、连接器、端点、效果、健康、个人数据读取、凭据、智能体、用量、模型、限额、运行、记忆、组织、项目、包、链接、时间线、视图、贡献），每个决定表单按目标类型从目录里现取；`hostconsole.tsx` 是宿主自己的作用域（`/v1/host/me|overview|tenants|templates|lifecycle|support`），只有被 `-host-admins` 点名的 subject 看得到入口。租户未装的应用（404）与角色不够（403）说人话，不抛红错。
- [x] 8.2 记录面补齐：证过的坑：`expectedRevision` 只在详情页带上（那是用户看到的版本）；列表页的批准/激活不带，否则一次读到的旧版本会把下一次决定挡回去（宿主如实回 409）。分页（50 一页）、按类型的搜索框、CSV 导出（`POST /v1/export/{type}`）、详情页把 `record.revision` 作为 `expectedRevision` 随决定提交（K4 C12，旧界面没有传），以及该记录的 `history`（谁、什么时候、哪次变更、改了哪些字段）。
- [x] 8.3 自举控制台重写：提问（`agent.run.start` / `system.architect`）、批准（`system.change.approve`）、激活与窗口内取消（`system.release.activate` / `system.release.cancel`）、改文件（`system.file.save`，带它期望的修订号）、提案（`system.change.propose`）。每个按钮先查目录里有没有这个动作，没有就不出现。**实测**：`chg-smoke1` 以控制台同形的提交提出（`system.change.propose`，带 `files`）→ `approved`；`chg-probe-1` 经内核客户端 outbox 提交后同样 `approved`；`platform.setting.set` 把租户名改成 `System` 并回读确认；`core.person.create` → `POST /v1/records/core.person/query` 搜到 `PER-SMOKE2`。
- [x] 8.4 前端自检：`pnpm -r typecheck`（kernel/platform/workspace 三包）与 `vite build` 全绿，产物 `index-*.js` 295.7 kB / gzip 88.7 kB、CSS 4.97 kB；宿主静态层按内容哈希给 `immutable` 一年缓存，`index.html` 走 `no-cache` + ETag。
- [x] 8.5 性能（`n=6000 c=16`，`PLATFORM_RATE_LIMIT=0`，不带 `Accept-Encoding`）：
  - `/v1/me` 1,255 → **7,281 rps**（p50 12.70 → 1.65 ms）；
  - `/v1/entities` 3,268 → **15,443**（0.76 ms）；
  - `/v1/actions` 3,614 → **13,112**；
  - `/v1/definitions` 2,883 → **8,251**；
  - `/`（静态首页）**40,953 rps**；`/v1/members` 4,985、`/v1/settings` 2,916。
  - 手段：视图缓存（键 = 权限签名 + 定义生成号，`maxViewEntries=8`）、编码体缓存（`kind:lang` → plain/etag/gz，`If-None-Match` → 304，gzip 直出已压字节：`/v1/entities` 65,913 → 12,074 B）、manifest 按 `platform.App` **值**缓存（draft/fork 同 ID 不同值）、`Accept-Language` 为空时不再列举租户语言（这一条单独把 `/v1/entities` 从 3.6k 推到 14k）、`AppsOf`/`TenantRecord`/`primaryRole`/`translator`/`setting` 全部走缓存 manifest、settings 无锁读（`atomic.Pointer` 快照）、`/v1/me` 不再是单请求 CPU 瓶颈（c=1 0.41 ms）。
- [x] 8.6 边缘层：安全头（nosniff/frame-deny/referrer）、CORS、`Vary`、令牌桶限流（`PLATFORM_RATE_LIMIT`，默认 600/s，0 关；429 + `Retry-After`）、`GET /metrics`（`platform_requests_total`、`platform_request_duration_seconds` 直方图、`platform_requests_in_flight`；需凭据或 loopback）、`PLATFORM_ACCESS_LOG=1`、`PLATFORM_PPROF=1` 才开 `/debug/pprof` 且只认 loopback。静态资源从内存出（启动时读 64 MB 上限）。
- 8.7 缺口（记录，不做）：**认证**只有 OIDC 或部署发放的令牌，没有本地口令、找回密码、邮箱验证——本地 IdP 只签发令牌（`idp/local.go` 的 HS256），加口令等于换一套身份模型；**计费/套餐/配额**不存在（多租户的生命周期、隔离与用量视图在，收款不在）；**整租户数据导出/删除**没有一键流程，只有按类型的 CSV 导出与逐记录决定；**界面文案**只有英文（宿主返回的声明文本按请求语言翻译，UI 自己的字没有字典）。
- [x] 8.8 决定面不逐个写死：`#/o/decisions` 把目录里所有「目标不是记录类型」的动作按目标类型分组列出（成员、角色、策略、团队、令牌、连接器、设置、工作、协议、端点、效果、操作、项目、包、凭据、通知、翻译），新增一个控制台动作不需要改前端。
- 8.9 历史下一刀主张（已撤回，其现状需按新审计核对）：`capabilities/server/platform/page_*.go` 78 文件 536 KB，加上 `apps/build/page.go` 与契约里的 `Section`，是最大的一块死重；新界面不读页面模型，删它是契约变更（重生成 `host.ts`）而不是纯删除，单独一个 change 做。

## 历史 9. 架构统一（第四轮，2026-10-24）

> 判据：一份定义，两个来源（Go 应用与 Build 应用）走同一条读通路；发布即生效，不改 Go、不改 React、不重启。

- [x] 9.1 应用与页面只有一条读通路：`capabilities/server/applications.go`（`Tenant.Applications`、`Tenant.PageView`）与 `routes_applications.go`（`GET /v1/applications`、`POST /v1/page-view`）。两者只读 `t.Definitions(m)`——code 应用的定义与 tenant 应用的定义在 `installDefinitions` 之后是同一份注册表，没有桥接层、适配层或第二套注册表。实测：`GET /v1/applications` 同时列出 code 的 `build` 与 tenant 的 `notes`、`directory`。
- [x] 9.2 Application Runtime：`web/packages/platform/src/runtime.tsx`。路由 `#/a/{app}/{page}[/{id}]`，侧栏按应用分组列页面，页面按定义渲染列表、记录、页面自带动作与子表；`POST /v1/page-view` 的应答就是它的全部输入。运行时不知道任何应用的名字。
- [x] 9.3 定义编辑同一份权威模型：`platform.Field` 增加 `fields`（子表列），`EntityActions` 把 `lines` 字段连同其列一起下发，前端 `Lines` 组件按列编辑行；`build.object.create/edit` 的 `fields` 因此是 20 列子表，而不是一个字符串。编辑表单由 `seedOf` 用记录现值预填，`build.object.edit` 第一次带上 `title`、`fields` 等既有值。
- [x] 9.4 缺陷①：客户端绑定身份后从不读 `GET /v1/declarations`，`authorityOf` 恒空，`Authorities.enqueue` 抛 `NOT_FOUND`，普通动作根本提交不出去。修复在 `api.connect()`（K5 A9）。实测：`platform.setting.set`（把租户名改成 `System`）与 `core.person.create` 都从内核 outbox 提交并确认。
- [x] 9.5 缺陷②：创建记录后 `CreateView` 打开的是幂等键而不是记录 id；`newId` 只有 6 个十六进制字符。改为 20 个十六进制字符，并用页面自己选的 id 打开。
- [x] 9.6 缺陷④及自举最后一公里：`installDefinitions` 在覆盖已有定义时只改数组元素、不推进 `defGen`，于是缓存里的 entity/catalog 永远是旧的——发布一个改过字段的对象，要重启才生效。实测修复后 `build.object.edit`（a,d）→ `publish` → `GET /v1/entities` 立刻是 a,e。
- [x] 9.7 缺陷（发布即隔离）：重装一个类型时，若旧记录还带着新声明已删掉的字段，严格解码失败会让已接受的批次无法应用并隔离租户（`install accepted object: json: unknown field "c"`）。`records.PruneImage` 在严格解码失败时丢弃已删字段重试，语义与 `recordStore.install` 的注释一致：记录保留仍在的字段的值。实测：删字段重发布不再隔离，行里的 `a` 保留、`c` 消失。
- [x] 9.8 平台管理页面由普通定义构建：租户用 `build.page.create` 建 `people`（`core.person`，list=name,kind,email，detail 六列，actions=`core.person.create/edit`），再用 `build.app.create` 建 `directory` 把它挂上，发布后 `POST /v1/page-view {"app":"directory","page":"people"}` 返回 list-detail 页面与平台的 `core.person` 动作。宿主自己没有为这个页面写一行 Go 或 React。
- [x] 9.9 楼宇供暖应用包：`deploy/heating/application.json`（5 对象 / 5 页面 / 1 应用）+ `capabilities/server/cmd/install-bundle`（读 `/v1/actions` 取内容版本，按 `bundle-<tenant>-<n>` 幂等，走与人相同的 create→publish 决定）。实装在**另一个租户** `workspace`：22 次决定、2.2 秒，装完 `GET /v1/applications` 出现 `heating` 五页，`page-view` 返回六个动作；业务链 BLD-1 → ZON-1 → MTR-1 → RDG-1 → WRK-1 assign → complete 跑通，`done` 上 cancel 被 409 拒绝。见 `deploy/heating/README.md`。
- [x] 9.10 发布候选可封闭：`POST /v1/releases/candidates` 原本不存在，而生产档的拒绝语就叫运维去 POST 它——文档里唯一的生产交付路径不可达。现在这条路由按预览给出的 candidate id 冻结草稿（走一直存在的 `SaveReleaseCandidates`，请求体类型 `ReleaseSaveRequest` 早已声明却没人用）。实测（`workspace` 租户，heating 应用）：`POST /v1/releases/preview` → `POST /v1/releases/candidates` 201 → `GET /v1/releases/candidates` → `GET .../{id}`（34 个资产、`canActivate:true`）→ `POST /v1/releases/active` 200 → `/v1/releases/active` 报告该 id，页面与记录完好。
- [x] 9.11 恢复路径的同一致命伤：重新声明过类型的租户在快照恢复时，遇到仍带着已删字段的行会 `restore snapshot 92: json: unknown field "c"` 并隔离。`decodeRowImage` 把「严格解码失败 → 丢弃已删字段重试」这条规则同时用于重装与快照恢复；被隔离的 `system` 租户随后恢复干净，唯一那行仍读出保留下来的字段。
- 9.12 原完整自举验收目标已撤回，保留未闭合债务：完整 composed/Document 页面运行时与旧应用退役后的历史/租户恢复仍未完成；已删除错误近似渲染，复杂声明在读取前明确说明不支持。过时命令、文档及本轮审计结果已同步；不将有限支持写成全量页面验收。
- [x] 9.13 按调用方核对页面契约：`page_*.go` 仍参与 Build 保存、候选发布、字段/端口/权限投影，不能判为无用代码。保留必要契约，删除前端将复杂组件冒充通用列表的错误近似；复杂 Document 页面明确说明当前运行时不支持，不读业务数据。
- 9.14 本轮净变化（`4830263..HEAD`）：41 文件、+2,711 / −890；其中 `pnpm-lock.yaml` 因旧锁文件损坏被重生成（−786）。Go 侧新增 `applications.go`（≈150 行）、`routes_applications.go`（≈60 行）、`PruneImage`（20 行），删掉 `publications.go` 里为定位隔离原因临时加的日志。

## 历史 10. 本轮自举审计收尾

- [x] 10.1 冷启动可按路径读取完整运行源码；保存/提案/构建使用同一闸门与规范路径，凭据不进入索引；完整基线绑定提案，后续仓库修改不进入构建。
- [x] 10.2 纯定义、混合发布、只读共享声明、业务记录归属及 SQL 安装 delta 复用原发布 owner；日志与快照恢复通过 owner 测试。同 Ref 旧版回退可用；需要移除新增资产的回退仍由原 owner 明确拒绝，不能伪装删除成功。
- [x] 10.3 updater 保留制品后再上报，未知答复复用原键；保存确切激活 ID 与前驱，取消重试不覆写原结果，支持首发监督接管与启动失败的新回滚决定。
- [x] 10.4 清除旧自举界面和未消费宿主构建接口；补双语帮助、真实应用 owner/name 路由和代码换版提示；修工具输入与日志 metadata 命名冲突、逻辑目标虚构来源、具名读取的实际 owner 失效。审阅改为逐文件前后对照，基线与提案一起冻结并能恢复，不信任客户端 correlation 标签作为 AI 运行证据。
- [x] 10.5 集中 Go build/vet、保留测试和新增 owner 测试、Web tsc/Vite、生成一致性、边界与 OpenSpec 检查通过；真实人工定义/代码、DeepSeek 提案换版、编译失败、实际数据目录启动失败回滚、取消重试和封存导出已记录于同一 WorkQueue。追加真实 PostgreSQL 隔离测试，发现并修复候选原始字节被嵌套 JSON 重组的问题；SQL 激活、业务写入、检查点与重连恢复通过。追加文件审阅收尾后，自举与 Agent 受影响测试、Go build/vet/格式/边界、Web 类型与产物、OpenSpec 严格检查通过；不重复运行未受影响的 PG 与整套演练。浏览器关闭后未再操作，新审阅界面未获浏览器验收；完整 Document、全资产退休和旧部署迁移仍未完成，不据此归档原 change。
