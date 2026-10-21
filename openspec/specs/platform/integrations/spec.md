# 数据接入与外部协同 Specification

## Purpose

规定逻辑连接、来源、版本化数据集、管道、效果投递、业务确认、设备派发、观测和协议网关的协作边界。它们复用正式发布、当前授权、Work 与接受结果；技术通信事实不能自动变成业务事实，不新增连接存储、调度器或业务执行权威。

通用接受与提交未知见 [宿主](../../runtime/host/spec.md)，工件与包生命周期见 [应用生命周期](../application-lifecycle/spec.md)，身份与派生数据保护见 [授权](../../runtime/access-control/spec.md)。本模块不提供跨系统 exactly-once、工业实时控制、安全联锁或未经验证的真机兼容承诺。

实现定位（仓库根相对路径）：`capabilities/server/internal/construction/connection.go`、`capabilities/server/internal/construction/source.go`、`capabilities/server/sources.go`、`capabilities/server/pipelines.go`、`capabilities/server/confirmation_reconcile.go`、`capabilities/server/dispatch_effects.go`、`capabilities/server/dispatch_readback.go`、`capabilities/server/observations.go`、`capabilities/server/observation_decision.go`、`capabilities/server/protocol_connections.go`、`capabilities/server/driver/`、`capabilities/server/gateway/`。

## Requirements

### Requirement: 连接分离逻辑定义与部署凭据

连接 SHALL 定义访问种类、逻辑用途、类型化配置及环境绑定需求，实际地址与凭据由环境提供。外部读取 MUST 遵守原网络、路径、版本与当前权限边界；同宿主来源也不能凭内部身份绕过授权，产品配置不接受任意可执行命令或密钥明文替代引用。

#### Scenario: 应用迁往另一城市

- **GIVEN** 应用使用稳定逻辑连接和设备身份
- **WHEN** 管理员为新环境绑定不同网关地址和密钥名
- **THEN** 业务页面与规则继续引用原逻辑身份
- **AND** 来源环境凭据不进入导出工件

### Requirement: 来源与管道区分记录、执行与内容身份

Source/Pipeline SHALL 为一次主动执行保存独立身份及固定定义、执行者和输入版本。内容摘要只校验同一执行的内容一致性，MUST NOT 将相同内容视为同一新事件；行操作身份从原执行及行身份产生，创建或更新选择在准备阶段固定。

#### Scenario: 同一记录内容 A 到 B 再到 A

- **GIVEN** 同一目标在三个主动来源执行中依次出现 A、B、A
- **WHEN** 系统处理第三次执行
- **THEN** 第三次是新的操作，不复用第一次 A 的历史答复
- **AND** 同次执行的失败重试仍使用原行身份

### Requirement: 部分接入可恢复且不跳过失败游标

接入执行 SHALL 保存可重读的固定输入及已接受的原行答复；重启后继续原执行，不重新读取变化内容冒充重试。未提交行 MUST 保持待处理，业务拒绝被记录为原答复；仅完整成功才推进成功游标，无法继续旧输入时需明确结束旧执行再开始新执行。

#### Scenario: 部分行完成后进程重启

- **GIVEN** 一次来源执行中前几行已接受，后续行尚未提交
- **WHEN** 宿主恢复并继续执行
- **THEN** 已完成行沿原键得到原答复，剩余行读取原固定输入
- **AND** 不因重拉上游变化内容或跳过拒绝而推进游标

### Requirement: 管道消费明确的数据集版本集合

管道 SHALL 固定主输入及 lookup/join 等全部数据集的确切版本，按发布的纯变换生成结果和隔离行。输出业务记录仍通过原对象 create/edit 决定；输出数据集形成原版本化结果。MUST 保留来源标记和失败原因，不将管道视为数据库或直接写入旁路。

#### Scenario: 重试期间 lookup 数据集已更新

- **GIVEN** 待完成管道固定了主输入和 lookup 的原版本
- **WHEN** lookup 发布新数据后重试原执行
- **THEN** 重试使用原固定结果或输入，不切换到最新 lookup
- **AND** 主动再次运行才建立新的执行与输入版本集合

### Requirement: 外部投递只使用冻结效果

所有对外业务写入 SHALL 在原决定中保存冻结请求、稳定身份和版本，再经原 Effect 机制投递与保存 outcome。MUST 区分 delivered、retry、rejected、unknown 及回调错误；接受前不得发出效果，恢复不得重新生成或重发外部请求。

#### Scenario: 外部写入成功但答复丢失

- **GIVEN** 已保存效果的请求可能已到达接收方
- **WHEN** 发送方未获得确定答复
- **THEN** 结果按原策略进入 unknown 或原键核对/重送
- **AND** 不生成新业务键冒充一次确定失败的重做

