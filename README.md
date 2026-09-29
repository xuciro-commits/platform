# 平台

**AI 业务应用平台**：内核契约 (`contract/`)、Go 宿主运行时 (`capabilities/server/`)、Web 工作区 (`web/`)、参考应用 (`apps/`) 以及跨行业解决方案 (`solutions/`)。具备多租户能力并在业务领域演进中保持稳健 (ADR-0003, ADR-0025)。其下一阶段赋能 FDE 与客户通过共享语义、受管 AI 以及代码或受控可视化编排来发展行业应用 (ADR-0031)；当前的开发路径仍为类型化的 Go 与 TypeScript。

主要业务目标应用为 **CRM、MES 和 ERP**，PMS、HCM 和 CSM 作为参考应用。

## 文档索引

- **面向 AI 编程助手：** 从 [AGENTS.md](AGENTS.md) 开始（Claude Code 通过 [CLAUDE.md](CLAUDE.md) 访问）。
- **本地环境与运行：** 参阅 [deploy/local/README.md](deploy/local/README.md)，了解 Docker Compose、Rauthy OIDC、账号密码及服务端点。
- **产品意图与原则：** [docs/Intent.md](docs/Intent.md)（能力主导、标杆对齐、应用作为探针）。
- **架构与能力地图：** [docs/Platform.md](docs/Platform.md)（当前分层、重放语义与假说 K1–K9；§10 为权威的下一阶段设计、历史审计、年度路线图与验收标准）。
- **测试场景：** [docs/Testing.md](docs/Testing.md)（跨业务场景验证的平台能力保证）。
- **活动计划与摩擦力：** [docs/WorkQueue.md](docs/WorkQueue.md)（唯一的活动任务列表）。
- **架构决策记录：** [docs/ADR/](docs/ADR/)（具有持久成本的决策；[ADR-0031](docs/ADR/0031-ai-application-platform.md) 确立了 AI 应用平台方向）。
- **应用编写指南：** [docs/Apps.md](docs/Apps.md)（当前的脚手架、实体、动作、工作流、翻译与运行；与未来 FDE/客户构建工具的边界）。
- **内核契约：** [contract/spec/](contract/spec/)（K1–K9 语义、错误代码、一致性测试向量）。

## 快速验证

AI 的阅读与批次规则见 AGENTS；当前项只看 WorkQueue，检查按 [Testing 的影响面选择表](docs/Testing.md#检查选择与停止)。下方是常用入口，不要求每批全部运行。

```sh
scripts/verify.sh ci          # 持续集成测试套件
scripts/verify.sh web         # Web 工作区与 UI 测试
scripts/verify.sh composition # 应用边界与布局检查
```
