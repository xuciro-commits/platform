import type { Api } from "@platform/kernel";

type Document = Api.PageDocument;
export type LayoutKind = "rows" | "columns" | "tabs" | "flow" | "toolbar" | "loop";
type Kind = LayoutKind;
import { pageUIProfile, pageVariableContract } from "@platform/app";

export const layoutID = (prefix: string) => `${prefix}${crypto.randomUUID()}`;

function parentOf(document: Document, child: string): string | undefined {
  return document.unusedWidgets?.find(entry=>entry.node===child)?.parent ?? Object.entries(document.nodes).find(([, node]) => node.children?.includes(child))?.[0];
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
  const parent = after && Object.entries(next.nodes).find(([,node])=>node.children?.includes(after))?.[0];
  if (after && parent) next.nodes[parent]!.children!.splice(next.nodes[parent]!.children!.indexOf(after) + 1, 0, id);
  else target.children = [...(target.children ?? []), id];
  return repairTabs(next);
}

/** Wrap the chosen widget and its next sibling; a lone widget can also start a group. */
export function groupWidget(document: Document, section: string, kind: Kind): { document: Document; id?: string } {
  const leaf = leafOf(document, section);
  const parent = leaf && Object.entries(document.nodes).find(([,node])=>node.children?.includes(leaf))?.[0];
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
  next.unusedWidgets=next.unusedWidgets?.filter(entry=>entry.node!==leaf);
  next.nodes[parent]!.children = (next.nodes[parent]!.children??[]).filter((id) => id !== leaf);
  delete next.nodes[leaf];
  next.events = next.events?.filter((event) => event.source !== section);
  pruneEmpty(next, parent);
  return repairTabs(next);
}

