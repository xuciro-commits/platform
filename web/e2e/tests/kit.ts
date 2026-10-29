// The browser tests' kit (see AGENTS.md): every spec imports
// `test` and `expect` from here, never from @playwright/test directly, and
// never re-implements signing in, making an object, a page, a release or the
// runtime. A spec reads as the route a person would walk; the kit is the
// hands. Members are the development host's seats: a builder (`manager`) and
// an operator (`desk`).
import { expect, test as base, type APIRequestContext, type Page } from "@playwright/test";
import { createServer } from "node:http";
import { once } from "node:events";
import type * as Api from "../../packages/kernel/src/gen/host";
import { decodePage } from "../../packages/app/src/pageui/wire";
import { decide, fresh, open, pageUIProfile, retainActionCatalog, stableReadRevision } from "./host";

/** The deployment-approved local model of the browser host: its provider id and port are fixed in playwright.config.ts. */
export const runtimeModelProvider = "runtimefixture", runtimeModelPort = 18590;

export { expect, fresh, pageUIProfile, retainActionCatalog, stableReadRevision };

const objectAreaForSection = {
  Overview: "Object structure", Properties: "Object structure", Links: "Object structure", "Record sets": "Object structure",
  Lifecycle: "Behavior", Actions: "Behavior",
  "Business constraints": "Rules and access", "Business versions": "Rules and access", Protocols: "Rules and access", Permissions: "Rules and access",
  Data: "Data and preview", Preview: "Data and preview",
} as const;

/** Selects an object section through its visible four-area navigation. */
export async function selectObjectSection(page: Page, section: keyof typeof objectAreaForSection) {
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  const area = page.getByRole("tablist", { name: "Object areas", exact: true }).getByRole("tab", { name: objectAreaForSection[section], exact: true });
  if (await area.getAttribute("aria-selected") !== "true") await area.click();
  const sections = page.getByRole("tablist", { name: "Object sections", exact: true });
  const tab = sections.getByRole("tab", { name: section, exact: true });
  if (await tab.getAttribute("aria-selected") !== "true") await tab.click();
  return { sections, tab };
}

export function protocolVersion(): string {
  const version = process.env.PROTOCOL_VERSION;
  if (!version) throw new Error("Run protocol browser tasks through make e2e so they use the packaged version.");
  return version;
}

const BUILDER = "manager";
const OPERATOR = "desk";
const APP = "build";

/** A short lowercase name usable as an object or page name. */
export const slug = (prefix: string) => fresh(prefix).replace(/[^a-z0-9]/gi, "").toLowerCase();

/** Sign in through the configured identity provider in delivery rehearsals. */
export async function signInWithOIDC(page: Page, email: string, password: string) {
  await page.goto("/");
  await page.locator('input[type="email"]').fill(email);
  await page.locator('button[type="submit"]').click();
  await page.locator('input[type="password"]').fill(password);
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("button", { name: "Applications", exact: true })).toBeVisible();
  const token = await page.evaluate(() => (JSON.parse(sessionStorage.getItem("oidc:session") ?? "null") as { accessToken?: string } | null)?.accessToken);
  if (!token) throw new Error("OIDC sign-in returned no browser session");
  return token;
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
  async read<T = any>(name: string): Promise<T> {
    const response = await this.request.get(`/v1/${name}`, { headers: this.headers });
    if (!response.ok()) throw Error(`Read ${name}: ${await response.text()}`);
    return response.json();
  }
  /** Opens the workspace at a route as this member. */
  open(page: Page, route: string) { return open(page, this.token, route); }
}

export type ObjectSpec = { name?: string; title: string; fields: unknown[]; states?: unknown[]; actions?: unknown[]; access?: unknown[]; plural?: string; implements?: string[]; extends?: string };

