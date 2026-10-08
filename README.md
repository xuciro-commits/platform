# 平台

**AI 业务应用平台**，产品形态对标 Palantir Foundry/AIP：内核契约 (`contract/`)、Go 宿主运行时 (`capabilities/server/`)、Web 工作区 (`web/`)、验证应用 (`apps/`) 与跨行业组合 (`solutions/`)。

- AI 编程助手与贡献者规则：[AGENTS.md](AGENTS.md)
- 产品意图：[docs/Intent.md](docs/Intent.md)；现行产品结构：[ADR-0052](docs/ADR/0052-foundry-aligned-platform-experience.md)
- 架构与能力地图：[docs/Platform.md](docs/Platform.md)
- 当前任务：[docs/WorkQueue.md](docs/WorkQueue.md)
- 决策记录：[docs/ADR/](docs/ADR/)；应用编写：[docs/Apps.md](docs/Apps.md)；检查方法：[docs/Testing.md](docs/Testing.md)
- 本地运行：[deploy/local/README.md](deploy/local/README.md)；内核契约：[contract/spec/](contract/spec/)

```sh
make help                     # 全部目标；三层：dev / check / release（ADR-0081）
make infra && make dev        # 基础设施容器 + 宿主热重启（另一终端 make web）
make check                    # 提交前：编译、vet、边界、生成类型、Catalog、tsc
make verify                   # 发布前完整证据；CI 在 v* 标签上跑同一条
```
