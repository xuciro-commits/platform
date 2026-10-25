# Proposal

## Why

平台已经能自举**租户的应用**：`build` 应用把对象、页面、动作、函数、流程与智能体保存为数据，发布即安装，租户页面由通用运行时渲染。但平台**自己的系统**仍完全锁在编译期：平台应用、工作区编辑器、路由与前端包都是 Go/React 源码，任何改动都必须由外部开发者提交、构建并重启。这使"改系统"与"用系统"是两条路，交付速度、复用和后续演进都卡在源码发布上。

本变更把这两条路合成一条：系统自身也成为受控对象，人和 AI 用同一套提交、接受、发布与激活机制修改它。内核（`contract/` K1–K9、提交管线、日志与历史）继续冻结，自举只在应用层与表现层生效。

同时，仓库当前 26 万行手写代码与 7 万行测试中，大部分服务于已经退出的行业探针、集成驱动、企业建模与专业可视化。它们与自举无关，却决定了构建时长、概念数与每次改动的验证面。本次同批删除。

## What Changes

- 新增宿主级 **system 租户**：平台自身的元对象、源码文件、变更与发布全部是它的受控记录，写入只经原提交与持久接受路径。
- 新增四个记录：**system.file**（按内容寻址的源文件与修订）、**system.change**（来源 prompt/ui、基线、冻结文件清单、诊断、批准）、**system.release**（不可变发布：清单摘要、构建证据、二进制与前端摘要、上一版本）、**system.activation**（排空、切换、健康与回滚证据）。
- 新增 **内核闸门**：路径拒绝列表（契约、提交管线、日志/重放/快照、接受结果）在保存与构建两处强制，AI 与人同样受限。
- 新增 **platform-updater** 独立进程：物化已批准 release 到独立工作树、构建宿主与工作区、启动新版本体检、排空切换、失败自动回滚；宿主不编译、不重启自己。
- 新增 **AI 入口**：`system.architect` 智能体以受管主体读取/检索/写入临时工作树，产出 `system.change`；模型走现有 AI 提供方，预算与轮数受限，不能批准自己的提案。
- 新增 **人工入口**：system 租户的单工作区提供 Platform 系统作者入口，定义继续复用原 Build 草稿与通用决定表单；System changes 展示冻结文件前后内容、选中定义、构建日志、批准与可取消的切换窗口，源码编辑与定义可放入同一提案。旧 Studio/Catalog 包已退出，不保留第二套编辑与发布实现。
- **删除**：行业应用包与 solution 宿主、无消费者的集成驱动与专用呈现、UAF 企业建模、Lean 形式化、组件 Catalog 与旧工作区，以及对应测试与脚本。文件/关系等历史 owner 的删除仍须先满足恢复与迁移责任；这部分以及完整页面运行时仍在原 tasks 中未完成，不能据核心链路可用宣称整个 change 已完成。

## Capabilities

### New Capabilities

- `platform/self-hosting`：系统作用域、元对象投影、源码记录、内核闸门、变更与发布、构建与激活、两个入口、恢复与导出。

### Modified Capabilities

- `runtime/host`：system 租户复用提交与接受；系统激活决定是接受结果，重放不重建。
- `runtime/deployment`：独立 updater、宿主本机构建、有窗口切换、回滚与保留策略。
- `runtime/access-control`：system.builder / system.publisher 与 AI 提案主体的分离，系统定义对其他租户只读。
- `platform/application-lifecycle`：系统变更复用草稿→候选→发布→激活；系统源码与受控代码的边界。
- `platform/business-models`：企业模型收敛为最小组织目录（单位、结构、任职）。
- `platform/ai-knowledge`：移除知识与派生内容能力，保留模型治理、函数与智能体。
- `experience/workspace`：Platform 应用、System changes 视图、源码编辑与元对象编辑共用草稿。

### Removed Capabilities

- `platform/integrations`：连接、来源、数据集、管道、回写、匹配、观测、确认、设备协议与协议包全部退出。
- `experience/professional-visualization`：地图、三维、图像标注与专用呈现退出。

## Impact

- **权威与数据**：新增 system 租户的日志与正式状态；其他租户数据、历史与在途版本不变。系统发布只追加，不重写历史。
- **部署**：新增 updater 进程、构建工作树与二进制/前端保留目录；`make lite`、`make local`、`deploy/local/update.sh` 的启动形态随之调整，数据卷、密钥与租户配置保留。
- **安全**：AI 首次获得写源码的能力，靠路径闸门、临时工作树、预算与人工批准约束；新增依赖需联网解析并记录进 release。
- **删除**：约 10 万行代码与大部分测试退出；`platform/integrations`、`experience/professional-visualization` 两个模块规范移除，`business-models` 与 `ai-knowledge` 收缩。
- **验收**：需要四次真实演练（人改元对象、AI 改源码经批准生效、构建失败与自动回滚、数据卷与修订一致）；仅跑单元测试不能作为交付证据。
