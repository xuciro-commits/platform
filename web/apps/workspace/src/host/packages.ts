// The UI packages this workspace is built with (D2): each loads only when the
// member holds a role in an app it serves. Public reference applications need
// no backend role; their runtime reads/actions still use the original host.
// One package may contribute several applications (ADR-0052 §3.3): the build
// package is the Ontology, Workshop, Automate, AI Functions, Code, Projects
// and Releases applications; the platform package is Control Panel, Runs,
// Data Connection, Agents, AI and Knowledge.
import type { AppUI } from "@platform/app";

export type PackageLoad = { default: AppUI; contributions?: AppUI[] };
export type Package = { serves: string[]; role?: string; public?: boolean; load: () => Promise<PackageLoad> };

export const packages: Package[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  { serves: ["build"], role: "builder", load: () => import("@pkg/build").then((m) => ({ default: m.default, contributions: m.contributions })) },
  { serves: ["build"], role: "publisher", load: () => import("@pkg/build").then((m) => ({ default: m.releases })) },
  { serves: ["crm"], load: () => import("@pkg/crm") },
  { serves: ["pms"], load: () => import("@pkg/pms/app") },
  { serves: ["hcm"], load: () => import("@pkg/hcm") },
  { serves: ["csm"], load: () => import("@pkg/csm") },
  { serves: ["mes"], load: () => import("@pkg/mes") },
  { serves: ["erp"], load: () => import("@pkg/erp") },
  { serves: ["erpadapter"], load: () => import("@pkg/erpadapter") },
  { serves: ["platform", "org", "ai", "flow", "agent", "knowledge"], load: () => import("@pkg/platform") },
];
