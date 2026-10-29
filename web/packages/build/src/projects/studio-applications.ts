import type {Definition as WorkspaceDefinition} from "@platform/app";
import type {PageUI} from "@platform/app";
import type { Api } from "@platform/kernel";
import type { ApplicationUIInfo } from "@platform/app";
import type { Project } from "./project";
import type { AuthoringSection } from "../workshop/page-editor/draft";
import { refKey } from "./resources";

/** Installed identity is separate from the optional Build draft record ID. */
export const installedStudioID = (ref: Api.AssetRef) => `installed/${encodeURIComponent(ref.app)}/${encodeURIComponent(ref.name)}`;
export function installedStudioRef(id?: string): Api.AssetRef | undefined {
  const parts = id?.split("/");
  return parts?.length === 3 && parts[0] === "installed"
    ? { app: decodeURIComponent(parts[1]!), kind: "app", name: decodeURIComponent(parts[2]!) } : undefined;
}

export type StudioApplication = {
  id: string; ref: Api.AssetRef; title: string; description?: string; version?: string;
  project?: Project; definition?: WorkspaceDefinition;
};

/** Original navigation remains a replaceable slot after its renderer retires. */
export function applicationViewOptions(entry?: ApplicationUIInfo): { id: string; title: string }[] {
  if (!entry) return [];
  const views = new Map<string, { id: string; title: string }>();
  const add = (id: string, title?: string) => views.set(id, { id, title: title || entry.views.find(view => view.id === id)?.title || id });
  entry.views.forEach(view => add(view.id, view.title));
  const visit = (items: ApplicationUIInfo["sections"][number]["items"]) => {
    for (const item of items) {
      if ("route" in item) add(item.route.view, item.label);
      else visit(item.items);
    }
  };
  entry.sections.forEach(section => visit(section.items));
  add(entry.home.view);
  Object.values(entry.opens).forEach(view => add(view));
  return [...views.values()];
}

export function studioApplications(apps: readonly Api.AppInfo[], definitions: readonly WorkspaceDefinition[], projects: readonly Project[]): StudioApplication[] {
  const entries = new Map<string, StudioApplication>();
  for (const app of apps) {
    const ref: Api.AssetRef = { app: app.id, kind: "app", name: app.id };
    entries.set(refKey(ref), { id: installedStudioID(ref), ref, title: app.title || app.id, version: app.version });
  }
  for (const definition of definitions) {
    if (definition.ref.kind !== "app" || !definition.application) continue;
    const { ref, application } = definition;
    entries.set(refKey(ref), { id: installedStudioID(ref), ref, title: application.title || application.name, description: application.description, version: definition.version, definition });
  }
  for (const project of projects) {
    const ref: Api.AssetRef = { app: "build", kind: "app", name: project.name };
    if (project.archived) continue;
    entries.set(refKey(ref), { ...entries.get(refKey(ref)), id: project.id, ref, title: project.title || project.name, description: project.description, project });
  }
  return [...entries.values()].sort((a, b) => a.title.localeCompare(b.title) || refKey(a.ref).localeCompare(refKey(b.ref)));
}

/** Reverse only bindings the Build page format can represent without loss. */
export function pageDraftFromDefinition(page: PageUI.Page) {
  const schema = (ref: Api.AssetRef) => {
    if (ref.app !== ref.name.split(".")[0]) throw Error("page-binding-owner");
    return ref.name;
  };
  const sections: AuthoringSection[] = (page.sections ?? []).map(section => {
    const { object, query, actions, function: fn, inlineEdit, ...configuration } = section;
    // A FunctionRef stores an ordinal only; it cannot retain this exact owner version.
    if (fn) throw Error("page-function-binding");
    return { ...configuration,
      object: object?.name ? schema(object) : undefined,
      query: query?.name ? `${query.app}.${query.name}` : undefined,
      actions: actions?.map(schema),
      inlineEdit: inlineEdit ? { ...inlineEdit, action: schema(inlineEdit.action) } : undefined,
    };
  });
  if (!["list-detail", "composed"].includes(page.layout)) throw Error("page-layout");
  return structuredClone({ title: page.title, description: page.description ?? "", object: schema(page.object),
    list: page.listFields ?? [], detail: page.detailFields ?? [], actions: (page.actions ?? []).map(schema),
    sections, document: page.document, selections: page.selections });
}
