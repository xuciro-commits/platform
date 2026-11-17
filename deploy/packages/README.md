# 产品包作者态源

这里的每个目录是一份产品版本的作者态源。当前七个产品都用这个布局。
顶层的 `*.json` 是生成的 `PackageDescriptor` 制品，由 `PLATFORM_PACKAGES_DIR` 读取；不手改。

```text
manufacturing.mes/
├── package.json
├── blueprint.json
├── package.lock.json
├── setup.json
├── roles/requirements.json
├── objects/
├── actions/
├── queries/
├── pages/
├── applications/
├── flows/
├── functions/
├── agents/
├── integrations/{connections,sources,listeners,writebacks,...}/
├── tests/{sample-sets,test-plans,evaluations}/
├── docs/guide.md
└── assets/
```

只建含有资源的目录。可选的 `migrations/` 与 `assets/` 目录在包第一次需要它们时出现。

`package.json` 装描述字段，外加 `artifact`：生成的运行时文件名。
`blueprint.json` 装根资源与一份资源索引，每个资源一条路径。
`setup.json` 装初始化步骤，`roles/requirements.json` 装角色要求。打包器把它们合成运行时制品。

```bash
make packages
node scripts/package-source.mjs pack deploy/packages/manufacturing.mes
node scripts/package-source.mjs check deploy/packages/manufacturing.mes
```

把另一份已有的制品迁移成作者态源目录：

```bash
node scripts/package-source.mjs split deploy/packages/<artifact>.json
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
