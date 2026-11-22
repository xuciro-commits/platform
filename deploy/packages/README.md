# 产品包作者态源

每个目录是一个产品包的作者态源（格式 v2：YAML，一个资源一个文件）。
顶层的 `*.json` 是已发布版本的制品快照，宿主由 `PLATFORM_PACKAGES_DIR` 读取；不手改，也不从源重建。

```text
crm/
├── package.yaml            id、version、标题、简介、依赖、roots；artifact 指向同版本的快照
├── package.lock.json       生成：每个资源的摘要、contractVersion、sourceVersion，对象的 schemaVersions 历史
├── setup.yaml              可选：初始化步骤与角色要求
├── docs/guide.md           可选：包的说明
├── objects/<对象>/
│   ├── object.yaml
│   ├── page.yaml           与对象同名的页面
│   └── actions/<动词>.yaml  以这个对象为目标的动作
├── applications/  flows/  pages/  actions/  queries/  functions/  agents/
├── integrations/{connections,sources,listeners,writebacks,matching,...}/
└── tests/{sample-sets,test-plans,evaluations}/
```

- 资源的标识由位置与内容得出：目录定种类；名字取 `name`，对象取 `namespace.name`（没有命名空间时 `build.name`），动作取 `schema`。源里不写 ref。
- 依赖、提供的能力与动作输入的模式由打包器用宿主的同一实现算出，写进制品，不进源。
- `schemaVersions` 只在锁文件里。改了对象的模式，`make packages` 追加一条；与上一版不兼容时升一个族。没有历史的旧对象保持没有。
- `make check` 第 3 步：每个包能打成宿主接受的制品，锁文件没有过期，版本号没变时与快照等价（宿主读到的一样）。

```bash
make packages
cd capabilities/server && go run ./cmd/pkg build -o /tmp/crm.json ../../deploy/packages/crm
cd capabilities/server && go run ./cmd/pkg split ../../deploy/packages/crm.json /tmp/crm
```

## 包边界

包边界就是归属。每个包都守的规矩在 `docs/domain.md`（“产品包”）；这一节记每个制造包拥有什么。

| 包 | 拥有 |
|---|---|
| `manufacturing.core` | 共用的供应商询价对象、连接、回写与查询。它的 Foundation 应用装这些共用的询价视图，不含采购、会计与车间页面 |
| `manufacturing.erp` | 物料、采购、库存、会计、生产订单，以及库存与会计过账 |
| `manufacturing.mes` | 工艺路线、工单执行、质量、设备、停机，以及车间动作与执行记录 |

- 依赖是 `erp -> core` 与 `mes -> core`。ERP 与 MES 互不使用对方的内部资源；MES 经对象接口、动作或带版本的查询绑定上游生产事实。
- ERP 拥有 `productionbatch`、`erpreceipt`、`erpmove`、`erpstock`、`erpproductionposting`、`erpentry` 与 `erpline`。
  收货批次、移动库存、生产与会计过账，都发生在同一条 ERP 接受结果里。
- MES 拥有车间执行，以及 `mesbatch`、`mesissue`、`messignature` 与 `mesproductionresult`。从 MES 完工到 ERP 收货是最终一致。
- MES 经 `mesproductionresult.send` 回写 ERP 的 `productionorder.confirm`。
  不新增发件箱、网关或分布式事务协调者；已收到的批次永不以新的幂等键重试。
- ERP 经具名查询 `productionorderupstream` 与 `productionbatchupstream` 给 MES 生产订单与批次。MES 不轮询 ERP 的内部记录地址。