/** The builder's hands: objects, records, pages, releases. */
export class Builder extends Member {
  /** Registers the deployment-approved runtime provider once per host and resolves its model. */
  async runtimeModel(): Promise<string> {
    const model = `${runtimeModelProvider}/probe`;
    const providers = await this.read<{ id: string }[]>("ai-providers");
    if (!providers.some(provider => provider.id === runtimeModelProvider)) {
      await this.decide("ai.provider.add", { type: "ai.provider", id: runtimeModelProvider }, { kind: "local", baseUrl: `http://127.0.0.1:${runtimeModelPort}`, wire: "openai" }, "ai");
      await this.decide("ai.model.enable", { type: "ai.model", id: model }, { access: "users" }, "ai");
    }
    return model;
  }
  /** Configures a local test provider through the normal administrator actions. */
  async adviceModel(address: string, drafts: Api.PackageDraftResult): Promise<void> {
    await this.decide("ai.provider.add", {type:"ai.provider",id:"fixture"}, {kind:"local",baseUrl:address,wire:"openai"}, "ai");
    await this.decide("ai.model.enable", {type:"ai.model",id:"fixture/probe"}, {access:"users"}, "ai");
    for (const id of ["ai/app-model","agent/model"]) await this.decide("platform.setting.set",{type:"platform.setting",id},{value:"fixture/probe"},"platform");
    for (const draft of drafts.drafts.filter(draft=>draft.ref.kind==="evaluation-set")) await this.decide("build.testplan.edit",{type:"build.testplan",id:draft.id},{model:"fixture/probe"});
  }
  /** Runs the original measured evaluators; no fixture writes a passing report. */
  async measureCandidate(candidateId: string): Promise<number> {
    const review = await this.read<Api.SavedReleaseReview>(`releases/candidates/${candidateId}`);
    const reports: {id:string;type:string;resource:string}[]=[];
    for (const asset of review.assets) {
      if (asset.ref.kind!=="evaluation-set"&&asset.ref.kind!=="agent") continue;
      const response=await this.request.post("/v1/releases/evaluations",{headers:this.headers,data:{candidateId,key:fresh("MEASURE"),...(asset.ref.kind==="evaluation-set"?{planId:asset.ref.name}:{agent:`${asset.ref.app}.${asset.ref.name}`})}});
      if (!response.ok()) throw Error(`Candidate evaluation: ${await response.text()}`);
      reports.push({id:(await response.json()).id,type:asset.ref.kind==="agent"?"agent.evaluation":"build.evaluation",resource:asset.ref.name});
    }
    // Poll the original small reports. Rebuilding a complete release preview
    // on every tick competes with the evaluator for the tenant's writer lane.
    await expect.poll(async()=>{
      const states=await Promise.all(reports.map(async report=>{
        const row=await this.record(report.type,report.id);
        const passed=row?.candidate===candidateId&&(report.type==="agent.evaluation"?row.state==="done"&&row.score===1&&row.fails===0&&row.differs===0:row.state==="passed");
        return passed?[]:[`${report.resource}: ${row?.state??"no report"}`];
      }));
      return states.flat();
    },{timeout:60_000}).toEqual([]);
    const measured=await this.read<Api.SavedReleaseReview>(`releases/candidates/${candidateId}`);
    expect(review.assets.filter(asset=>asset.ref.kind==="function"||asset.ref.kind==="agent").flatMap(asset=>{
      const item=measured.ai?.find(item=>item.resource.ref.kind===asset.ref.kind&&item.resource.ref.name===asset.ref.name);
      return item?.evaluations.some(report=>report.candidate===candidateId&&report.state==="passed")?[]:[`${asset.ref.kind}/${asset.ref.name}: missing passing candidate report`];
    })).toEqual([]);
    return review.assets.filter(asset=>asset.ref.kind==="function"||asset.ref.kind==="agent").length;
  }
  /** Seals an existing object through its original release owner. */
  async candidate(id: string | { kind: string; id: string }[]): Promise<string> {
    const selection = typeof id === "string" ? { kind: "object", id } : { drafts: id };
    const response = await this.request.post("/v1/releases/preview", { headers: this.headers, data: selection });
    const preview = await response.json();
    if (!response.ok() || preview.diagnostic || !preview.candidateId) throw Error(`Candidate preview: ${JSON.stringify(preview)}`);
    const saved = await this.request.post("/v1/releases/candidates", { headers: this.headers, data: { ...selection, candidateId: preview.candidateId, key: fresh("CANDIDATE") } });
    const answer = await saved.json();
    if (!saved.ok() || answer.id !== preview.candidateId) throw Error(`Candidate save: ${JSON.stringify(answer)}`);
    return answer.id;
  }
  /** Activates a sealed candidate through the original release owner. */
  async activate(candidateId: string): Promise<void> {
    const response = await this.request.post("/v1/releases/active", { headers: this.headers, data: { candidateId, key: fresh("ACTIVATE") } });
    if (!response.ok()) throw Error(`Candidate activation: ${await response.text()}`);
  }
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
  /** A native page draft with supplied sections or one editor placeholder. */
  async page(spec: { name?: string; title: string; object?: string; actions?: string[]; sections?: unknown[]; document?: unknown }): Promise<{ id: string; name: string }> {
    const name = spec.name ?? slug("page"), id = fresh("PAGE");
    await this.decide("build.page.create", { type: "build.page", id }, {
      name, title: spec.title, object: spec.object, actions: spec.actions,
      sections: spec.sections ?? [{ id: "initial", widget: "text", configVersion: 1, text: "Initial" }],
      document: spec.document===null?undefined:spec.document ?? { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["initial"] }, initial: { kind: "widget", section: "initial" } } },
    });
    return { id, name };
  }
  /** An application draft over original published resources; publication stays explicit. */
  async application(spec: {name?:string;title:string;pages?:string[];groups?:Api.Application["groups"];languages?:Api.Application["languages"];resources?:unknown[];profile?:unknown;extends?:{app:string;kind:string;name:string};entry?:string;replacements?:{view:string;page:string}[]}): Promise<{id:string;name:string}> {
    const name=spec.name??slug("app"),id=fresh("APP");
    await this.decide("build.app.create",{type:"build.app",id},{...spec,name,pages:spec.pages??[]});
    return {id,name};
  }
  /** A page draft as the renderer sees it: the five-element record decoded back to widget sections and document. */
  async draft(pageID: string): Promise<any> {
    const record = await this.record("build.page", pageID);
    return record.format === 3 ? { ...record, ...decodePage(record) } : record;
  }
  /** The draft shaped for build.page.edit: action names, and no binding the host derives itself (a model without chosen fields). */
  async editable(pageID: string): Promise<{ sections: any[]; document: any }> {
    const { sections = [], document } = await this.draft(pageID);
    return {
      document,
      sections: sections.map((section: any) => {
        const { binding, triggers, ...rest } = section;
        if (Array.isArray(rest.actions)) rest.actions = rest.actions.map((a: any) => a?.name ?? a);
        if (!rest.model?.fields?.length && !rest.query) { delete rest.object; delete rest.model; delete rest.fields; }
        return rest;
      }),
    };
  }
  async sections(pageID: string): Promise<any[]> {
    return (await this.draft(pageID)).sections ?? [];
  }
  /** Rewrites a page draft's sections and document over the API (what a later edit would do). */
  edit(pageID: string, draft: { sections?: unknown; document?: unknown }) {
    return this.decide("build.page.edit", { type: "build.page", id: pageID }, draft);
  }
}

