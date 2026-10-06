# 平台

**AI 业务应用平台**，产品形态对标 Palantir Foundry/AIP：内核契约 (`contract/`)、Go 宿主运行时 (`capabilities/server/`)、Web 工作区 (`web/`)、验证应用 (`apps/`) 与跨行业组合 (`solutions/`)。

- AI 编程助手与贡献者规则：[AGENTS.md](AGENTS.md)
- 产品意图：[docs/Intent.md](docs/Intent.md)；现行产品结构：[ADR-0052](docs/ADR/0052-foundry-aligned-platform-experience.md)
- 架构与能力地图：[docs/Platform.md](docs/Platform.md)
- 当前任务：[docs/WorkQueue.md](docs/WorkQueue.md)
- 决策记录：[docs/ADR/](docs/ADR/)；应用编写：[docs/Apps.md](docs/Apps.md)；检查方法：[docs/Testing.md](docs/Testing.md)
- 本地运行：[deploy/local/README.md](deploy/local/README.md)；内核契约：[contract/spec/](contract/spec/)

```sh
scripts/verify.sh ci          # 持续集成组合
scripts/verify.sh web-check   # Web 类型、单元测试与构建
scripts/verify.sh composition # 应用边界检查
```
