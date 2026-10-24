import { expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({ host: undefined }));

vi.mock("@platform/app", async () => {
  const { createElement } = await import("react");
  return {
    defineApp: app => app,
    useHost: () => state.host,
    useReadQuery: () => ({ data: [] }),
    newId: () => "id",
    Records: ({ type }) => createElement("div", { "data-record-type": type }),
  };
});

vi.mock("@platform/ui", () => {
  const empty = () => null;
  const links = items => items.flatMap(item => "route" in item ? [item] : links(item.items));
  const components = [
    "Button", "Card", "PageHeader", "Checkbox", "DataTable",
    "Form", "FlowGraph", "StatusTag", "Tag", "Select",
  ];
  return {
    t: text => text,
    register: () => {},
    navigationLinks: links,
    defineStatuses: states => states,
    useWorkspace: () => ({ open: () => {} }),
    ...Object.fromEntries(components.map(name => [name, empty])),
  };
});

vi.mock("./people", () => ({
  Members: () => null,
  MemberDetail: () => null,
}));
vi.mock("./permissions", () => ({ Permissions: () => null }));
vi.mock("./credentials",()=>({Credentials:()=>null,CredentialEditor:()=>null}));
vi.mock("./account", () => ({ MyAccount: () => null }));
vi.mock("./organisation", () => ({ Organisation: () => null }));
vi.mock("./enterprise", () => ({ Enterprise: () => null }));
vi.mock("./apps", () => ({
  Apps: () => null,
  Matrix: () => null,
  Protocols: () => null,
}));
vi.mock("./words", () => ({ Words: () => null }));
vi.mock("./operations", () => ({
  Automation: () => "Work queue content",
  Deliveries: () => null,
  SystemHealth: () => null,
  Integrations: () => null,
  Endpoints: () => null,
  OutboundEffects: () => null,
  AppSettingsView: () => null,
  Audit: () => null,
}));
vi.mock("./ai", () => ({
  AIProviders: () => null,
  AIPlayground: () => null,
  AIUsage: () => null,
}));
vi.mock("./knowledge", () => ({
  Glossary: () => null,
  Knowledge: () => null,
}));
vi.mock("./host", () => ({
  HostMigrations: () => null,
  HostOverview: () => null,
  HostPromotions: () => null,
  HostTenant: () => null,
}));

const {
  controlPanel, agents, ai, enterprise, dataConnection,
  runs, knowledge, contributions,
} = await import("./index.tsx");
const { Flows } = await import("./processes.tsx");
const { Automation } = await import("./operations.tsx");

const host = (roles = {}, entities = []) => ({
  role: app => roles[app],
  can: () => false,
  entities,
  me: { tenantId: "test" },
});
const links = items => items.flatMap(item => "route" in item ? [item] : links(item.items));
const routes = (app, member) => app.nav(member)
  .flatMap(section => links(section.items))
  .map(item => item.route.view);

test("control, enterprise and AI have one menu owner while keeping original member conditions", () => {
  const admin = host({ platform: "admin", ai: "admin" });
  const sections = controlPanel.nav(admin);

  expect(sections).toHaveLength(3);
  for (const section of sections) {
    expect(section.items.length).toBeGreaterThanOrEqual(3);
  }
  expect(routes(controlPanel, admin)).not.toContain("enterprise");
  expect(routes(controlPanel, admin)).not.toContain("ai-providers");
  expect(controlPanel.views.some(view => view.id === "enterprise" || view.id === "ai-providers")).toBe(false);
  expect(routes(ai, admin)).toContain("ai-providers");
  expect(routes(ai, host({ ai: "user" }))).not.toContain("ai-providers");
  expect(enterprise.for(host({ enterprise: "viewer" }))).toBe(true);
  expect(enterprise.for(host({ platform: "admin" }))).toBe(true);
  expect(enterprise.for(host())).toBe(false);
});

test("runtime lists have dedicated routes and administration stays outside a builder's data menu", () => {
  const builder = host({ build: "builder" });
  const admin = host({ platform: "admin" });

  expect(dataConnection.for(builder)).toBe(true);
  expect(routes(dataConnection, builder)).toEqual(["observations", "lineage"]);
  expect(routes(dataConnection, admin)).toEqual([
    "integrations", "endpoints", "observations", "lineage", "effects",
  ]);
  expect(routes(agents, host({ agent: "admin" }))).toEqual([
    "agents", "agent-runs", "agent-memories", "evaluations",
  ]);
  for (const [view, type] of [["agent-runs", "agent.run"], ["agent-memories", "agent.memory"]]) {
    expect(agents.views.find(item => item.id === view).render({}).type().props.type).toBe(type);
  }

  const home = runs.views.find(view => view.id === runs.home.view);
  state.host = admin;
  expect(home.render({}).type().type).toBe(Automation);
  state.host = host({ flow: "operator" });
  expect(home.render({}).type().type).toBe(Flows);
  expect(routes(runs, host({ flow: "operator" }))).toEqual(["flows"]);
});

test("master data groups every existing core type and keeps Explorer in this application", () => {
  const core = contributions.find(app => app.id === "core");
  const types = [
    "person", "partner", "site", "location", "material",
    "uom", "currency", "account", "period", "journal",
  ].map(name => `core.${name}`);
  const member = host({ core: "steward" }, types.map(type => ({ type, app: "core", plural: type })));
  const items = core.nav(member)[0].items;

  expect(items).toHaveLength(4);
  expect(items.filter(item => "items" in item).map(group => group.items.length)).toEqual([4, 3, 3]);
  expect(links(items)
    .filter(item => item.route.view === "master-data")
    .map(item => item.route.params.type)
    .sort()).toEqual([...types].sort());
  expect(links(items).filter(item => item.route.view === "explorer")).toHaveLength(1);
  expect(knowledge.category).toBe("ontology");
});
