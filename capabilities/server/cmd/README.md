# cmd 分类清单

命令按实际生命周期分类，目录数量不代表默认服务数量。当前产品只有一个Platform入口，应用、API与登录共用默认8495；隔离执行通过内部socket接线，不为业务应用增加宿主或固定端口。

## 宿主与隔离执行

| 程序或目录 | 职责与部署归属 |
|---|---|
| `platform-server/` | 唯一平台宿主，按部署配置监听8495；仅显式配置`PLATFORM_SINK_ADDRESS`时内嵌本地调试替身 |
| `wasm-worker/` | 独立受控WASIp1执行进程，经Unix socket接受原命令/数据ABI；不承担浏览器规则求值 |
| `code-builder/` | 固定命令驱动，唯一获准创建离线编译guest的受控构建进程，经Unix socket接线 |
| `compute/` | 构建上述执行镜像的Dockerfile目录，不是另一个Go服务；worker/builder的权限和故障边界保留 |

## 静态维护与调试

| 程序 | 职责 |
|---|---|
| `platform-updater/` | 监督原保留发布、探活、排空、回滚前驱、原回执核对及源码导出；`build/activate/watch`和`serve -watch`已冻结，不再消费新动态自举 |
| `install-bundle/` | 使用真实令牌沿原构建动作提交对象、页面与应用声明，不建立第二安装路径或独立服务 |
| `storage-files/` | 停机状态下核验原文件存储 |
| `webhook-sink/` | 本地替身的独立调试入口；默认产品部署不另启该服务 |

维护步骤、账号、地址和私有配置唯一说明见[本地环境指南](../../../../deploy/local/README.md)。监督状态和制品不能通过删除来解除未知回执。

## 生成与检查

| 程序 | 职责 |
|---|---|
| `api-types/` | 从真实宿主API生成Web契约类型及企业SDK |
| `check-boundaries/` | 检查导入、授权边界、文件预算、同名路径、视觉声明及模型协议归属；包含条件编译文件，存量基线只能递减 |
| `rule-browser/` | 将同一个纯规则实现编译为浏览器WASM，供本地公式/表单联动使用；构建入口为`scripts/rule-browser.mjs` |
完整UAF标准及生成/导入命令位于独立[可选工具](../../../../tools/platform-uaf-importer/README.md)，不参与默认宿主构建。原组织历史引用和旧输入交接仍由原owner负责。