### Requirement: 业务确认具有独立证据与终结

Confirmation SHALL 记录各参与方接受、拒绝、未知、对账差异及人工裁定，不能由 HTTP 成功推导业务确认。状态回读 MUST 使用固定来源和预算，并在锁外读取后复核运行前驱、当前权限与配置；相同轮询结果不无限追加相同业务事实。

#### Scenario: 对账读取期间执行者被撤权

- **GIVEN** 已授权执行者发起一次外部状态读取
- **WHEN** 读取完成前权限被撤销
- **THEN** 回锁复核阻止把该读取变成新的业务确认
- **AND** 原差异与已有裁定不被覆盖

### Requirement: 危险设备动作未知不得自动重发

Dispatch SHALL 区分投递、设备执行、回读结果和人工结束。不能安全重复的动作 MUST 在实际发送前保存未知边界，崩溃、超时或取消等待不得使其恢复为可自动发送；继续处理依靠声明的回读或有权人工决定，补偿也是另一个有独立证据的动作。

#### Scenario: 非幂等设备动作发送后崩溃

- **GIVEN** 派发已进入一次非安全重发尝试
- **WHEN** 宿主在收到设备结果前退出并恢复
- **THEN** 原派发保持未知并等待回读或人工处理
- **AND** 不从普通重试队列再执行同一物理动作

### Requirement: 原始观测留在外部存储

Observation SHALL 通过有界读取取得明确设备、信号、单位和时间范围的数据，只有规则实际使用的摘要、来源与决定证据进入业务接受结果。MUST 保留采样时间、质量与映射修订，失败或缺失不表示数值零；平台不下载所有原始点重建另一套时序存储。

#### Scenario: 设备断线后只有旧读数

- **GIVEN** 外部存储返回最近值但质量或时间不满足当前读取条件
- **WHEN** 界面展示或设备完成规则使用该读数
- **THEN** 界面保留时间与质量说明，业务规则按声明拒绝不合格证据
- **AND** 不将旧值补成新样本或将失败补为零

### Requirement: 完整窗口必须有覆盖证据

协议来源及声明 requireComplete 的观测 SHALL 检查保留范围、窗口完成时间、截断与质量。MUST 拒绝将只有汇总数值的结果当作完整窗口；旧不可变发布保持其原解释，新发布使用当前声明，不反写旧证据或复制求值器。

#### Scenario: 请求窗口仍在等待迟到数据

- **GIVEN** 汇总值已返回，但覆盖元数据尚未确认窗口完成
- **WHEN** 规则要求完整窗口作出决定
- **THEN** 系统明确报告未就绪或不合格
- **AND** 不提前接受完整窗口业务结论

### Requirement: 协议包复用安装而隔离设备 I/O

协议包 SHALL 复用原包许可、确切版本、制品摘要和类型化配置，在独立网关/驱动进程执行技术 I/O。包版本、接口版本、配置修订及设备兼容 MUST 分别核验；只列出实际支持的目标与操作，安装不等于授权设备写入，也不意味着进程是恶意代码沙箱。

#### Scenario: 只读协议 profile 被用于写命令

- **GIVEN** 已安装包只声明读取和采样
- **WHEN** 连接或派发要求写入设备
- **THEN** 原配置或执行检查明确拒绝
- **AND** 不通过通用 HTTP 写入口绕开 profile 能力

### Requirement: 配置切换保留在途请求与旧回读

派发 SHALL 冻结包版本、配置修订、命令映射及尝试身份。切换连接 MUST 停止旧配置的新写入，并保留在途请求需要的原回读映射；不能满足旧会话读取时推迟切换或显式人工结束，不能将旧修订重定向到新地址。取消仅停止等待，不证明网络报文或物理动作撤回。

#### Scenario: 更换点位地址时仍有未知派发

- **GIVEN** 原派发依赖旧配置修订的设备和点地址
- **WHEN** 管理员发布新的连接修订
- **THEN** 原派发继续用旧映射回读，或明确保持未知并阻止不安全切换
- **AND** 新配置不重新解释原指令，也不自动漂移写入节点

### Requirement: 跨系统协同保持原权威与诚实边界

外部协议协同 SHALL 通过版本化请求、答复与补偿，由各方保存自己的接受事实；网关只返回技术通信事实，不创建告警、工单或业务决定。MUST 区分模拟服务、协议子集、真实设备及客户交付证据；平台不提供跨权威原子提交或安全控制回路。

#### Scenario: 网关命令返回成功

- **GIVEN** 网关报告报文已发送或协议已应答
- **WHEN** 系统展示业务处理进度
- **THEN** 该结果只满足对应投递阶段
- **AND** 设备完成和业务确认仍等待各自声明的证据
