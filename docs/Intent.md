# Platform Core Intent & Anti-Bloat Engineering Constitution

## 1. 我们的哲学：干什么，为什么而存在？ (Why We Exist)
本平台不是“大模型套壳玩具”，也不是给军工寡头画大饼的“UAF 巴别塔”，而是面向实体产业（如制造业、供热、服务业）与不懂代码的一线实干者的**“高确定性生产力外骨骼（Intelligence Augmentation, IA）”**[cite: 1]。

我们的核心技术信仰只有一句话：
**“把外部大模型当成可随时替换的廉价耗材，把确定性的内核契约（Kernel K1-K9）、事件账本（Journal）与有限状态机规则（Rules/FSM）当成唯一的核心资产。”**[cite: 1]

1. **确定性高于一切**：AI 永远没有直接修改业务状态或越权读写数据库的特权；AI 只能产出 `Proposal / Candidate`（提议），必须经过 `Simulate`（沙箱预演）、`Business Constraints / Refusals`（确定性规则校验与拒止）以及 `Confirmation / Accepted`（确认落盘）的状态机闭环[cite: 1]。
2. **设计态与运行态硬隔离（Zero Runtime Token Tax）**：昂贵的云端大模型（如 Anthropic Claude）严格限制在 **设计态（Design-Time / Studio Authoring）** 辅助生成 Schema、工作流草稿和公式；一旦应用发布进入 **运行态（Run-Time / Production）**，70% 以上的流量必须由纯代码状态机与端侧 WASM 引擎（0 云端算力成本）执行，其余常规非结构化任务仅允许路由至本地/边缘轻量开源小模型（7B-14B）[cite: 1]。
3. **把“确定性拒止”转化为“私有蒸馏资产”（Refusal-to-DPO）**：每一次 AI 提议被内核规则拒止（`Refusal Facts`）并在修正后通过（`Accepted`），都是极高价值的工业级偏好对齐样本[cite: 1]。平台必须自动将这些交互落盘为标准化 SFT/DPO 训练语料，用云端巨头的智力免费喂肥我们自己的垂直小模型[cite: 1]。
4. **平民化赋能（Human-Centric IA）**：彻底屏蔽学院派与军工级黑话（如 UAF 1.3 元模型、`JointRelease`、`ObservationIntervals`），向一线业务人员和 AI 上下文仅暴露最朴素的四大领域原语：**组织与岗位（Identity/Authz）、台账与记录（Books/Records）、工序与流转（Flow/Work）、铁律与红线（Rules/Constraints）**[cite: 1]。

---

## 2. 现状病灶诊断：我们必须铲除的“四大 AI 开发病” (Pathologies to Eliminate)
当前代码库（尤其是 `capabilities/server/`）患上了严重的“AI 局部补丁综合征”，任何后续开发必须以**消灭**以下病灶为前提，严禁延续旧习[cite: 1]：

1. **病灶一：五层俄罗斯套娃传话筒（5-Layer Pass-Through Bloat）**
   同一个领域概念（如 `formula`, `writeback`, `table`, `observation`, `release`, `function`）在 `capabilities/server/`、`platform/`、`apps/build/`、`internal/construction/`、`internal/host/` 五层目录中被重复定义、层层包装转发，充斥着毫无业务价值的 `Struct A -> Struct B` 胶水代码[cite: 1]。
2. **病灶二：文件无脑平铺与“名词通胀”（File Proliferation）**
   过去每次响应 Spec 变更都通过“新建 `.go` 文件”来逃避抽象，导致 `capabilities/server/` 根目录下堆积了 230+ 个文件（如 24 个 `accepted_*.go`、十几个 `business_refusal_*.go`），缺乏泛型、接口与表驱动（Table-Driven）收敛[cite: 1]。
3. **病灶三：后端越界充当“前端 DOM 保姆”（Backend UI Coupling）**
   `capabilities/server/platform/` 下存在 60+ 个 `page_*.go` 文件（甚至包含 `page_spacer.go`, `page_separator.go`, `page_button_group.go`, `page_treemap.go` 等纯视觉排版组件）[cite: 1]。用 Go 后端校验前端像素级 UI 组件，不仅锁死了前后端迭代，更导致 AI 生成页面时 Token 消耗暴增 5 倍以上[cite: 1]。
4. **病灶四：自举死锁与重甲包袱（Self-Hosting Deadlock & UAF Armor）**
   在平台边界尚未固化时强推全量 UI 自举（`platform-self-hosting`），极易因 Schema 演进导致系统控制面锁死；同时在核心包内塞入数兆大小的 `internal/enterprise/uaf/`（OMG UAF 1.3 规范），严重污染上下文与编译速度[cite: 1]。

---

## 3. 铁律：干什么 vs 别干什么 (Strict DOs & DON'Ts for AI Agents)

### 【绝对禁止 (NEVER DO)】
- **严禁靠“新建文件”交差**：在任何目录下新增 `.go` 文件前，必须先证明无法通过扩展现有结构体或表驱动模式实现；**每新增 1 个文件，原则上必须在同一次变更中合并或删除至少 2 个旧冗余文件**。
- **严禁跨包同名传话筒**：严禁在两个以上的包中出现同名业务文件（例如禁止 `apps/build/formula.go` 与 `internal/construction/formula.go` 并存）[cite: 1]。`internal/construction/` 与 `internal/host/` 必须被逐步清空并物理删除[cite: 1]。
- **严禁在 Go 后端感知具体 UI 视觉组件**：Go 代码中禁止出现 `Spacer`, `Separator`, `ButtonGroup`, `Kanban`, `Gantt`, `Heatmap`, `Sparkline`, `Treemap`, `Gauge` 等纯前端视觉组件名称及专用校验逻辑[cite: 1]。
- **严禁业务代码直调外部大模型 SDK**：除 `aigateway` 底层 Provider 实现外，任何业务包不得直接引用 `anthropic` 或外部 AI HTTP 客户端，所有 AI 调用必须经由统一的 MCP 网关、ReBAC 权限检查与 Quota 熔断器[cite: 1]。
- **严禁将系统核心控制面交由动态 Page 引擎自举**：租户管理（`tenants`）、凭据管理（`credentials`）、模型路由配置（`model_configuration`）必须保持静态、确定性的系统级控制面，绝不参与动态 UI 自举[cite: 1]。

### 【必须执行 (ALWAYS DO)】
- **做减法优先于做加法**：衡量每次 OpenSpec 变更质量的核心指标是 **“净删除代码行数（Net LOC Removed）”** 与 **“包内聚度提升”**，而非堆砌新名词[cite: 1]。
- **保持 `contract/` 与 `journal/` 的纯洁性**：所有状态变更必须严格收敛于 `contract/proto/platform/kernel/v1alpha1/`（K1-K9）与 `contract/lean/` 所证明的幂等语义[cite: 1]。
- **用 `cmd/check-boundaries` 物理锁死红线**：所有架构约束必须同步转化为 `cmd/check-boundaries/main.go` 中的静态 AST/目录扫描断言，不通过边界检查的代码视同构建失败[cite: 1]。
