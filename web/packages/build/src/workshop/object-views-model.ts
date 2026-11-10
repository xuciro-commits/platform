import { encodePage } from "@platform/app/page-contract";
import { pageDocumentFromSections, type PageUI } from "@platform/app";
import type { Api } from "@platform/api";
import { t, type EntityInfo } from "@platform/ui";
import { useDraftInventory } from "../branches/records";
import type { Project } from "../projects/project";

export type ObjectViewPage = Omit<Partial<Api.Page>, "object"> & {
  id: string; revision: number; name: string; title: string; object: string; state: string; archived?: boolean; changed?: Api.Stamp;
};

export const objectViewKey = (view: Api.ObjectView) => JSON.stringify([view.application.app, view.application.kind, view.application.name, view.page.app, view.page.kind, view.page.name]);

export function useObjectViews() {
  const modules = useDraftInventory<Project>("build.app");
  const pages = useDraftInventory<ObjectViewPage>("build.page");
  const rows = (modules.data?.records ?? []).filter(module => !module.archived).flatMap(module =>
    (pages.data?.records ?? []).filter(page => {
      if (page.archived || !module.pages?.includes(page.name)) return false;
      const inputs = page.inputSchema?.interface?.inputs;
      const input = inputs?.currentObject;
      return input?.type === "record" && input.required === true && input.object?.kind === "object" && input.object.app === page.object.split(".")[0] && input.object.name === page.object && !Object.entries(inputs ?? {}).some(([name, port]) => name !== "currentObject" && port.required);
    }).map(page => ({ module, page, binding: {
      application: { app: "build", kind: "app", name: module.name }, page: { app: "build", kind: "page", name: page.name },
    } satisfies Api.ObjectView })));
  return { modules, pages, rows, isLoading: modules.isLoading || pages.isLoading, isError: modules.isError || pages.isError };
}

export function objectViewDraft(entity: EntityInfo, name: string, title: string, offeredActions: string[]) {
  const object = { app: entity.app, kind: "object", name: entity.type };
  const fields = entity.fields.filter(field => field.type !== "lines").map(field => field.name);
  const actions = offeredActions.map(schema => ({ app: schema.split(".")[0]!, kind: "action", name: schema }));
  const { sections, document } = pageDocumentFromSections<PageUI.Section>([{ widget: "record-view", width: "full", object, fields, actions, recordVariable: "currentObject", recordView: { tabs: ["overview", "properties", "links", "history"] } }]);
  document.variables = { currentObject: { title: t("Current object"), scope: "page", type: "record", mode: "input" } };
  document.interface = { version: 1, inputs: { currentObject: { type: "record", variable: "currentObject", required: true, object } } };
  return { ...encodePage({ name, title, object, layout: "composed", listFields: fields, detailFields: fields, actions, sections, document }), object: entity.type };
}