function pruneEmpty(next: Document, parent: string) {
  // Empty non-root containers are removed so a saved document remains valid.
  let current = parent;
  while (current !== next.root && next.nodes[current]?.children?.length === 0 && !next.unusedWidgets?.some(entry=>entry.parent===current)) {
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
  if(leaf&&document.unusedWidgets?.some(entry=>entry.node===leaf))return restoreWidget(document,section,container,afterSection);
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
  next.unusedWidgets=next.unusedWidgets?.map(entry=>entry.parent===group?{...entry,parent}:entry);
  delete next.nodes[group];
  return repairTabs(next);
}

export function setLayoutKind(document: Document, id: string, kind: Kind): Document {
  const next = structuredClone(document), node = next.nodes[id];
  if (!node || node.kind === "widget") return document;
  node.kind = kind;
  if (kind === "loop" && !node.loop) {
    const item = layoutID("item");
    next.variables = { ...next.variables, [item]: { title: "", scope: "loop-item", owner: id, type: "record", mode: "resource", source: { kind: "item", node: id } } };
    node.loop = { collection: Object.entries(next.variables).find(([, value]) => (loopOwner(next,id)?value.scope==="loop-item"&&value.owner===loopOwner(next,id)&&value.source?.kind==="plan":variableAccessible(value,undefined,overlayOwner(next,id))) && value.type==="object-set" && (value.mode === "resource" && (value.source?.kind === "query" || value.source?.kind === "plan") || value.mode==="shared"))?.[0] ?? "", itemVariable: item, limit: 50 };
  }
  if (kind !== "loop") delete node.loop;
  next.uiProfile = pageUIProfile;
  if (kind !== "flow" && kind !== "toolbar") delete node.align;
  if (kind === "tabs") {
    next.uiProfile = pageUIProfile;
    if (!node.activeVariable) {
      const variable = layoutID("tab");
      const overlay = overlayOwner(next, id), loop = loopOwner(next, id);
      next.variables = { ...next.variables, [variable]: { title: "", scope: loop ? "loop-item" : overlay ? "overlay" : "page", owner: loop ?? overlay, type: "string", mode: "state", initial: node.children?.[0] ?? "" } };
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
    [...(node.children??[]),...(next.unusedWidgets??[]).filter(entry=>entry.parent===id).map(entry=>entry.node)].forEach(remove); delete next.nodes[id];
  };
  remove(overlay.root); next.unusedWidgets=next.unusedWidgets?.filter(entry=>!!next.nodes[entry.node]&&!!next.nodes[entry.parent]); delete next.overlays![id];
  next.queries=Object.fromEntries(Object.entries(next.queries??{}).filter(([,q])=>q.owner!==id)); delete next.variables?.[overlay.openVariable];
  for (const [key, variable] of Object.entries(next.variables ?? {})) if (variable.scope === "overlay" && variable.owner === id) delete next.variables![key];
  for (const event of next.events ?? []) if (event.target === overlay.openVariable) sections.add(event.source);
  for (const section of sections) Object.assign(next, removeWidget(next, section));
  next.events = next.events?.filter((event) => !sections.has(event.source) && event.target !== overlay.openVariable);
  return { document: next, sections };
}

export function loopOwner(document: Document, node: string): string | undefined {
  const seen = new Set<string>(); let parent = parentOf(document, node);
  while (parent && !seen.has(parent)) {
    if (document.nodes[parent]?.kind === "loop") return parent;
    seen.add(parent); parent = parentOf(document, parent);
  }
  return undefined;
}

export function overlayOwner(document: Document, node: string): string | undefined {
  const roots = new Map(Object.entries(document.overlays ?? {}).map(([id, overlay]) => [overlay.root, id]));
  const seen = new Set<string>(); let current: string | undefined = node;
  while (current && !seen.has(current)) {
    if (roots.has(current)) return roots.get(current);
    seen.add(current); current = parentOf(document, current);
  }
  return undefined;
}

export function variableAccessible(variable: Api.PageVariable, loop?: string, overlay?: string) {
  return variable.scope === "page" || variable.scope === "application" || variable.scope === "loop-item" && !!loop && variable.owner === loop || variable.scope === "overlay" && !!overlay && variable.owner === overlay;
}

export function synchronizeLoopBindings<T extends { id?: string; widget: string; selection?: string; recordVariable?: string }>(document: Document, sections: T[]): { document: Document; sections: T[] } {
  const removed = new Set(Object.entries(document.variables ?? {}).filter(([, value]) => value.scope === "loop-item" && document.nodes[value.owner ?? ""]?.kind !== "loop").map(([id]) => id));
  const next = { ...document, variables: Object.fromEntries(Object.entries(document.variables ?? {}).filter(([id]) => !removed.has(id))) };
  return { document: next, sections: sections.map((section) => {
    const leaf = leafOf(document, section.id ?? ""), owner = leaf ? loopOwner(document, leaf) : undefined;
    const item = owner && (pageVariableContract.loop.recordWidgets as readonly string[]).includes(section.widget) ? document.nodes[owner]?.loop?.itemVariable : undefined;
    return item ? { ...section, selection: undefined, recordVariable: item } : section.recordVariable && document.variables?.[section.recordVariable]?.mode !== "input" && document.variables?.[section.recordVariable]?.source?.kind !== "record" ? { ...section, recordVariable: undefined } : section;
  }) };
}

/** Dormant edges retain original scopes; actual children remain render-only. */
export function stashWidget(document:Document,section:string):Document {
 const leaf=leafOf(document,section),parent=leaf&&parentOf(document,leaf);
 if(!leaf||!parent||document.unusedWidgets?.some(entry=>entry.node===leaf))return document;
 const next=structuredClone(document);next.uiProfile=pageUIProfile;
 next.nodes[parent]!.children=next.nodes[parent]!.children?.filter(id=>id!==leaf);
 next.unusedWidgets=[...(next.unusedWidgets??[]),{node:leaf,parent}];return repairTabs(next);
}
export function restoreWidget(document:Document,section:string,container:string,afterSection?:string):Document {
 const leaf=leafOf(document,section),entry=document.unusedWidgets?.find(entry=>entry.node===leaf);
 const after=afterSection?leafOf(document,afterSection):undefined,target=after?Object.entries(document.nodes).find(([,node])=>node.children?.includes(after))?.[0]:container;
 if(!entry||!target||document.nodes[target]?.kind==="widget"||!document.nodes[target])return document;
 const next=structuredClone(document);next.uiProfile=pageUIProfile;next.unusedWidgets=next.unusedWidgets?.filter(e=>e.node!==leaf);
 const children=next.nodes[target]!.children??[];children.splice(after?children.indexOf(after)+1:children.length,0,entry.node);next.nodes[target]!.children=children;
 if(entry.parent!==target)pruneEmpty(next,entry.parent);return repairTabs(next);
}
