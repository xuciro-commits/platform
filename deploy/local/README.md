# 本地运行

需要 Go 1.27.1、Node.js ≥ 22.18.0、pnpm 12.5.1、Python 3；完整环境需要 Docker。

```bash
make setup
make local
```

单一入口为 [http://localhost:8495](http://localhost:8495)，页面、应用、API 与登录共用此地址。`make local` 启动 Air、Vite、隔离计算进程及基础服务容器，日志在 `.build/dev/`。

首次启动自动生成私有配置。账号密码见 `.build/local/credentials.md`，机器可读账号见 `.build/local/accounts.json`。默认账号为 `admin@platform.test`、`builder@platform.test`、`user@platform.test`，密码各不相同。新环境没有预装业务包、业务授权或 AI 提供方。

配置保存在忽略的 `deploy/local/.env` 与 `.build/local/`。重复启动复用原租户、账号和密钥；不得删除配置或数据卷来修复启动问题。不要手改 bootstrap 校验的配置文件。

| 命令 | 用途 |
|---|---|
| `make local` | 后台热更新，入口 8495 |
| `make docker` | 切回容器运行，源码须先提交 |
| `make local-update` | 更新已提交版本并核对健康、身份服务与页面资源 |
| `make infra-down` | 停止容器，保留数据卷 |
| `make lite` | 无 Docker 持久化运行，数据在 `.build/lite/platform/` |
| `make host ARGS='…'` | 编译并运行宿主，显式传入部署参数 |

热更新 API 在本机回环 18495，基础服务映射数据库 5433、身份服务 8480、对象存储 9000。浏览器只访问 8495。计算进程通过 `.build/dev/compute/` 下的 Unix socket 通信。

轻量模式沿原签名角色卡登录，与完整环境的密码登录不同。运行 `make lite LITE_HOST=0.0.0.0` 可供受信任网络访问；不要与其他模式同时占用 8495。备份轻量数据前停止宿主，一起保存日志、文件、快照和签名密钥。

运行态 AI 推断许可由启动环境的 `PLATFORM_AI_INFERENCE_POLICY` 指定，格式为 `{"runtime":[{"provider":"实际提供方ID","origin":"http://实际推断服务:8000","location":"edge"}]}`，`location` 为 `local` 或 `edge`。默认没有运行态推断许可；配置提供方本身不授予许可。
