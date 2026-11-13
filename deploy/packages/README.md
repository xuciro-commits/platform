# Product package sources

Each directory here is an authoring source for one product version. The top-level
`*.json` files are generated `PackageDescriptor` artifacts consumed by
`PLATFORM_PACKAGES_DIR`; do not edit them by hand.

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

`package.json` holds the descriptor fields. `blueprint.json` holds roots and an
asset index with a path per asset. `setup.json` holds setup steps, and
`roles/requirements.json` holds role requirements. The packer combines them
into the runtime artifact.

```bash
make packages
node scripts/package-source.mjs pack deploy/packages/manufacturing.mes
node scripts/package-source.mjs check deploy/packages/manufacturing.mes
```

To migrate another existing artifact into a source directory:

```bash
node scripts/package-source.mjs split deploy/packages/<artifact>.json
```
