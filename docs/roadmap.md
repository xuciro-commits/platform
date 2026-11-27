# 路线图

这份文档回答：当前阶段的每一行还剩哪几项，改哪里，怎样验收。
行与“做完你能看到”在 [plan](plan.md)；阶段 4 的灵魂与门槛剧本也在那里，每一项都对着它验收。

## 规矩

- 工程验收遵循[三档验收](README.md#三档验收)，不额外设日常全量门禁。
- 一项一次提交。先写能跑的验收（内核不变量或 trunk 的一步），再动手；做完把这一项删掉，一行的项删完就删掉 plan 里的那一行。
- 一项做完，它定下的规则同一提交写进归属的文档（类型与对象进 [domain](domain.md)，界面进 [ux](ux.md)，分层进 [architecture](architecture.md)），这里只留没做的。
- 路上发现的新项加在本阶段末尾，不另起清单。
- “trunk”指 `frontend/e2e/<模块>.spec.ts` 里的一步；共用的登录与取值在 `e2e/support.ts`。`make trunk SPEC=<模块>` 定点跑，`make trunk-changed` 按改动映射跑。
- 这里写的设计是负责人 11-27 定的；照写的做。对不上代码或做不到时停下来问，不自己改设计。
- 缺口登记在 `inner-box/`（不入库）。开工把那一条的 `status` 改成 `implementing`，做完改 `verified` 并写上提交号；条目只记证据，设计以本文为准。

## 并行的规矩

- 多个 Agent 并行时，本文先给每人列出拥有的路径。各用一个 worktree 与分支 `wave/<主题>`；每项做完 `git rebase origin/main`，定点验收通过后快进推送 main。
- 只改自己拥有的路径。别人的路径缺东西，在本阶段末尾记一项“要某某做：……”，不动手。
- 共享文件只追加或只改自己的小节：`frontend/src/i18n/`、`docs/glossary.md`、本文与 plan（只删自己做完的项）。
- 生成文件（`contract/`、`frontend/src/host/gen/`）谁改宿主类型谁跑 `make contract`；rebase 冲突时重新生成，不手合。
- 各自通过不等于合起来通过：host 行数棘轮、文件改名、新增的类型取值都会在合并后才撞上。合并后在合起来的 main 上再跑 `make check`、`make check-frontend` 与相关不变量。
- 交叉审查：改 `kernel/`、接受路径、授权或包协议的提交，推送 main 之前交给另一个 Agent 审。只给它代码差异，不给作者的结论；
  它答三句：改的函数还有谁在用；接外部程序的有没有真实运行记录；同类问题别处还有没有。三句的答案写进提交信息的“检查”一节。

## 阶段 4 剩下的

### 3.x 阶段 3 收口

- 用最新 main 受控重启 `sim` 宿主，补跑模拟客户第 3、6、11、13、14 个业务日；问题记在本阶段末尾，一项一修。
- 空载跑一次干净提交的 `make verify` 与 `make load-sql` 万行导入基线；然后由 AI 走一次阶段 3 门槛演示，录屏与结论交负责人。
- 做完删掉 plan 里的阶段 3 一行。

### 4.1d 变异验收（4.1a、4.2a 留下的）

- 把余额约束、事实建后不可改、只能冲正一次、关账日期、余额的唯一写入者、编译期拒绝跨引用、受限值让守卫拒绝、守卫读来源当前值八处逐个改坏，
  K47–K49 每次都必须变红；先证明原版用例真的跑过且通过，语法错误不算检出。
- 照 [agent-autoloop](https://github.com/xuciro-commits/agent-autoloop) 的 `sim/mutate.ts` 做成按需跑的脚本，不进日常检查；漏检一处就补不变量。

### 4.2c 纯度检查收尾

- 现状：检查已进 `make check`，只报告不拦截；内核、授权与策略对话框里的泄漏已清。
- 报出的两千多行候选几乎都是误报：`item`、`assets`、`call`、`party`、`inventory`、`projects` 这些包名同时是常用词。
  改成只查命名空间的限定名（以 `<namespace>.` 开头的标识与字符串）和与包 `id` 完全相等的字符串字面量，不查散落的整词。
- 市场卡片不再按包名挑封面与图标（`PackageCard.tsx` 的 `officialCovers`、`anatomyCovers` 与 `key.includes("crm")`）：封面与图标由包声明。
- 候选清零后改成拦截。验收：在内核里加一行 `case "crm"`，`make check` 失败。

### 4.3c 积木故事走一遍界面

- K50 已守住组合预检与停用后的降级；界面还没走过。marketplace 的 trunk 加一步：装采购 → 装库存，审阅里看到要接上的契约 →
  收货后出现库存流水 → 停用库存，采购照常，界面写明“库存未接入”。

### 4.x trunk 的 SPEC 是子串匹配

- `make trunk SPEC=records` 把 `records.spec.ts` 当作模式交给 Playwright，`crm-records.spec.ts` 也被选中，却跑在 records 场景的租户里，报“missing action crm.dealline.create”。
- 改成按文件名精确选（例如 `/records\.spec\.ts$`）；验收：`SPEC=records` 只跑 records.spec.ts，`SPEC="records crm-records"` 两个都跑且各在自己的场景里通过。

### 4.4b 发版就绪（IB-PACKAGE-0002）

- 对候选提交跑 `make verify`，确认 CI 全绿，整理发版说明，交负责人打 tag；tag 后核对 Release 资产、`SHA256SUMS` 与来源证明。
- 包库把 `platform.lock` 切到新版本，用发行版 `pkg` 重新核对全部包与升级门禁后发版；市场能装到新版本。

### 4.x 阶段 4 门槛演示

- 上面各项做完后，在空租户装 item-master、inventory、purchasing，按 plan“阶段 4”的六步剧本走一遍，录屏、日志与结论交负责人。

## 之后的项

不在三波里；等用到它的阶段或模拟客户的剧本再排。

- 扩展包的落点：目标应用的记录页显示扩展包挂的附属对象（现在只有人员页显示），到第一个扩展包时做。
- 语音网关：真手机实测来电分配与中继热更新，修本机外放；公网生产化（TLS 与 SRTP、NAT、多中继、录音），等第一个电话客户。
- 受管服务：注册、证书、探活、预留与结算；第二个探针 `document.render/1`（服务端渲染 PDF），归档与对外发送单据时做。
- 商业供应商适配：客户选定供应商后按连接器契约接。
- 界面微件：仪表盘的散点、迷你图与排行；`PdfViewer`；高精度小数；`RelationCanvas`、字段溯源、构建仿真；
  治理与血缘工作台。原始意图查 Git 标签 `archive/web-final`。
- 开放应用接入的后续包（2.8）：按 [platform-packages](https://github.com/xuciro-commits/platform-packages) 的 sources 逐个接，坑写进 `open-app-intake` Skill。
- 阶段 6 的智能体：设计在 [architecture](architecture.md)“智能体”。最先做的是动作的 `ask` 声明（平台认它、`pkg check` 校验），
  做了以后 ERP 包把删除、对外发送、不可冲正的动作标上，工具面与委派直接用。
- 包的读在并发下慢：按定义纪元缓存包视图，审阅与准备只算被问的那个包。

## 承重的要求

- 规则只在宿主求值一次：编号、默认值、带出、余额、状态转换、授权都在接受路径里算；界面只读类型描述，不复算、不猜。
- 一项能力一个实现：发号只经 `Caller.Next`（`platform.Sequence`），表达式只经 Expr 求值，授权只经唯一的决定点，汇总只经对象集聚合；不新写第二套。
- 不写行业特化代码：平台里没有库存、采购、会计、CRM 这些词；能力声明在类型、动作、角色上，所有类型同时得到。
- 重放得到同样的字节：宿主算出的号、默认值、带出值写进接受结果，重放读回，不重算。
- 投影可以丢：余额这类由事实算出的东西不进接受结果，格式变了就从事实重建，不写迁移；K47 守着它与事实一致。
- 每项一次提交：先写 trunk 步骤或内核不变量，再实现；改宿主类型后 `make contract`；相关 `make test RUN=K…`、
  改到的 trunk 用例通过才提交。完整静态与构建检查由 CI 执行，发版前才用 `make verify`；没有改动不重跑。
- 每条命令计时，控制在一分钟内；超过的写明命令交负责人在外面跑。

## 后端

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，依赖例外、包名与目录名不一致、能力探测均为零。
宿主为上面某一项补字段或接口时，保持租户数据、凭据引用、授权、当前格式纪元内的历史字节与已接受结果；定点验收，
涉及并发、分叉或恢复时跑 K13 竞态检查或 `make verify-sql`（[architecture](architecture.md)）。
