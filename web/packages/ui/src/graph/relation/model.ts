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
};

export type RelationEdge = {
  id: string; source: string; target: string; label?: string;
  /** Marks the edges that form the hierarchy a tree or radial layout arranges by. */
  tree?: boolean;
  dashed?: boolean; directed?: boolean; tone?: string; reconnectable?: boolean;
};

/** How the canvas may arrange what it is given. `tree-*` reads each relationship as
 * child → parent; `layered` reads it in the direction the owner declared. */
export type RelationLayout = "tree-down" | "tree-right" | "layered" | "radial" | "grid";
export const relationLayouts: RelationLayout[] = ["tree-down", "tree-right", "layered", "radial", "grid"];

/** The box one element occupies; every layout in this family measures it. */
export const relationNodeSize = { width: 180, height: 56, gapX: 40, gapY: 48 };
