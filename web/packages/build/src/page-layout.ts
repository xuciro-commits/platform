import type { Api } from "@platform/kernel";

type Document = Api.PageDocument;
export type LayoutKind = "rows" | "columns" | "tabs" | "flow" | "toolbar";
type Kind = LayoutKind;
import { pageUIProfile } from "@platform/app";

export const layoutID = (prefix: string) => `${prefix}${crypto.randomUUID()}`;

function parentOf(document: Document, child: string): string | undefined {
  return Object.entries(document.nodes).find(([, node]) => node.children?.includes(child))?.[0];
}

function leafOf(document: Document, section: string): string | undefined {
  return Object.entries(document.nodes).find(([, node]) => node.kind === "widget" && node.section === section)?.[0];
}

export function appendWidget(document: Document, section: string, container = document.root, afterSection?: string): Document {
  const next = structuredClone(document);
  const target = next.nodes[container];
  if (!target || target.kind === "widget") return document;
  const id = layoutID("node");
  next.nodes[id] = { kind: "widget", section };
  const after = afterSection ? leafOf(next, afterSection) : undefined;
  const parent = after && parentOf(next, after);
  if (after && parent) next.nodes[parent]!.children!.splice(next.nodes[parent]!.children!.indexOf(after) + 1, 0, id);
  else target.children = [...(target.children ?? []), id];
  return repairTabs(next);
}

/** Wrap the chosen widget and its next sibling; a lone widget can also start a group. */
export function groupWidget(document: Document, section: string, kind: Kind): { document: Document; id?: string } {
  const leaf = leafOf(document, section);
  const parent = leaf && parentOf(document, leaf);
  if (!leaf || !parent) return { document };
  const next = structuredClone(document);
  const siblings = next.nodes[parent]!.children!;
  const at = siblings.indexOf(leaf);
  const children = siblings.splice(at, Math.min(2, siblings.length - at));
  const id = layoutID("group");
  next.nodes[id] = { kind: "rows", children };
  siblings.splice(at, 0, id);
  return { document: setLayoutKind(next, id, kind), id };
}

export function moveWidget(document: Document, section: string, delta: -1 | 1): Document {
  const leaf = leafOf(document, section);
  const parent = leaf && parentOf(document, leaf);
  if (!leaf || !parent) return document;
  const next = structuredClone(document);
  const siblings = next.nodes[parent]!.children!;
  const at = siblings.indexOf(leaf), target = at + delta;
  if (target < 0 || target >= siblings.length) return document;
  [siblings[at], siblings[target]] = [siblings[target]!, siblings[at]!];
  return repairTabs(next);
}

export function removeWidget(document: Document, section: string): Document {
  const leaf = leafOf(document, section);
  const parent = leaf && parentOf(document, leaf);
  if (!leaf || !parent) return document;
  const next = structuredClone(document);
  next.nodes[parent]!.children = next.nodes[parent]!.children!.filter((id) => id !== leaf);
  delete next.nodes[leaf];
  next.events = next.events?.filter((event) => event.source !== section);
  pruneEmpty(next, parent);
  return repairTabs(next);
}

function pruneEmpty(next: Document, parent: string) {
  // Empty non-root containers are removed so a saved document remains valid.
  let current = parent;
  while (current !== next.root && next.nodes[current]?.children?.length === 0) {
    const above = parentOf(next, current);
    if (!above) break;
    next.nodes[above]!.children = next.nodes[above]!.children!.filter((id) => id !== current);
    delete next.nodes[current];
    current = above;
  }
}

/** Reparent a leaf without changing its widget identity or business bindings. */
export function relocateWidget(document: Document, section: string, container: string, afterSection?: string): Document {
  const leaf = leafOf(document, section), after = afterSection ? leafOf(document, afterSection) : undefined;
  if (!leaf || leaf === after) return document;
  const from = parentOf(document, leaf), to = after ? parentOf(document, after) : container;
  if (!from || !to || document.nodes[to]?.kind === "widget" || !document.nodes[to]) return document;
  const next = structuredClone(document);
  next.nodes[from]!.children = next.nodes[from]!.children!.filter((id) => id !== leaf);
  const children = next.nodes[to]!.children ?? [];
  const at = after ? children.indexOf(after) + 1 : children.length;
  children.splice(at, 0, leaf); next.nodes[to]!.children = children;
  pruneEmpty(next, from);
  return repairTabs(next);
}

export function ungroup(document: Document, group: string): Document {
  const parent = parentOf(document, group);
  if (!parent || document.nodes[group]?.kind === "widget") return document;
  const next = structuredClone(document);
  const siblings = next.nodes[parent]!.children!;
  const at = siblings.indexOf(group);
  siblings.splice(at, 1, ...(next.nodes[group]!.children ?? []));
  delete next.nodes[group];
  return repairTabs(next);
}

export function setLayoutKind(document: Document, id: string, kind: Kind): Document {
  const next = structuredClone(document), node = next.nodes[id];
  if (!node || node.kind === "widget") return document;
  node.kind = kind;
  next.uiProfile = pageUIProfile;
  if (kind !== "flow" && kind !== "toolbar") delete node.align;
  if (kind === "tabs") {
    next.uiProfile = pageUIProfile;
    if (!node.activeVariable) {
      const variable = layoutID("tab");
      next.variables = { ...next.variables, [variable]: { title: "", scope: "page", type: "string", mode: "state", initial: node.children?.[0] ?? "" } };
      node.activeVariable = variable;
    }
  } else delete node.activeVariable;
  return repairTabs(next);
}

function repairTabs(document: Document): Document {
  for (const node of Object.values(document.nodes)) {
    if (node.kind !== "tabs" || !node.children?.length) continue;
    const variable = document.variables?.[node.activeVariable ?? ""];
    if (variable?.type === "string" && variable.mode === "state" && !node.children.includes(String(variable.initial))) variable.initial = node.children[0];
  }
  return document;
}

/** An independent root; the editor must add content before saving. */
export function addOverlay(document: Document, title: string): { document: Document; root: string } {
  const next = structuredClone(document), id = layoutID("overlay"), root = layoutID("overlayRoot"), variable = layoutID("open");
  next.uiProfile = pageUIProfile;
  next.nodes[root] = { kind: "rows", children: [] };
  next.variables = { ...next.variables, [variable]: { title, scope: "page", type: "boolean", mode: "state", initial: false } };
  next.overlays = { ...next.overlays, [id]: { root, kind: "modal", title, openVariable: variable } };
  return { document: next, root };
}

/** Remove this root and its triggers, preserving every other section identity. */
export function removeOverlay(document: Document, id: string): { document: Document; sections: Set<string> } {
  const next = structuredClone(document), overlay = next.overlays?.[id], sections = new Set<string>();
  if (!overlay) return { document, sections };
  const remove = (id: string) => {
    const node = next.nodes[id]; if (!node) return;
    if (node.section) sections.add(node.section);
    node.children?.forEach(remove); delete next.nodes[id];
  };
  remove(overlay.root); delete next.overlays![id]; delete next.variables?.[overlay.openVariable];
  for (const event of next.events ?? []) if (event.target === overlay.openVariable) sections.add(event.source);
  for (const section of sections) Object.assign(next, removeWidget(next, section));
  next.events = next.events?.filter((event) => !sections.has(event.source) && event.target !== overlay.openVariable);
  return { document: next, sections };
}
