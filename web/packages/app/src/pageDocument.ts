import type { Api } from "@platform/kernel";
import { pageUIProfile, widgetContract } from "./widgets/registry";

type Document = Api.PageDocument;

/** Lift an older flat composed page into a stable layout tree for editing.
 * Existing published V1 pages keep their original renderer until saved as V2.
 */
export function pageDocumentFromSections<T extends Pick<Api.Section, "id" | "widget" | "width" | "configVersion">>(source: T[]): { sections: (T & { id: string; configVersion?: number })[]; document: Document } {
  const used = new Set(source.flatMap((section) => section.id ? [section.id] : []));
  const sections = source.map((section, i) => {
    let id = section.id;
    if (!id) { id = `v1section${i}`; while (used.has(id)) id += "x"; used.add(id); }
    return { ...section, id, configVersion: section.configVersion ?? widgetContract(section.widget)?.configVersion };
  });
  const root: string[] = [];
  const nodes: Document["nodes"] = { root: { kind: "rows", children: root } };
  for (let i = 0; i < sections.length; i++) {
    const leaf = `v1node${i}`;
    const section = sections[i]!;
    nodes[leaf] = { kind: "widget", section: section.id };
    if (section.width !== "half") {
      root.push(leaf);
      continue;
    }
    const row = `v1columns${i}`;
    const children = [leaf];
    if (sections[i + 1]?.width === "half") {
      i++;
      const second = `v1node${i}`;
      nodes[second] = { kind: "widget", section: sections[i]!.id };
      children.push(second);
    }
    nodes[row] = { kind: "columns", children };
    root.push(row);
  }
  return { sections, document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes } };
}
