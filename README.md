# Platform · AI 业务应用平台

面向 **FDE（前线部署工程师）** 与 **企业客户构建者** 的下一代 AI 业务应用平台，产品形态与用户体验对标 **Palantir Foundry / AIP**。

平台提供从**数据连接**、**语义建模（Ontology）**、**动作/流程/AI 编排（Workshop / Automate / AIP Logic）**、**操作界面渲染** 到 **受控发布与持续演进** 的完整闭环。底层通过语言中立的内核契约（`contract/`）、强类型多租户 Go 宿主运行时（`capabilities/server/`）、现代化 Web 工作区（`web/`）以及自治业务应用（`apps/`、`solutions/`）提供数学与工程层面的可靠性保证。

---

## ⚡️ 5 分钟快速上手 (Quick Start)

### 1. 环境准备 (Prerequisites)

运行平台所需基础环境：
- **Go** >= 1.23
- **Node.js** >= 20 且启用 `corepack`（使用 `pnpm 8+`）
- **Docker** 或 **OrbStack**（用于本地运行 PostgreSQL、Rauthy OIDC、RustFS 等轻量基础设施）
- **Python 3**（用于本地环境编排脚本 `deploy/dev/environment.py`）

首次克隆代码库后，执行一键环境初始化（安装 Web 依赖、代码热载工具 `air` 及浏览器内核）：

```bash
make setup
```

---

### 2. 启动模式选择：轻量单机 vs 全栈热开发

平台提供**两套完备的运行架构**（[ADR-0049](docs/ADR/0049-delivery-and-lightweight-profiles.md)），可根据你的开发环境与资源情况自由选择：

#### 方案 A：零 Docker 轻量持久化服务 (`make lite` · 强烈推荐个人/轻量环境)

**完全不需要启动任何 Docker 容器**！单二进制、单数据目录，内置自研轻量 IdP 认证与本地文件存储：

```bash
make lite                          # 默认启动酒店业工作台 (http://localhost:8495)
# 或
make lite SOLUTION=manufacturing   # 启动制造业工作台 (http://localhost:8490)
```

> **运行机制说明**：
> `make lite` 自动完成：
> 1. 构建 Web 前端工作区（`web/apps/workspace/dist`）；
> 2. 编译对应行业宿主单二进制；
> 3. 在本地目录（`.build/lite/<solution>/`）自动生成自研本地 IdP 密钥（`idp.key`）；
> 4. 自动签发开箱即用的管理员 Token；
> 5. 以单进程直接提供完整的 API、持久化日志、本地文件存储与前端 SPA 页面服务。

---

#### 方案 B：全栈热更新开发环境 (`make local` · 适合多租户/标准协议交付)

基于轻量基础设施容器（PostgreSQL、Rauthy OIDC、RustFS S3），支持前后端双端热重载：

```bash
make local
```

> **运行机制说明**：
> `make local` 会在后台以守护进程方式运行：
> 1. 自动拉起基础设施容器（PostgreSQL、Rauthy、RustFS、Webhook Sink）；
> 2. 通过 `air` 监听运行两个行业的 Go 宿主后端；
> 3. 通过 Vite 启动前端工作区并反向代理后端 API。
> 日志输出至 `.build/dev/` 目录。重复执行不会导致进程重复启动。

---

启动完成后，直接在浏览器中打开对应行业工作台：

