# Platform

业务应用构建平台，提供数据接入、建模、动作、流程、AI、界面与发布能力。

需要 Go 1.27.1、Node.js ≥ 22.18.0、pnpm 12.5.1、Python 3。完整运行需要 Docker。

```bash
make setup
make local
```

入口为 [http://localhost:8495](http://localhost:8495)，应用、API 与登录共用此地址。账号保存在本地 `.build/local/credentials.md`。

```bash
make docker
make lite
make infra-down
```

`make docker` 使用容器运行；`make lite` 无需 Docker，使用独立数据目录和签名角色卡；`make infra-down` 停止容器并保留数据。不同模式不能同时占用同一端口。只有 Corepack 时，在命令后加 `PNPM="corepack pnpm"`。

配置与启动细节见 [本地运行](deploy/local/README.md)，可用命令见 `make help`。
