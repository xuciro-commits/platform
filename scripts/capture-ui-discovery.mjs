// Runs inside the authenticated Vite workspace; consumes its original owners.
export async function discoverUI({ root, samples, tenant }) {
  const session = JSON.parse(sessionStorage.getItem("oidc:session") || "null");
  const token = session?.accessToken || sessionStorage.getItem("workspace:identity");
  if (!token) throw new Error("The workspace has no signed-in identity");
  const headers = { Authorization: "Bearer " + token, ...(tenant ? { "Platform-Tenant": tenant } : {}) };
  const skipped = [], routes = new Map(), records = new Map();
  const read = async (path, optional = false) => {
    const response = await fetch(path, { headers });
    if (!response.ok) {
      if (!optional) throw new Error(path + ": HTTP " + response.status);
      skipped.push({ path, reason: "HTTP " + response.status });
      return undefined;
    }
    return response.json();
  };
  const me = await read("/v1/me"), actions = await read("/v1/actions");
  const entities = await read("/v1/entities"), definitions = await read("/v1/definitions");
  const admin = await read("/v1/host/me", true);
  const imports = await Promise.all([
    import("/src/host/packages.ts"), import("/src/host/applications.ts"),
    import("/src/chrome.tsx"), import("/src/tenantApps.tsx"),
    import("/@fs" + root + "/web/packages/build/src/projects/resources.ts"),
    import("/@fs" + root + "/web/packages/build/src/projects/studio-model.ts"),
  ]);
  const [packages, applications, chrome, delivered, resources, studio] = imports;
  const host = {
    me, entities, definitions, catalog: actions, outbox: [],
    role: id => me.apps.some(app => app.id === id) ? me.profile.roles[id] : undefined,
    can: schema => actions.some(action => action.schema === schema),
    action: schema => actions.find(action => action.schema === schema),
    source: { entity: type => entities.find(entity => entity.type === type) },
  };
  const loaded = await Promise.all(packages.availablePackages(me.apps.map(app => app.id), me.profile.roles).map(pkg => pkg.load()));
  const contributions = [...loaded.flatMap(module => [module.default, ...(module.contributions || [])]), chrome.personalWork, chrome.explorerApplication]
    .filter(app => (!app.for || app.for(host)) && (app.id !== "host-console" || !!admin));
  host.applications = applications.applicationUIInfo(contributions, host);
  const composed = applications.composeApplications(contributions);
  const native = composed.map(app => ({ ...app, home: app.homeFor(host) }));
  const all = [...applications.extendApplications(native, definitions, host.applications), ...delivered.tenantApps(definitions)];
  const viewInventory = new Map();
  for (const app of composed) for (const view of app.views) viewInventory.set(view.id, { id: view.id, title: view.title({}), application: app.id });
  for (const view of chrome.chromeViews(() => all, () => definitions)) viewInventory.set(view.id, { id: view.id, title: view.title({}), application: "workspace" });
  const add = (route, title, application, source) => {
    const params = new URLSearchParams(Object.entries(route.params || {}).sort(([a], [b]) => a.localeCompare(b)));
    const hash = "#/" + route.view + (params.size ? "?" + params : "");
    if (!routes.has(hash)) routes.set(hash, { route: hash, view: route.view, title, application, source });
  };
  add({ view: "home" }, "首页", "workspace", "navigation");
  add({ view: "portal" }, "应用门户", "workspace", "navigation");
  const walk = (items, app) => {
    for (const item of items) {
      if (item.route) add(item.route, item.label, app, "navigation");
      else if (item.items) walk(item.items, app);
    }
  };
  for (const app of all) {
    add(app.home, app.title, app.id, "navigation");
    for (const section of app.nav(host)) walk(section.items, app.id);
  }
  const readRecords = async type => {
    if (!records.has(type)) {
      const result = await read("/v1/records/" + encodeURIComponent(type) + "?limit=" + Math.max(8, samples * 4), true);
      records.set(type, result?.records || []);
    }
    return records.get(type);
  };
  // Existing draft editors use the same resource-to-route mapping as Studio.
  if (host.role("build") === "builder" || host.role("build") === "integrator") {
    for (const kind of resources.resourceKinds) {
      if (!entities.some(entity => entity.type === kind.type)) continue;
      const rows = (await readRecords(kind.type)).filter(row => resources.matchesResourceKind(kind, row)).slice(0, samples);
      if (!rows.length) skipped.push({ view: kind.view, reason: "No existing " + kind.type + " sample" });
      for (const row of rows) add(resources.resourceRoute(kind, row.id), (row.title || row.name || kind.label) + " · 编辑", "build", "editor");
    }
    if (entities.some(entity => entity.type === "build.app")) {
      for (const app of await readRecords("build.app")) {
        for (const { id: area } of studio.studioAreas) {
          add({ view: "project", params: { id: app.id, area } }, (app.title || app.name) + " · " + area, "build", "studio");
        }
      }
    }
  }
  // Inspect one real record of each readable object, without creating fixtures.
  const opens = Object.assign({ "agent.run": "run", "flow.instance": "flow-instance" }, ...host.applications.map(app => app.opens));
  const authoringTypes = new Set(resources.resourceKinds.map(kind => kind.type));
  for (const entity of entities) {
    if (authoringTypes.has(entity.type)) continue;
    const rows = (await readRecords(entity.type)).slice(0, samples);
    for (const row of rows) {
      if (!row.id) continue;
      add({ view: opens[entity.type] || "record", params: { type: entity.type, id: row.id } }, (entity.title || entity.type) + " · 详情", entity.app, "record");
    }
  }
  const kinds = new Set();
  for (const definition of definitions) {
    const ref = definition.ref;
    if (ref.kind === "page" && definition.page) add({ view: "page", params: ref }, definition.page.title || ref.name, ref.app, "published-page");
    const kind = definition.source + "/" + ref.kind;
    if (!kinds.has(kind)) {
      kinds.add(kind);
      add({ view: "definition", params: ref }, ref.name + " · 定义", ref.app, "definition");
    }
  }
  for (const id of admin?.tenants || []) add({ view: "host-tenant", params: { tenant: id } }, id + " · 宿主维护", "host-console", "navigation");
  const covered = new Set([...routes.values()].map(route => route.view));
  for (const view of viewInventory.values()) {
    if (!covered.has(view.id)) skipped.push({ view: view.id, title: view.title, reason: "No navigation or existing context sample; not fabricated" });
  }
  return {
    principal: me.principalId, tenant: me.tenantId, roles: me.profile.roles,
    views: [...viewInventory.values()], routes: [...routes.values()], skipped,
  };
}
