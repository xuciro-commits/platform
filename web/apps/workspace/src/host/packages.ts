// The UI packages this workspace is built with (D2): each loads only when the
// member holds a role in an app it serves. Public reference applications need
// no backend role; their runtime reads/actions still use the original host.
// Packages contribute to functional applications; editors
// and runtime views can join one application while retaining their owners.
import type { AppUI } from "@platform/app";

type PackageLoad = { default: AppUI; contributions?: AppUI[] };
export type Package = { serves: string[]; roles?: string[]; public?: boolean; load: () => Promise<PackageLoad> };

export const packages: Package[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  { serves: ["build"], roles: ["builder", "integrator", "publisher"], load: () => import("@platform/build") },
  // Every member has an account in the Control Panel; each contribution still decides who sees it (`for`).
  { serves: ["platform", "enterprise", "core", "ai", "flow", "agent", "knowledge"], public: true, load: () => import("@platform/govern") },
];

/** Package availability, rather than every role value, determines loading. */
export function availablePackages(apps: readonly string[], roles: Readonly<Record<string, string>>): Package[] {
  const held = new Set(apps);
  return packages.filter((p) => p.public || p.serves.some((id) => held.has(id) && (!p.roles || p.roles.includes(roles[id] ?? ""))));
}
