import type { ReactNode } from "react";

/** A relation canvas draws things and what holds them together: an enterprise
 * element and its relationships, an asset and what it is built from, a record and
 * its neighbours, a run and the effects it caused. There is no order to read and no
 * port to join — a line says two things are related, and the owner says what the
 * line means and what may be done about it (ADR-0086 D3). */

/** One fact the details card shows about a chosen element. */
export type RelationFact = { label: string; value: string };

/** Drawn as a disc with this text inside, for a reader who only needs to tell one
 * neighbour of a record apart from another. */
export type RelationBadge = { label: string; size: number };

export type RelationNode = {
  id: string; label: string; caption?: string; icon?: ReactNode; tone?: string; dim?: boolean;
  flag?: string; detail?: string; linkable?: boolean; facts?: RelationFact[]; badge?: RelationBadge;
  /** What this thing is, for drawing and grouping (ADR-0090): the class decides
   * the glyph, the default tone and the caption word when the node names none. */
  class?: string;
  /** The box this element occupies; without it the family's standard box is used. */
  size?: { width: number; height: number };
};

export type RelationEdge = {
  id: string; source: string; target: string; label?: string;
  /** Marks the edges that form the hierarchy a tree or radial layout arranges by. */
  tree?: boolean;
  dashed?: boolean; directed?: boolean; tone?: string; reconnectable?: boolean;
};

/** How the canvas may arrange what it is given. `tree-*` reads each relationship as
 * child → parent; `layered` reads it in the direction the owner declared. */
export type RelationLayout = "tree-down" | "tree-right" | "layered" | "radial" | "grid" | "force" | "organization" | "layered-up" | "layered-left";
export const relationLayouts: RelationLayout[] = ["tree-down", "layered", "layered-up", "layered-left", "organization", "radial", "force", "grid"];

/** The box one element occupies; every layout in this family measures it. */
export const relationNodeSize = { width: 180, height: 56, gapX: 40, gapY: 48 };

/** The standing relation classes (ADR-0090 D1): what a thing on a relationship
 * drawing *is*. The icon is a name from the kit's one icon vocabulary (ADR-0084
 * D1), so this table is pure data — a new class is one row, never a renderer. A
 * node's own icon/tone/caption always win; the class only fills what is absent. */
export const relationNodeClasses: Record<string, { title: string; icon: string; tone?: string }> = {
  // Data plane: what feeds what.
  connection: { title: "Connection", icon: "server", tone: "neutral" },
  source: { title: "Source", icon: "database", tone: "info" },
  dataset: { title: "Dataset", icon: "table", tone: "success" },
  pipeline: { title: "Pipeline", icon: "workflow", tone: "warning" },
  object: { title: "Object type", icon: "boxes", tone: "info" },
  writeback: { title: "Writeback", icon: "trending-up", tone: "danger" },
  // Asset plane: what an installed asset is built from.
  app: { title: "Application", icon: "grid", tone: "info" },
  page: { title: "Page", icon: "file-text", tone: "success" },
  entity: { title: "Object type", icon: "boxes", tone: "warning" },
  query: { title: "Query", icon: "search", tone: "neutral" },
  function: { title: "Function", icon: "braces", tone: "neutral" },
  linktype: { title: "Relationship", icon: "git-branch", tone: "neutral" },
  propertytype: { title: "Shared property", icon: "tag", tone: "neutral" },
  action: { title: "Action", icon: "zap", tone: "neutral" },
  operation: { title: "Operation", icon: "settings-2", tone: "neutral" },
  // Execution plane: what a run caused.
  record: { title: "Record", icon: "table", tone: "neutral" },
  flow: { title: "Flow", icon: "workflow", tone: "neutral" },
  run: { title: "Agent run", icon: "rocket", tone: "info" },
  effect: { title: "Effect", icon: "zap", tone: "warning" },
  // Model plane: the object model itself.
  property: { title: "Property", icon: "list-ordered", tone: "neutral" },
  relation: { title: "Relationship", icon: "git-branch", tone: "neutral" },
};

/** The class a node draws under: the class it names, else nothing (the view then
 * falls back to the plain box). Accepts a relation node. */
export function relationNodeClassOf(node: { class?: string }): string | undefined {
  return node.class;
}

/** The caption a node shows: its own, else its class's title — translated where
 * it is rendered, as every other word on the canvas is. */
export function relationNodeCaption(node: { caption?: string; class?: string }): string | undefined {
  return node.caption ?? (node.class ? relationNodeClasses[node.class]?.title : undefined);
}
