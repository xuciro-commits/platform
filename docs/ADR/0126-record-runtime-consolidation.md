# ADR-0126 — XXL：记录运行时与授权查询收敛

状态：接受执行（2026-10-17），归 ADR-0125 XXXL。只规范实现收敛，不改变外部记录/API 契约。

## 问题与唯一归属

`capabilities/server/records.go` 把记录存储/历史、查询谓词、权限投影、HTTP 记录视图、企业引用和个人审计组织在一个大文件内；其他层还存在记录编码、字段投影、查询作用域及复制操作。文件大小本身不是分包理由，真正的问题是记录值、授权视图、查询和 API 映射的边界容易重复执行。

- `recordStore` 是租户记录值、不可变行/历史、引用索引与 staged promotion 的唯一 owner。
- `platform` 查询语义负责声明式查询输入；宿主记录查询 owner 只编译一次，并对当前授权投影执行，不能在 UI/另一服务复制筛选或排序语义。
- `Tenant` 读取层是 member 可见性、字段遮蔽和引用投影的唯一 owner。HTTP 层只序列化其结果，不自行补授权。
- 事务/恢复的历史输入仍走原保存字节；读模型/索引是可重建投影，不成为新的写入权威。

## 施工决定

1. 追踪 `recordStore.put/get/check/find`、`Tenant.Records/RecordOf/Readable` 和字段投影的所有调用，画出一条数据路径；同一授权计算只保留一个主人。
2. 清除同一操作里重复 JSON 编解码、重复反射字段解析和不必要的深拷贝。可直接读不可变记录时不复制；向调用者暴露可变值或需遮蔽时才复制。不得共享可变 slice/map。
3. 先将可纯化的查询编译与 recordStore 访问分离，再决定是否文件拆分；不得建立另一个查询缓存/权限索引。现有 `recordIndex`/引用索引只在写入及 schema 替换处维护。
4. 检查 `records.go` 的热点与 `recordStore` staged fork：保留 copy-on-write 和锁顺序；不得为减少扫描让授权前的候选结果泄漏。
5. 迁移全部消费者后删旧 mapper/helper。只有权限差异或历史版本明确要求的路径保留，并由测试覆盖。

## 不变量

租户隔离；角色/字段/AI input 遮蔽；不可枚举未授权记录；作用域查询先于返回；稳定分页排序；归档/业务时间；修订冲突；引用限制；immutable history；已接受结果只应用原映像；replay 不重新裁决。

## 首批实现

`Tenant.RecordOf` 的 comments/files/processes/tasks/approvals 原先各自重复构造 domain JSON、调用授权 Records 查询、处理拒绝结果；现在集中到 `detailRecords`，owner 仍是原 Tenant 记录读路径，错误仍按“此附属列表不可用即留空”处理。`Held` 与 `recordStore.get` 之前对同一 `recordIndex` 做两次 ID 查询，现在只取一次 row。此切片尚未完成 RecordOf 的历史、关联与活动职责收敛，也未证明更大规模性能改善。

## 完成条件

一个端到端记录读写路径唯一且可定位。针对至少 1k、10k 行的实体记录测量查询、单记录读取、staged write/fork 的 CPU、分配和延迟；固定 workload 与环境，不把模拟基准写成生产容量。结果语义测试覆盖有/无角色、字段遮蔽、归档、scope、排序/分页、旧历史恢复。code-size 记录本主题净 handwritten production 变化。

## 先行核对

记录当前已有 `recordIndex`、reference indexes、member-view filtering、copy-on-write promotion，不能另造相同设施。`records.go` 目前包含约 1,800 行，拆成多个文件前先完成职责映射；纯搬迁不计进度。