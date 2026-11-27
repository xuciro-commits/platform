const SCENARIOS = {
  runtime: {
    records: { tenants: ["acceptance"] },
    gantt: { tenants: ["acceptance"] },
    workspace: { tenants: ["acceptance", "publisher"], seed: ["acceptance"] },
    builder: { tenants: ["acceptance"] },
    "i18n-grouping": { tenants: ["acceptance", "phone"] },
  },
  business: {
    crm: { tenants: ["crm"] },
    "crm-records": { tenants: ["acceptance"], acceptanceCrm: true },
    phone: { tenants: ["phone"] },
    connector: { tenants: ["acceptance"] },
  },
  delivery: {
    "package-extension": { tenants: ["acceptance"], acceptanceCrm: true },
    "package-lifecycle": { tenants: ["acceptance"] },
    official: { tenants: ["official"] },
    marketplace: { tenants: ["blank"] },
    "package-upgrade": { tenants: ["upgrade"] },
    "party-upgrade": { tenants: ["party-upgrade"] },
    "package-library": { tenants: ["library"] },
    "package-apps": { tenants: ["publisher"] },
  },
  security: {
    rebac: { tenants: ["acceptance", "rebac", "crm"] },
    "tenant-security": { tenants: ["audit-a"], seed: [] },
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
  return { specs, tenants, seed, acceptanceCrm: entries.some(entry => entry.acceptanceCrm), cache: seed.length > 0 };
}

export function countOption(name, fallback, max, env = process.env) {
  const raw = env[name] ?? String(fallback);
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < 1 || value > max) throw Error(name + " must be an integer from 1 to " + max);
  return value;
}

export function specSelectors(args, spec) {
  const name = arg => arg.match(/(?:^|\/)([a-z0-9-]+)\.spec\.ts(?::\d+)*$/)?.[1];
  const selectors = args.filter(arg => name(arg) === spec);
  return [...args.filter(arg => !name(arg)), ...(selectors.length ? selectors : [spec + ".spec.ts"])];
}

export function playwrightSelectors(args) {
  return args.map(arg => {
    const match = arg.match(/(?:^|\/)([a-z0-9-]+)\.spec\.ts((?::\d+)*)$/);
    return match ? "/" + match[1] + "\\.spec\\.ts$" + match[2] : arg;
  });
}
