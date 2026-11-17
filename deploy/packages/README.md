# Product package sources

Each directory here is an authoring source for one product version. All seven
current products use this layout. The top-level `*.json` files are generated
`PackageDescriptor` artifacts consumed by `PLATFORM_PACKAGES_DIR`; do not edit
them by hand.

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

Only directories that contain assets are created. Optional `migrations/` and
`assets/` directories appear when a package first needs them.

`package.json` holds the descriptor fields plus `artifact`, the generated
runtime file name. `blueprint.json` holds roots and an asset index with a path
per asset. `setup.json` holds setup steps, and `roles/requirements.json` holds
role requirements. The packer combines them into the runtime artifact.

```bash
make packages
node scripts/package-source.mjs pack deploy/packages/manufacturing.mes
node scripts/package-source.mjs check deploy/packages/manufacturing.mes
```

To migrate another existing artifact into a source directory:

```bash
node scripts/package-source.mjs split deploy/packages/<artifact>.json
```

## Pack boundaries

A pack boundary is ownership. The rules every pack follows are in `docs/domain.md`
("产品包"); this section records what each manufacturing pack owns.

| Pack | Owns |
|---|---|
| `manufacturing.core` | The shared supplier-inquiry objects, connections, writebacks and queries. Its Foundation app holds the shared inquiry views, and no purchasing, accounting or shop-floor pages |
| `manufacturing.erp` | Materials, purchasing, inventory, accounting, production orders, and the inventory and accounting postings |
| `manufacturing.mes` | Routings, work-order execution, quality, equipment, downtime, shop-floor actions and execution records |

- Dependencies are `erp -> core` and `mes -> core`. ERP and MES never use each
  other's internal resources; MES binds upstream production facts through object
  interfaces, actions or versioned queries.
- ERP owns `productionbatch`, `erpreceipt`, `erpmove`, `erpstock`,
  `erpproductionposting`, `erpentry` and `erpline`. Receiving a batch, moving
  stock and posting production and accounting happen in one ERP accepted result.
- MES owns shop-floor execution, `mesbatch`, `mesissue`, `messignature` and
  `mesproductionresult`. From MES completion to ERP receipt is eventually
  consistent.
- MES writes back to ERP's `productionorder.confirm` through
  `mesproductionresult.send`. No outbox, gateway or distributed transaction
  coordinator is added, and a received batch is never retried under a new
  idempotency key.
- ERP gives MES production orders and batches through the named queries
  `productionorderupstream` and `productionbatchupstream`. MES does not poll
  ERP's internal record addresses.
