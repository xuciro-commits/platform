import { withApp, type AppUI, type Host, type Me } from "@platform/app";
import { memberRoles } from "@platform/app/roles";

type PackageLoad = { default: AppUI; contributions?: AppUI[] };
export type Package = { serves: string[]; roles?: string[]; public?: boolean; load: () => Promise<PackageLoad> };

const governPackage: Package = { serves: ["platform", "enterprise", "ai", "flow", "agent", "knowledge"], public: true, load: () => import("@platform/govern") };
const buildPackage: Package = { serves: ["build"], roles: ["builder", "integrator", "publisher"], load: () => import("@platform/build") };
export const packages: Package[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  governPackage,
  buildPackage,
];

export function availablePackages(apps: readonly string[], member: Me["profile"]): Package[] {
  const held = new Set(apps);
  if ((member.grants ?? []).some(grant => grant.app === "build" && grant.role === "builder" && grant.by?.startsWith("project:"))) held.add("build");
  return packages.filter(p => p.public
    || p.serves.some(id => held.has(id) && (!p.roles || memberRoles(member, id).some(role => p.roles!.includes(role))))
    || p === buildPackage && (held.has("platform") && memberRoles(member, "platform").includes("admin") || held.has("agent") && !!member.roles.agent));
}

export async function loadPackages(selected: readonly Package[], objectExplorer: AppUI): Promise<(host: Host) => AppUI[]> {
  const groups = await Promise.all(selected.map(async source => ({ source, module: await source.load() })));
  const govern = groups.find(group => group.source === governPackage)?.module as typeof import("@platform/govern") | undefined;
  const build = groups.find(group => group.source === buildPackage)?.module as typeof import("@platform/build") | undefined;
  const entries = groups.flatMap(({ source, module }) => [module.default, ...(module.contributions ?? [])]
    .map(app => withApp(app, { serves: app.serves ?? source.serves })));
  return host => [
    ...entries,
    build ? build.extendObjectExplorer(objectExplorer, host) : objectExplorer,
    ...(govern ? [govern.createMonitoring(host, build?.integrationMonitoring)] : []),
    ...(build && govern ? [build.createDataConnection(host, govern.dataConnectionOperations), build.createAgentStudio(host, govern.agentOperations)] : []),
  ];
}
