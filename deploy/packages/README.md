# 产品包作者态源（开发与测试夹具）

这里只保留平台开发与自动化验收用的内置夹具包（格式 v2：YAML，一个资源一个文件）：
- `application.production-provider`：用于内核不变量与构建者端到端验收（安装、贡献动作、升级、退场、下线与重装保留记录）。
- 旧 CRM 0.1.x 已移到 `../test-packages/`，仅浏览器主干测试显式加载，不再随普通宿主市场提供。正式 CRM 只从官方包库安装。

所有正式社区与官方业务包（`crm`、`helpdesk` 等）统一维护在独立仓库 [platform-packages](https://github.com/xuciro-commits/platform-packages)，
宿主市场默认内置该官方仓库地址并自动加载，同时支持注册多个第三方仓库。历史示例包（`heating`、`hospitality`、`manufacturing.*`、`production-consumer`）已归档于 Git 标签 `archive/legacy-packages`。

顶层的 `*.json` 是夹具的已发布制品快照，宿主由 `PLATFORM_PACKAGES_DIR` 读取；不手改，也不从源重建。

```bash
make packages
cd capabilities/server && go run ./cmd/pkg try -script /tmp/crm-try.yaml ../../deploy/test-packages/crm
```
