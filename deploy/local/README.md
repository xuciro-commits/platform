# 本地运行

需要 Go 1.27.1、Node.js ≥ 22.18.0、pnpm 12.5.1、Python 3；完整环境需要 Docker。

```bash
make setup
make local
```

单一入口为 [http://localhost:8495](http://localhost:8495)，页面、应用、API 与登录共用此地址。`make local` 启动 Air、Vite、隔离计算进程及基础服务容器，日志在 `.build/dev/`。

首次启动自动生成私有配置。账号密码见 `.build/local/credentials.md`，机器可读账号见 `.build/local/accounts.json`。默认账号为 `admin@platform.test`、`builder@platform.test`、`user@platform.test`，密码各不相同。新环境没有预装业务包、业务授权或 AI 提供方。

配置保存在忽略的 `deploy/local/.env` 与 `.build/local/`。重复启动复用原租户、账号和密钥；不得删除配置或数据卷来修复启动问题。不要手改 bootstrap 校验的配置文件。

`make local` 与 `make local-update` 在平台就绪后由 `deploy/local/identity.py` 写入平台的身份服务设置：`platform-web` 客户端的主题与标记（色值取自 `web/packages/ui/src/themes/tokens.css`），以及平台登录表单使用的直接登录流程——凭据直接送到身份服务换取令牌，不经过平台后端；启用 MFA 或通行密钥的账号仍走 SSO。品牌访问键由 bootstrap 生成（已有环境在下次启动时补写），身份服务只在空数据卷把它写入数据库，因此在本功能之前创建的数据卷要重建一次才生效（账号与密码按 `.build/local/` 原样重建，仅登录会话与 JWKS 重新生成）：

```bash
source deploy/local/.env && docker compose --env-file deploy/local/.env -f deploy/local/compose.yaml stop rauthy \
  && docker volume rm "${PLATFORM_DATA_NAMESPACE}_rauthy"
```

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


## 注册与邀请

完整部署可显式设置 `PLATFORM_IDENTITY_OPEN_REGISTRATION=true`（Rauthy 注册入口）与 `PLATFORM_SELF_SERVICE=1`（用户自助创建工作区），默认关闭；不修改 bootstrap 生成的私有配置。重启相关服务后生效。
Rauthy 的验证邮件通过部署环境的 `SMTP_URL`（主机名）、`SMTP_PORT`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`SMTP_FROM` 配置；Platform 邀请使用 `PLATFORM_IDENTITY_SMTP_URL`（如 `smtp://user@mail.example.com:587`）、`PLATFORM_IDENTITY_SMTP_FROM`（裸邮箱）、`PLATFORM_IDENTITY_SMTP_PASSWORD`。邀请链接采用 `PLATFORM_PUBLIC_URL`。不把密码放进 URL 或仓库。仅隔离测试可启用 `SMTP_DANGER_INSECURE=true` 与 `PLATFORM_IDENTITY_SMTP_ALLOW_PRIVATE=1`。
已有工作区从切换器进入 Workspaces，零工作区用户登录后直接进入该页；受邀者用对应已验证邮箱登录并接受。未配置投递通道不代表邮件已送达；管理员可重发邀请或撤回。自助开通当前不含起步包自动安装及失败后的持久化恢复。

## 身份与入驻

- 身份提供方验证登录身份；`/v1/auth/me` 不要求租户成员关系。选择工作区后才进入租户业务授权。账号注册、密码与邮箱验证由身份提供方负责，平台不另存密码。
- 宿主启用自助开通时，已认证的用户可创建工作区，首位管理员由宿主从当前认证身份推导，不接受请求指定另一主体。
- 添加已有账号与邀请是不同动作：添加建立成员关系，不继承来源工作区权限；邀请先建立草稿并配置预授角色，发送后七天内由对应认证邮箱显式接受才激活。撤回、过期或草稿均不提供访问权；接受和撤回进入接受日志。历史无邀请凭证的成员保持原行为。
- 身份邀请使用宿主专用邮件端点（未配置时沿租户邮件通道）；收件人为登录身份邮箱，不取可编辑的联系邮箱。直接撤销、停用或移除最后一名管理员不得导致工作区无管理员。
