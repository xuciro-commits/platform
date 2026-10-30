import type { ReactNode } from "react";

export type RelationFact = { label: string; value: string };

export type RelationBadge = { label: string; size: number };

export type RelationNode = {
  id: string; label: string; caption?: string; icon?: ReactNode; tone?: string; dim?: boolean;
  flag?: string; detail?: string; linkable?: boolean; facts?: RelationFact[]; badge?: RelationBadge;
  class?: string;
  size?: { width: number; height: number };
};

export type RelationEdge = {
  id: string; source: string; target: string; label?: string;
  parent?: "source" | "target";
  dashed?: boolean; directed?: boolean; tone?: string; reconnectable?: boolean;
};

export type RelationLayout = "down" | "up" | "right" | "left" | "radial" | "force" | "grid";
export const relationLayouts: RelationLayout[] = ["down", "up", "right", "left", "radial", "force", "grid"];

export const relationNodeSize = { width: 180, height: 56 };

export const relationNodeClasses: Record<string, { title: string; icon: string; tone?: string }> = {
  connection: { title: "Connection", icon: "server", tone: "neutral" },
  observation: { title: "Observation source", icon: "activity", tone: "info" },
  source: { title: "Source", icon: "database", tone: "info" },
  dataset: { title: "Dataset", icon: "table", tone: "success" },
  pipeline: { title: "Pipeline", icon: "workflow", tone: "warning" },
  object: { title: "Object type", icon: "boxes", tone: "info" },
  writeback: { title: "Writeback", icon: "trending-up", tone: "danger" },
  app: { title: "Application", icon: "grid", tone: "info" },
  page: { title: "Page", icon: "file-text", tone: "success" },
  entity: { title: "Object type", icon: "boxes", tone: "warning" },
  query: { title: "Query", icon: "search", tone: "neutral" },
  function: { title: "Function", icon: "braces", tone: "neutral" },
  linktype: { title: "Relationship", icon: "git-branch", tone: "neutral" },
  propertytype: { title: "Shared property", icon: "tag", tone: "neutral" },
  valuetype: { title: "Value type", icon: "tag", tone: "neutral" },
  action: { title: "Action", icon: "zap", tone: "neutral" },
  operation: { title: "Operation", icon: "settings-2", tone: "neutral" },
  protocol: { title: "Protocol", icon: "plug", tone: "neutral" },
  interface: { title: "Interface", icon: "boxes", tone: "neutral" },
  record: { title: "Record", icon: "table", tone: "neutral" },
  flow: { title: "Flow", icon: "workflow", tone: "neutral" },
  run: { title: "Agent run", icon: "rocket", tone: "info" },
  effect: { title: "Effect", icon: "zap", tone: "warning" },
  property: { title: "Property", icon: "list-ordered", tone: "neutral" },
  relation: { title: "Relationship", icon: "git-branch", tone: "neutral" },
};

export function relationNodeClassOf(node: { class?: string }): string | undefined {
  return node.class;
}

export function relationNodeCaption(node: { caption?: string; class?: string }): string | undefined {
  return node.caption ?? (node.class ? relationNodeClasses[node.class]?.title : undefined);
}
