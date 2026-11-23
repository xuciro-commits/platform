# Platform

一个业务应用平台：声明业务对象，就得到能用的应用；行业知识做成能安装、能升级的产品包。

- 内核的模型向 Palantir Foundry 看齐：业务对象，动作是唯一的写入，处处受治理，每次改变都可追溯。
- 应用框架的体验要胜出 Frappe：声明一个类型，就得到现代的列表、看板、记录页、录入、权限与接口，不写前端代码。
- 行业应用不进平台代码：制造、酒店、供热这些行业的概念，只在产品包里用对象与动作表达，经市场安装与升级。

## 现在到哪一步

还没有客户在用。当前阶段是“类型即应用”。阶段、门槛、下一步与先不碰的东西，都在[长期规划](docs/plan.md)里；别处不写进度。

## 跑起来

需要 Go 1.27、Node.js 22.18 以上、pnpm 12。

```bash
pnpm --dir frontend install
```

```bash
make frontend-host
```

`make frontend-host` 起一个内存里的宿主（`http://127.0.0.1:18511`），自带一个示例销售项目。
在 `frontend/.env` 里写一行 `PLATFORM_HOST=http://127.0.0.1:18511`，然后另开一个终端：

```bash
pnpm --dir frontend dev
```

打开 [http://localhost:8496](http://localhost:8496)，在登录页选 `admin-1`。

带 PostgreSQL、对象存储与登录服务的整套环境用 `make setup` 和 `make local`，需要 Docker，说明见[本地运行](deploy/local/README.md)。
本地账号保存在 `.build/local/`，不要写进代码或提交。

## 文档

十份，按问题分。规矩见[文档地图](docs/README.md)。

| 想知道 | 读 |
|---|---|
| 为谁、凭什么赢、什么不做、哪些决定不能动 | [意图](docs/intent.md) · [PRODUCT.md](PRODUCT.md) |
| 现在在哪、下一步、什么先不碰 | [长期规划](docs/plan.md) |
| 系统的组成，写入、读取、发布的路径 | [架构](docs/architecture.md) |
| 资源、类型、对象、动作、权限、产品包 | [领域模型](docs/domain.md) |
| Go 代码的位置与规矩 | [后端](docs/backend.md) |
| 界面代码的位置与做法 | [前端](docs/frontend.md) |
| 导航、页面、操作、文字 | [界面与交互](docs/ux.md) |
| 观感与禁忌、视觉令牌与规约 | [审美](docs/design.md) · [DESIGN.md](DESIGN.md) |
| 名字与翻译 | [术语表](docs/glossary.md) |

## 仓库

```text
capabilities/server   宿主：内核、服务、HTTP 入口（Go）
contract              内核协议（protobuf）
frontend              前端：由类型生成界面（React、Astryx）
deploy/packages       产品包的源与制品
deploy                本地、容器与无 Docker 的部署
scripts               检查、生成、打包
docs                  十份文档
```

```bash
make help
```

代理与人共用的工作约定在 [AGENTS.md](AGENTS.md)。
