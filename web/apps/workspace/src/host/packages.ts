import type { AppUI, Me } from "@platform/app";
import { memberRoles } from "@platform/app/roles";

type PackageLoad = { default: AppUI; contributions?: AppUI[] };
export type Package = { serves: string[]; roles?: string[]; public?: boolean; load: () => Promise<PackageLoad> };

export const packages: Package[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  { serves: ["build"], roles: ["builder", "integrator", "publisher"], load: () => import("@platform/build") },
  { serves: ["platform", "enterprise", "core", "ai", "flow", "agent", "knowledge"], public: true, load: () => import("@platform/govern") },
];

export function availablePackages(apps: readonly string[], member: Me["profile"]): Package[] {
  const held = new Set(apps);
  return packages.filter((p) => p.public || p.serves.some((id) => held.has(id) && (!p.roles || memberRoles(member, id).some(role => p.roles!.includes(role)))));
}
