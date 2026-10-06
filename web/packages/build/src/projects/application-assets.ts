import type { Api } from "@platform/kernel";

/** Object and Flow identities include their authority prefix; the other
 * Build names are local to their owner. Never match a foreign owner's draft. */
export function applicationAssetRef(kind: string, name: string): Api.AssetRef {
  return { app: "build", kind, name: kind === "object" || kind === "flow" ? `build.${name}` : name };
}

export const applicationAssetKey = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

export const applicationAssetEditors = [
  { kind: "object", type: "build.object", view: "object-type", label: "Objects" },
  { kind: "flow", type: "build.process", view: "flow", label: "Flows" },
  { kind: "query", type: "build.query", view: "query", label: "Queries" },
  { kind: "function", type: "build.function", view: "function", label: "AI functions" },
  { kind: "compute", type: "build.code", view: "code", label: "Code functions" },
  { kind: "link-type", type: "build.linktype", view: "link-type", label: "Relationships" },
  { kind: "property-type", type: "build.propertytype", view: "property-type", label: "Shared properties" },
] as const;
