# 设计：拆除设备链与旧租户迁移机

## 拆除边界判定

两条链按负责人裁决整体拆除，边界按下述判据划线：

1. **设备连接链**（gateway + modbus/s7 驱动 + 协议包装配）：只服务已放弃的设备 I/O 场景，其规格要求独立网关进程、驱动生命周期、协议 profile 授权与在途设备动作核对——整套机器没有任何其他在用消费者，属反模式过度工程，全删。
2. **旧租户迁移链**（hotel/manufacturing 迁移、legacy 兼容、旧配置、日志保留义务）：旧租户数据已裁决放弃（"以后也不用伺候它们"），迁移机失去服务对象，连同兼容承诺一并删除。
3. **保留线**：外部派发与回读是通用能力（`dispatch_effects.go` / `dispatch_readback.go` / `observations.go`），非设备专用；`internal/construction` 的连接与来源建模服务通用数据接入，均保留。设备专用措辞在规格里改写为"外部动作/外部执行"的一般语义。

## 规格同步做法

- 差量以 `REMOVED 5 / ADDED 1 / MODIFIED 5` 表达：删除五条设备不变量（协议包隔离、配置切换冻结、驱动资源释放、驱动替换核对、非幂等设备动作），新增一条去掉"设备"限定的派发规则，五条修改把设备措辞泛化为外部系统。
- 主规格 `platform/integrations` 在实施时直接合并差量内容；归档前核对：五条 REMOVED 已不在主规格，ADDED 已在，五条 MODIFIED 与差量逐字一致，实现定位行删除已不存在的 `protocol_connections.go`、`driver/`、`gateway/` 路径。

## 实现处置

- **Go**：设备与迁移代码整体删除；受影响的两个测试重写为等价的通用场景（不保留设备前提），其余用 `-skip` 基线核对；`go build`、`go vet`、`boundaries.sh` 全程保持通过。
- **Web**：删除 `migration.tsx` 与 `e2e/tests/migration.spec.ts`，清掉 release/changes/selection/studio-model/application-runs 中的迁移入口；71 个迁移相关 i18n 键按 HEAD 白名单删除后重扫零孤儿。
- **目录页与目录资产**：`catalog.json` 重新生成，`check-web` 复核。
- **边界**：`scripts/boundaries.sh` 十个平台 owner 不变；设备删除不引入新的逃逸。

## 不做的事

- 不为满足删除数量绕过规格：规格差量与实现同步逐条核对，`check-specs`（含归档校验）通过。
- 不动 accepted_* 提交管线、`platform/` API、`apps/build`、journal、计算隔离对、idp、contract/lean、企业 UAF 链。
- 不保留网关、驱动、协议包的任何兼容入口或退役适配层——链是整体死亡，不是逐步下线。
- 浏览器路线未在本沙箱执行（无浏览器）：相关验证缺口在 tasks 6.3 如实注明，不以其他证据冒充。
