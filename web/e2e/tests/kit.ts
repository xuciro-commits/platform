// The browser tests' kit (docs/Testing.md "测试规范"): every spec imports
// `test` and `expect` from here, never from @playwright/test directly, and
// never re-implements signing in, making an object, a page, a release or the
// runtime. A spec reads as the route a person would walk; the kit is the
// hands. Members are the development host's seats: a builder (`manager`) and
// an operator (`desk`).
import { readFileSync } from "node:fs";
import { expect, test as base, type APIRequestContext, type Page } from "@playwright/test";
import { decide, fresh, open, pageUIProfile, stableReadRevision } from "./host";

export { expect, fresh, open, pageUIProfile, stableReadRevision };

export const BUILDER = "manager";
export const OPERATOR = "desk";
const APP = "build";

/** A short lowercase name usable as an object or page name. */
export const slug = (prefix: string) => fresh(prefix).replace(/[^a-z0-9]/gi, "").toLowerCase();

/** The sample Workshop module the import dialog understands, fresh each call. */
export function sampleModule(): any {
  return JSON.parse(readFileSync(new URL("../../packages/build/src/workshop/module-import/sample.workshop.json", import.meta.url), "utf8"));
}

/** The host as one member: decisions over the API, records read back. */
export class Member {
  constructor(readonly request: APIRequestContext, readonly token: string) {}
  get headers() { return { Authorization: `Bearer ${this.token}` }; }
  decide(schema: string, target: { type: string; id: string }, payload: unknown, app = APP) {
    return decide(this.request, this.token, app, schema, target, payload);
  }
  async record<T = any>(type: string, id: string): Promise<T> {
    return (await (await this.request.get(`/v1/records/${type}/${id}`, { headers: this.headers })).json()).record;
  }
  async definitions<T = any>(): Promise<T[]> {
    return (await this.request.get("/v1/definitions", { headers: this.headers })).json();
  }
  /** Opens the workspace at a route as this member. */
  open(page: Page, route: string) { return open(page, this.token, route); }
}

export type ObjectSpec = { name?: string; title: string; fields: unknown[]; states?: unknown[]; actions?: unknown[]; access?: unknown[]; plural?: string; implements?: string[] };

/** The builder's hands: objects, records, pages, releases. */
export class Builder extends Member {
  /** Creates and publishes an object; resolves to its type (`build.<name>`). */
  async object(spec: ObjectSpec): Promise<{ type: string; name: string; id: string }> {
    const name = spec.name ?? slug("obj"), id = fresh("OBJ");
    await this.decide("build.object.create", { type: "build.object", id }, { ...spec, name });
    await this.decide("build.object.publish", { type: "build.object", id }, {});
    return { type: `build.${name}`, name, id };
  }
  /** Creates records of a published type as this member. */
  async records(type: string, rows: Record<string, unknown>[], prefix = "REC"): Promise<string[]> {
    const ids: string[] = [];
    for (const row of rows) { const id = fresh(prefix); await this.decide(`${type}.create`, { type, id }, row); ids.push(id); }
    return ids;
  }
  /** A page draft with one placeholder section, ready for the editor or an import. */
  async page(spec: { name?: string; title: string; object?: string; sections?: unknown[]; document?: unknown }): Promise<{ id: string; name: string }> {
    const name = spec.name ?? slug("page"), id = fresh("PAGE");
    await this.decide("build.page.create", { type: "build.page", id }, {
      name, title: spec.title, object: spec.object,
      sections: spec.sections ?? [{ id: "initial", widget: "text", configVersion: 1, text: "Initial" }],
      document: spec.document ?? { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["initial"] }, initial: { kind: "widget", section: "initial" } } },
    });
    return { id, name };
  }
  /** Rewrites a page draft's sections and document over the API (what a later edit would do). */
  edit(pageID: string, draft: { sections?: unknown; document?: unknown }) {
    return this.decide("build.page.edit", { type: "build.page", id: pageID }, draft);
  }
}

/** The page editor within the current Workshop module workbench. */
export class Editor {
  readonly tree; readonly inspector;
  constructor(readonly page: Page) {
    this.tree = page.getByRole("region", { name: "Page structure", exact: true });
    this.inspector = page.getByRole("region", { name: "Inspector", exact: true });
  }
  open(builder: Builder, pageID: string) { return builder.open(this.page, `/module?page=${pageID}&surface=studio`); }
  /** Imports a Workshop module through the dialog; `map` chooses object/field/metric targets. */
  async importModule(module: unknown, map?: (dialog: ReturnType<Page["getByRole"]>) => Promise<void>) {
    await this.page.getByRole("button", { name: "More page commands", exact: true }).click();
    await this.page.getByRole("menuitem", { name: "Import Workshop module…", exact: true }).click();
    const dialog = this.page.getByRole("dialog", { name: "Import Workshop module", exact: true });
    await dialog.getByRole("textbox", { name: "Source module JSON", exact: true }).fill(JSON.stringify(module));
    await map?.(dialog);
    await dialog.getByRole("checkbox").check();
    await dialog.getByRole("button", { name: "Apply imported page draft", exact: true }).click();
  }
  select(widgetTitle: string) { return this.tree.getByRole("button", { name: widgetTitle, exact: true }).click(); }
  get publish() { return this.page.getByRole("button", { name: "Publish", exact: true }); }
  /** The current canvas saves valid edits after a pause; Cmd/Ctrl-S requests it immediately. */
  save() { return this.page.keyboard.press("ControlOrMeta+s"); }
  /** Requests saving, then confirms the original host's saved section count. */
  async saveUntil(builder: Builder, pageID: string, widget: string, count: number): Promise<any> {
    await this.save();
    let saved: any;
    await expect.poll(async () => { saved = await builder.record("build.page", pageID); return saved.sections?.filter((s: any) => s.widget === widget).length; }).toBe(count);
    return saved;
  }
  /** The release route: review, check, immutable candidate. `between` runs before activation. */
  async release(between?: () => Promise<void>, joint = false, command = "Publish") {
    await this.page.getByRole("button", { name: command, exact: true }).click();
    if (joint) await this.page.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
    await this.page.getByRole("button", { name: joint ? "Check joint candidate" : "Check draft and dependencies", exact: true }).click();
    await this.page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await between?.();
    await this.page.getByRole("button", { name: "Activate release", exact: true }).click();
  }
}

/** The released page as the operator sees it, in a second tab with a stable read revision. */
export async function runtime(page: Page, operator: Member, pageName: string, app = APP): Promise<Page> {
  const tab = await page.context().newPage();
  await stableReadRevision(tab);
  await operator.open(tab, `/page?app=${app}&kind=page&name=${pageName}`);
  return tab;
}

/** Screenshots a locator at desktop and phone widths when PLATFORM_SCREENSHOTS is set. */
export async function shots(tab: Page, target: ReturnType<Page["getByRole"]>, name: string, info: { outputPath(p: string): string }) {
  if (!process.env.PLATFORM_SCREENSHOTS) return;
  await target.screenshot({ path: info.outputPath(`${name}.png`) });
  await tab.setViewportSize({ width: 390, height: 844 });
  await tab.evaluate(() => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r()))));
  await target.screenshot({ path: info.outputPath(`${name}-narrow.png`) });
  await tab.setViewportSize({ width: 1280, height: 720 });
}

export const test = base.extend<{ builder: Builder; operator: Member; editor: Editor }>({
  builder: async ({ request }, use) => { await use(new Builder(request, BUILDER)); },
  operator: async ({ request }, use) => { await use(new Member(request, OPERATOR)); },
  editor: async ({ page }, use) => { await use(new Editor(page)); },
});