/** This provider proves mechanics and human review, not real model quality. */
export async function scriptedAdviceProvider() {
  const server=createServer(async(request,response)=>{
    let raw="";for await(const chunk of request)raw+=chunk;
    const body=JSON.parse(raw);
    const message=body.tools?.length
      ? {tool_calls:[{id:"ask-target",type:"function",function:{name:"ask",arguments:JSON.stringify({question:"Which confirmed record and facts should I review?",answers:["Provide the target"],rationale:"A confirmed target is required before a business change."})}}]}
      : {content:JSON.stringify({summary:"Review the actual record and confirm missing facts with the responsible person.",category:"review",review:true,confidence:0.9})};
    response.setHeader("Content-Type","application/json");
    response.end(JSON.stringify({choices:[{message}],usage:{prompt_tokens:10,completion_tokens:10,cost:0.001}}));
  });
  server.listen(0,"127.0.0.1");await once(server,"listening");
  const address=server.address();if(!address||typeof address==="string")throw Error("Advice provider did not listen");
  return {address:`http://127.0.0.1:${address.port}`,close:async()=>{server.closeAllConnections();await new Promise<void>((resolve,reject)=>server.close(error=>error?reject(error):resolve()));}};
}

/** Reviews a proposal on the selected record's normal application page. */
export async function adoptAdvice(page: Page, member: Member, type: string, id: string) {
  const before=await member.record(type,id);
  const requested=page.waitForResponse(response=>response.request().method()==="POST"&&response.url().endsWith("/v1/submissions")&&response.request().postDataJSON().schema?.name==="build.function-call.start");
  await page.getByRole("button",{name:"Request original analysis",exact:true}).click();
  const accepted=await requested;expect((await accepted.json()).error).toBeUndefined();
  const callID=accepted.request().postDataJSON().target.id as string;
  await expect.poll(async()=>(await member.record("build.function-call",callID)).state,{timeout:20_000}).toBe("ready");
  expect(await member.record(type,id)).toEqual(before);
  const review=page.getByRole("region",{name:"Proposal review",exact:true});
  await review.getByRole("textbox",{name:"Review advice *",exact:true}).fill("Confirmed with the responsible person; review the remaining facts.");
  await review.getByRole("button",{name:"Adopt proposal",exact:true}).click();
  await expect.poll(async()=>(await member.record("build.function-call",callID)).decision).toBe("adopted");
  expect(await member.record(type,id)).toMatchObject({state:before.state,advice:"Confirmed with the responsible person; review the remaining facts.",advicecategory:"review",advicereview:true});
  await expect(review.getByText("adopted",{exact:true})).toBeVisible();
}

