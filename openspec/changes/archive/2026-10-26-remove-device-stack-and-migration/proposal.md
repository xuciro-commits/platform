# Proposal

## Why

平台后端审计（负责人 2026-10-25 拍板）判定两套子系统属于"为设计而设计、实际不起作用"的负资产，应当整体移除：

1. **设备接入链**（协议网关 + modbus/s7/simulator 驱动）：网关进程不在任何默认部署拓扑中（compose 无此服务，仅手动 `make gateway` 与一条 e2e 使用），与已关闭的旧制造业务绑定；负责人拍板"这能力不要了"。保留的通用连接（http/odata/postgres）、应用间版本化协议、派发/回写/确认机制不受影响。
2. **旧租户数据机器**（migration_* + legacy_*）：专门伺候旧酒店/制造租户的搬家与兼容逻辑；负责人拍板"旧数据直接丢弃"，兼容义务随之消失。同时清掉全仓无人引用的旧示例（`deploy/heating/`、`deploy/examples/heating/`）与文档里指向不存在目录 `deploy/packages/` 的撒谎边界。

**负责人同轮拍板明确保留**：Lean 形式化证明（contract/lean）、企业架构建模（UAF 链）、试算（simulate）、发布评估（agent_eval）。

## What Changes

- 删除 `capabilities/server/{gateway,driver,drivers}`、`cmd/{gateway,driver-modbus,driver-s7,driver-simulator}` 及其宿主接线：驱动绑定校验、协议连接拨号、包索引中的驱动制品、派发/回写中的设备分支、协议调用路由。
- 删除 `platform/protocol_driver.go`、`platform/driver_lifecycle.go` 等设备类型与 `internal/host` 驱动角色；`construction.Connection.Protocol` 与写回请求的设备字段移除。
- 删除 `migration_*`、`legacy_*` 及接受结果中的旧格式回退分支；宿主不再保留任何旧租户兼容读取路径。
- 删除 `make packages` / `make gateway`、`scripts/package-artifacts.py`、e2e 的 `protocol-services.mjs` 与 `protocol-packages.spec.ts`、`deploy/examples/hvac` 设备夹具、无人引用的旧示例目录。
- OpenSpec：`platform/integrations` 移除设备相关需求并改写受影响条款；`runtime/host`、`platform/application-lifecycle` 同步迁移与兼容条款；Architecture、doc.go、cmd/README、AGENTS 代码地图同步。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `platform/integrations`：移除协议包/驱动/网关/设备 I/O 需求；保留连接、来源、外部投递、回写、确认、观测（改写设备措辞）。
- `runtime/host`：移除旧输入兼容读取与迁移相关义务。
- `platform/application-lifecycle`：移除租户迁移/退役搬家路径中依赖旧数据的条款（如有）。

### Removed Capabilities

- 设备协议接入（协议网关进程、驱动子进程模型、modbus/s7/simulator、协议连接配置与设备命令执行）。

## Impact

`capabilities/server`（根包、platform/、internal/construction、internal/host、apps/build）、`web`（连接编辑中的协议绑定 UI 与 i18n、e2e）、`Makefile`、`scripts/`、`deploy/examples`、`docs/Architecture.md`、openspec 规范。既有租户数据卷按拍板直接丢弃，不提供迁移路径；8490/8495 双入口、通用连接与应用包安装（蓝图）语义不变。
