// The UI packages this workspace is built with (D2): each loads only when the
// member holds a role in an app it serves. Public reference applications need
// no backend role; their runtime reads/actions still use the original host.
// Packages contribute to functional applications (ADR-0018 §3.3); editors
// and runtime views can join one application while retaining their owners.
import type { AppExtension, AppUI } from "@platform/app";

type PackageLoad = { default: AppUI; contributions?: (AppUI | AppExtension)[] };
export type Package = { serves: string[]; role?: string; public?: boolean; load: () => Promise<PackageLoad> };

export const packages: Package[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  { serves: ["build"], role: "builder", load: () => import("@pkg/build") },
  { serves: ["build"], role: "integrator", load: () => import("@pkg/build") },
  { serves: ["build"], role: "publisher", load: () => import("@pkg/build") },
  { serves: ["crm"], load: () => import("@pkg/crm") },
  { serves: ["pms"], load: () => import("@pkg/pms/app") },
  { serves: ["hcm"], load: () => import("@pkg/hcm") },
  { serves: ["csm"], load: () => import("@pkg/csm") },
  { serves: ["mes"], load: () => import("@pkg/mes") },
  { serves: ["erp"], load: () => import("@pkg/erp") },
  // Every member has an account in the Control Panel (ADR-0028); each contribution still decides who sees it (`for`).
  { serves: ["platform", "enterprise", "core", "ai", "flow", "agent", "knowledge"], public: true, load: () => import("@pkg/platform") },
];

/** Package availability, rather than every role value, determines loading. */
export function availablePackages(apps: readonly string[], roles: Readonly<Record<string, string>>): Package[] {
  const held = new Set(apps);
  return packages.filter((p) => p.public || p.serves.some((id) => held.has(id) && (!p.role || roles[id] === p.role)));
}