/** Starts a declared assistant through the shared workspace and opens its run. */
export async function askAssistant(page: Page, member: Member, agent: string) {
  await page.getByRole("button",{name:"Assistant",exact:true}).click();
  await page.getByRole("combobox",{name:"Agent",exact:true}).selectOption(agent);
  await page.getByRole("textbox",{name:"Goal",exact:true}).fill("Help me review a business task; ask for the confirmed target before changing it.");
  const started=page.waitForResponse(response=>response.request().method()==="POST"&&response.url().endsWith("/v1/submissions")&&response.request().postDataJSON().schema?.name==="agent.run.start");
  await page.getByRole("button",{name:"Ask",exact:true}).click();
  const accepted=await started;expect((await accepted.json()).error).toBeUndefined();
  const id=accepted.request().postDataJSON().target.id as string;
  await expect.poll(async()=>(await member.record("agent.run",id)).state,{timeout:20_000}).toBe("waiting");
  await member.open(page,`/run?id=${id}`);
  const run=page.getByRole("region",{name:id,exact:true}).filter({has:page.getByRole("heading",{name:"Help me review a business task; ask for the confirmed target before changing it.",exact:true})});
  await expect(run.getByText("asked: Which confirmed record and facts should I review?",{exact:true})).toBeVisible();
  await run.getByRole("button",{name:"Stop",exact:true}).click();
  await expect.poll(async()=>(await member.record("agent.run",id)).state).toBe("stopped");
  await expect(run.getByText(/^Stopped: stopped by /)).toBeVisible();
}

/** The page editor within the current Workshop module workbench. */
export class Editor {
  readonly tree; readonly inspector;
  constructor(readonly page: Page) {
    this.tree = page.getByRole("region", { name: "Page structure", exact: true });
    this.inspector = page.getByRole("region", { name: "Inspector", exact: true });
  }
  async open(builder: Builder, pageID: string, mode: "visual" | "source" = "visual") {
    await builder.open(this.page, `/module?page=${pageID}&surface=studio`);
    if (mode === "visual") await this.visual();
    else await this.page.locator('[data-structured-editor^="build.page:"] > div > [role="group"]').getByRole("button", { name: "Source editor", exact: true }).click();
  }
  async visual() {
    const mode = this.page.locator('[data-structured-editor^="build.page:"] > div > [role="group"]');
    const visual = mode.getByRole("button", { name: "Visual", exact: true });
    if (await visual.getAttribute("aria-pressed") !== "true") await visual.click();
  }
  async select(widgetTitle: string) {
    await this.visual();
    await this.tree.getByRole("button", { name: widgetTitle, exact: true }).click();
  }
  get publish() { return this.page.getByRole("button", { name: "Publish", exact: true }); }
  /** The current canvas saves valid edits after a pause; Cmd/Ctrl-S requests it immediately. */
  save() { return this.page.keyboard.press("ControlOrMeta+s"); }
  /** Requests saving, then confirms the original host's saved section count. */
  async saveUntil(builder: Builder, pageID: string, widget: string, count: number): Promise<any> {
    await this.save();
    let saved: any;
    await expect.poll(async () => { saved = await builder.draft(pageID); return saved.sections?.filter((s: any) => s.widget === widget).length; }).toBe(count);
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

/** Saves the real built shell and this member's bounded application reads. */
export async function prepareOffline(page:Page){
  await page.getByRole("button",{name:"Offline applications",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"Offline applications",exact:true});
  await dialog.getByRole("button",{name:"Prepare for offline",exact:true}).click();
  await expect(dialog.getByRole("status").filter({hasText:/Prepared at/})).toBeVisible({timeout:60_000});
  await page.keyboard.press("Escape");await expect(dialog).not.toBeVisible();
  // Ordinary HTTP memory/disk cache must not impersonate a prepared shell.
  const browser=await page.context().newCDPSession(page);await browser.send("Network.clearBrowserCache");await browser.detach();
}

/** Closes the old document and opens its known development identity again.
 * The original sign-in hands supply that identity; no token enters snapshots. */
export async function reopenPrepared(page:Page,member:Member,route:string){
  const context=page.context();await page.close();
  const reopened=await context.newPage();reopened.setDefaultTimeout(20_000);
  await member.open(reopened,route);return reopened;
}

/** Screenshots a locator at desktop and phone widths when PLATFORM_SCREENSHOTS is set. */
export async function shots(tab: Page, target: ReturnType<Page["getByRole"]>, name: string, info: { outputPath(p: string): string }, ready?: () => Promise<unknown>) {
  if (!process.env.PLATFORM_SCREENSHOTS) return;
  await ready?.();
  await target.screenshot({ path: info.outputPath(`${name}.png`) });
  await tab.setViewportSize({ width: 390, height: 844 });
  await tab.evaluate(() => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r()))));
  await ready?.();
  await target.screenshot({ path: info.outputPath(`${name}-narrow.png`) });
  await tab.setViewportSize({ width: 1280, height: 720 });
}

export const test = base.extend<{ builder: Builder; operator: Member; editor: Editor }>({
  builder: async ({ request }, use) => { await use(new Builder(request, BUILDER)); },
  operator: async ({ request }, use) => { await use(new Member(request, OPERATOR)); },
  editor: async ({ page }, use) => { await use(new Editor(page)); },
});
