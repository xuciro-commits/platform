const SCENARIOS = {
  runtime: {
    records: { tenants: ["acceptance"] },
    gantt: { tenants: ["acceptance"] },
    workspace: { tenants: ["acceptance", "publisher"], seed: ["acceptance"] },
    builder: { tenants: ["acceptance"], acceptanceCrm: true },
    "i18n-grouping": { tenants: ["acceptance", "phone"] },
  },
  business: {
    crm: { tenants: ["acceptance", "crm"], acceptanceCrm: true },
    phone: { tenants: ["phone"], fresh: true },
    connector: { tenants: ["acceptance"], fresh: true },
  },
  delivery: {
    "package-lifecycle": { tenants: ["acceptance"], fresh: true },
    official: { tenants: ["official"], fresh: true },
    marketplace: { tenants: ["blank", "upgrade", "publisher", "library", "party-upgrade"], fresh: true },
  },
  security: {
    rebac: { tenants: ["acceptance", "rebac", "crm"] },
    "tenant-security": { tenants: ["audit-a"], seed: [], fresh: true },
  },
};
export const securityAPIPlan = { specs: [], tenants: ["audit-a", "audit-b", "library"], seed: [], acceptanceCrm: false, cache: false };
export const layers = Object.fromEntries(Object.entries(SCENARIOS).map(([layer, specs]) => [layer, Object.keys(specs)]));
const specsByName = Object.assign({}, ...Object.values(SCENARIOS));
export function seedPlan(args, interactive = false) {
  if (interactive) return { specs: [], tenants: ["acceptance"], seed: ["acceptance"], acceptanceCrm: true, cache: true };
  const named = [...new Set(args.map(arg => arg.match(/(?:^|\/)([a-z0-9-]+)\.spec\.ts(?::\d+)*$/)?.[1]).filter(Boolean))];
  for (const name of named) if (!Object.hasOwn(specsByName, name)) throw Error("No scenario assigned to spec " + name);
  const specs = named.length ? named : Object.keys(specsByName);
  const entries = specs.map(name => specsByName[name]);
  const tenants = [...new Set(entries.flatMap(entry => entry.tenants))].sort();
  const seed = [...new Set(entries.flatMap(entry => entry.seed ?? entry.tenants))].sort();
  return { specs, tenants, seed, acceptanceCrm: entries.some(entry => entry.acceptanceCrm), cache: named.length > 0 && !entries.some(entry => entry.fresh) };
}