| 行业解决方案 | 工作台地址 | 对应租户 | 包含业务应用 |
|---|---|---|---|
| **酒店业 (Hospitality)** | [http://localhost:8495](http://localhost:8495) | `hotel-a` | CRM, PMS, HCM, CSM, 自动化, 构建者工坊 |
| **制造业 (Manufacturing)** | [http://localhost:8490](http://localhost:8490) | `plant-sz` | MES, ERP, 设备管理, 自动化, 构建者工坊 |

---

### 3. 开箱即用人员账号与凭据

所有预设本地人员账号的登录密码均为：**`Plant-Local-1`**（在 `make lite` 模式下，工作台登录页还会自动列出预签发的各角色卡片，点击即可一键进入）。

#### 常用核心角色推荐：

| 身份角色 | 登录邮箱 | 建议访问宿主 | 权限与职责说明 |
|---|---|---|---|
| **制造业主管 (全能/构建者)** | `sup@plant.test` | `http://localhost:8490` | MES 主管，拥有 `builder`（构建者）、企业管理员与 AI 管理员权限，可进入 Ontology、Workshop 与配置 |
| **酒店总经理 (全能/构建者)** | `manager@hotel.test` | `http://localhost:8495` | 酒店管理及跨应用审批（两级审批人），拥有构建者与平台管理员全权 |
| **酒店销售顾问** | `sales@hotel.test` | `http://localhost:8495` | CRM 销售商机、PMS 前台日常接待与操作 |
| **酒店客服专员** | `desk@hotel.test` | `http://localhost:8495` | CSM 服务工单处理，AI 降级时的工单承接人 |
| **工厂采购员** | `buyer@plant.test` | `http://localhost:8490` | ERP 采购订单下发与收货处理 |
| **工厂总账会计** | `accountant@plant.test` | `http://localhost:8490` | ERP 会计凭证起草、过账、冲销与发票登记 |
| **产线操作员** | `op1@plant.test` | `http://localhost:8490` | MES 产线工位执行与报工操作 |

*注：若需查看不同人员的权限控制隔离效果，可在普通窗口与隐私/无痕窗口中分别登录不同账号。*

---

### 4. 其他启动模式

- **极速内存开发探针**：
  使用纯内存状态运行单个后端，配合 `air` 快速调试代码：
  ```bash
  make dev-light SOLUTION=hospitality    # 端口 8495
  ```
- **全容器生产验证模式（Docker Stack）**：
  一键切回与生产交付一致的多容器镜像模式（包含 Wasm 沙箱与隔离编译能力）：
  ```bash
  make docker
  ```
- **停止开发容器**：
  ```bash
  make infra-down   # 停止容器，数据卷自动持久化保留
  ```

---

## ⚖️ 架构双轨制：交付 Profile vs 轻量 Profile

依据 [ADR-0049](docs/ADR/0049-delivery-and-lightweight-profiles.md)，平台在底座层实现了**语义完全对齐的两套基础设施 Profile**，通过启动参数显式声明（`-profile delivery|lightweight`），绝不隐式降级或混用：

| 维度对比 | 生产交付 Profile (`delivery` · 对应 `make local` / `make docker`) | 轻量单机 Profile (`lightweight` · 对应 `make lite`) |
|---|---|---|
| **定位场景** | 企业级多租户交付、标准云原生基础设施部署、合规审计 | 边缘单机运行、离线小设备、快速体验、无 Docker 限制机型 |
| **依赖环境** | 需 Docker (PostgreSQL, Rauthy, RustFS, Compute 容器) | **0 容器依赖**（仅需 1 个可执行二进制 + 1 个本地数据目录） |
| **账本持久化** | **PostgreSQL** 关系型数据库存储提交日志与状态快照 | **纯 Go 单文件日志**（`journal.jsonl` + 快照目录；预留 SQLite 同契约接口） |
| **身份认证 (IdP)** | 外部 **OIDC 认证提供商**（本地默认使用 Rauthy 容器 `:8480`） | **内置自研轻量 IdP**（基于 HMAC-SHA256 签名，本地管理 `idp.key`） |
| **文件存储** | **S3 协议兼容**对象存储（本地默认使用 RustFS 容器 `:9000`） | **本地内容寻址存储**（`<data>/files/<tenant>/<hash>`，零外部依赖） |
| **计算引擎** | 外部容器化 Wasm Worker 与 Code Builder | **宿主进程内 `wazero`** 纯 Go Wasm 运行时 |
| **前端工作区分发** | Vite 反向代理开发服务器 或 生产独立 Nginx | **Go 宿主单二进制直接托管**静态工作台 SPA（`-web` 参数） |
| **备份与迁移** | 需同步备份 PostgreSQL 卷与 S3 存储桶 | **直接复制单一数据目录**（如 `.build/lite/<solution>`）即可完成全量备份与迁移 |

---

## 🏛 核心架构与目录分层

平台严格遵循“**单向契约、单一归属、内核零领域词汇**”的设计原则：

```
├── contract/                 # 【内核契约】语言中立 K1–K9 规范、Protobuf 定义、Go 参考实现及 Lean 形式化证明
│   ├── spec/                 #   K1 身份、K2 事实、K3 决策、K4 租户隔离等内核契约
│   ├── proto/                #   Protobuf 规范定义（不含业务领域语义）
│   └── lean/                 #   有界数学不变量的形式化定理证明
│
├── capabilities/server/      # 【宿主运行时】多租户权威宿主（单写者提交日志、快照重放、OIDC、安全沙箱）
│   ├── platform/             #   业务应用唯一可导入的官方运行时接口 (App API)
│   ├── journal/              #   权威账本存储、分录追加与状态快照
│   ├── idp/                  #   身份认证集成（OIDC / Rauthy / 本地 Token）
│   └── apps/                 #   平台级通用能力（work 审批与任务, flow 流程, ai/agent 智能体, org 组织）
│
├── web/                      # 【前端工作区】基于 Vite + React + TypeScript 的高保真企业级工作台
│   ├── packages/ui/          #   共享 UI Kit（RecordList 记录列表、看板 Board、透视表 Pivot、图表 Chart）
│   ├── packages/app/         #   前端应用运行时模型、双语上下文与组件状态
│   ├── packages/build/       #   构建者工坊（Ontology 对象建模、Workshop 页面装配、Automate 流程、AI Functions）
│   ├── packages/platform/    #   治理与控制中心（Control Panel、Runs 运行中心、Data Connection 数据连接）
│   └── apps/workspace/       #   统一主工作区（Shell 导航轨、应用门户、资源浏览器、数据血缘 Lineage）
│
├── apps/                     # 【自治业务应用】各业务领域独立逻辑（实体、动作、只读视图、生命周期）
│   ├── crm/                  #   客户关系管理
│   ├── mes/                  #   智能制造执行系统
│   ├── erp/                  #   企业资源计划系统（科目、凭证、库存）
│   └── pms/ hcm/ csm/        #   参考应用：酒店管理、人力资源、客户服务
│
├── solutions/                # 【跨行业组合宿主】将多个自治应用组装为完整交付实例
│   ├── hospitality/          #   酒店业组合宿主 (CRM + PMS + HCM + CSM)
│   └── manufacturing/        #   制造业组合宿主 (MES + ERP)
│
├── protocols/                # 【跨应用标准协议】应用间交互的唯一接口标准，严禁私自直连数据库
└── deploy/                   # 【部署与交付】本地环境配置、Compose 栈、演练测试脚本与容器沙箱
```

---

## 🛠 开发与质量保障流水线 (Makefile 三层契约)

项目严格遵照 [ADR-0081](docs/ADR/0081-build-layers.md) 规定的三层统一命令入口：

### 1. `dev` — 本地敏捷开发
- `make local`：一键进入本地开发状态（Docker 提供基础件，Air + Vite 实现双端秒级热更新）。
- `make docker`：切回全容器化运行模式，对齐交付态。
- `make dev-light`：零 Docker 依赖的内存宿主运行。
- `make infra` / `make infra-down`：独立拉起或停止基础服务容器。

### 2. `check` — 提交前快速门禁（< 2 分钟，无容器、无浏览器）
- `make check`：完整静态检查（编译、Go vet、代码格式化、架构边界合规、Web 类型检查）。
- `make check-go`：宿主构建与 `boundaries.sh` 边界守护检查。
- `make check-web`：资产库 Catalog 校验及全部前端包 `tsc` 类型检查。
- `make test-go [PKG=...] [RUN=...]`：运行后端指定单测。
- `make e2e [SPEC=page-notice]`：运行单条 Playwright 浏览器关键路线测试。

### 3. `release` — 发布与演练验证（完整证据链）
- `make rehearse`：一键执行交付演练（启动干净临时容器栈，验证从 OIDC 鉴权、业务恢复到 UI 路线的完整链条）。
- `make verify`：执行全量 CI 验收流水线（内核契约、形式化证明、全量单测与构建）。
- `make build`：编译生成前端发布包与两个行业的宿主二进制文件至 `.build/release/`。
- `make images`：构建并标记生产 Docker 镜像。

---

## 🔌 本地基础设施服务清单

本地开发栈同时开放了以下配套调试与管理端点：

| 服务组件 | 访问端口 / 地址 | 认证凭据 / 账号说明 |
|---|---|---|
| **OIDC 认证中心 (Rauthy)** | `http://localhost:8480/auth/v1/` | 管理员：`admin@platform.test` / `Admin-Local-Only-1` |
| **PostgreSQL 数据库** | `localhost:5433` (数据库 `platform`) | 用户名：`platform` / 密码：`platform-local-only` |
| **RustFS 对象存储 (S3 兼容)** | 接口：`http://localhost:9000`<br>管理控制台：`http://localhost:9001` | 访问密钥：`platform`<br>私钥密码：`platform-files-local-only` |
| **Webhook / 系统替身 Sink** | `http://localhost:8497` | 用于模拟外部 ERP、邮件通知及本地模型回调 |

---

## 📖 核心文档索引与开发规范

在贡献代码或排查问题前，请参阅以下权威文档：

- 🤖 **开发者与 AI 助手准则**：[AGENTS.md](AGENTS.md)（*代码修改必读：开发边界、双语文案与检查规范*）
- 🎯 **产品宗旨与意图**：[docs/Intent.md](docs/Intent.md)（*为什么构建这个平台、核心权衡与不可变量*）
- 📐 **系统架构全貌**：[docs/Platform.md](docs/Platform.md)（*五大平面、提交模型与能力演进图谱*）
- 🧭 **现行工作队列**：[docs/WorkQueue.md](docs/WorkQueue.md)（*当前正在进行与待排期任务*）
- 📋 **架构决策记录**：[docs/ADR/](docs/ADR/)（*包含 Shell 设计 ADR-0052、统一 Makefile ADR-0081 等*）
- 🧪 **测试与验收规范**：[docs/Testing.md](docs/Testing.md)（*测试分级、浏览器 Spec 与性能标准*）
- 🚢 **本地运维详细指南**：[deploy/local/README.md](deploy/local/README.md)（*网络拓扑、外部 API Key 配置与故障排查*）
