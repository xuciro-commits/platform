import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { StrictMode, useState, type ReactNode } from "react";
import { pageUIManifest, type Api, type EdgeClient } from "@platform/kernel";
import { ApplicationPage, ApplicationSessionsProvider, ComposedPage, HostContext, useHost, type Definition, type Host } from "../index";
import { CatalogFixture, FixtureClient, sampleDefinitions, samplePage, sampleObject, sampleRecords, sampleSource } from "../catalog.fixtures";
import { usePageEffects } from "./PageNavigation";
import { RecordCollaborationRenderer } from "../widgets/RecordCollaboration";
import { WorkViewsRenderer } from "../widgets/WorkViews";
import { AIWidget } from "../widgets/AIWidget";
import { ComputeCall } from "../automation/capability";
import { InlineActionForm } from "../actions/actions";
import { CreateFormRenderer } from "../widgets/CreateForm";
import { usePageComputations } from "./PageComputations";
import { PageSessionStore } from "./Session";
import { recordResourceSlot } from "./resources";

/** Records every mutating port the rendered page attempts, so the assertion is
 * "no host/client write happened", never "a button looked disabled". */
function makeSpy() {
  const events: string[] = [];
  const decide: Host["decide"] = async (schema) => { events.push(`decide:${String(schema)}`); return false; };
  return { events, decide };
}

/** FixtureClient that also records the raw mutating client ports. */
class RecordingClient extends FixtureClient {
  constructor(private readonly events: string[]) { super({}); }
  override async call<T>(...args: unknown[]): Promise<{ ok: boolean; status: number; body: T }> {
    this.events.push(`client.call:${String(args[0])}`);
    return super.call<T>();
  }
  override async upload(..._args: unknown[]): Promise<never> {
    this.events.push("client.upload");
    return super.upload();
  }
  override async importCSV(..._args: unknown[]): Promise<never> {
    this.events.push("client.importCSV");
    return super.importCSV();
  }
}

const appDefinition: Definition = {
  ref: { app: "catalog", kind: "app", name: "shared" }, source: "catalog-fixture", version: "fixture-v1", contractVersion: 1, requires: [], provides: [],
  application: { name: "shared", title: "Shared application", pages: ["samples"], uiProfile: "platform.page.v2.12", variables: {}, queries: {} },
};

const page = {
  ...samplePage.page, layout: "composed" as const,
  selections: [{ name: "first", object: samplePage.page.object }],
  sections: [
    { id: "records", widget: "table", configVersion: 1, title: "Records", selection: "first", fields: ["title", "state", "quantity"], width: "full" },
    { id: "actions", widget: "actions", configVersion: 1, title: "Actions", selection: "first",
      actions: [{ app: "catalog", kind: "action" as const, name: "catalog.sample.create" }, ...samplePage.page.actions], width: "full" },
  ],
};

const publishedRoute = { view: "page" as const, params: { application: "catalog:shared", instance: "main" } };
const previewRoute = { view: "page-preview" as const, params: { application: "catalog:shared", instance: "main" } };
const standaloneRoute = { view: "page-preview" as const, params: {} };

afterEach(cleanup);

async function renderedPage(ui: React.ReactElement) {
  const view = render(ui);
  await waitFor(() => expect(screen.getByText("1\u20132 of 2")).toBeTruthy(), { timeout: 10000 });
  return view;
}

/** Attempts a create action as far as the current mode allows, through the
 * same buttons a person would use. Returns every recorded port event. */
async function attemptCreate(spy: { events: string[] }) {
  const trigger = screen.queryByRole("button", { name: /create/i });
  if (trigger) {
    fireEvent.click(trigger);
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 30)); });
    const dialog = screen.queryByRole("dialog");
    if (dialog) {
      const title = within(dialog).queryByLabelText(/title/i);
      if (title) fireEvent.change(title, { target: { value: "Attempted from preview" } });
      const submit = within(dialog).queryAllByRole("button", { name: /create/i }).at(-1);
      if (submit && !(submit as HTMLButtonElement).disabled) fireEvent.click(submit);
      await act(async () => { await new Promise((resolve) => setTimeout(resolve, 30)); });
    }
  }
  return spy.events;
}

test("published application runtime stays live: the action port is reachable (positive control)", async () => {
  const spy = makeSpy();
  await renderedPage(
    <CatalogFixture client={new RecordingClient(spy.events)} decide={spy.decide} definitions={[...sampleDefinitions, appDefinition]}>
      <ApplicationSessionsProvider>
        <ApplicationPage pageRef={samplePage.ref} route={publishedRoute}><ComposedPage page={page} /></ApplicationPage>
      </ApplicationSessionsProvider>
    </CatalogFixture>);
  const events = await attemptCreate(spy);
  expect(events).toContain("decide:catalog.sample.create");
});

test.each([
  ["standalone new page", standaloneRoute, sampleDefinitions, "new-page"],
  ["new page of an unpublished application", previewRoute, sampleDefinitions, "new-page"],
  ["unpublished page added to a published application", previewRoute, [...sampleDefinitions, appDefinition], "new-page"],
  ["existing page of a published application", previewRoute, [...sampleDefinitions, appDefinition], "samples"],
])("authoring preview of a %s records no host or client write", async (_label, route, definitions, name) => {
  const spy = makeSpy();
  await renderedPage(
    <CatalogFixture client={new RecordingClient(spy.events)} decide={spy.decide} definitions={definitions}>
      <ApplicationSessionsProvider>
        <ApplicationPage pageRef={{ ...samplePage.ref, name }} route={route} preview><ComposedPage page={page} /></ApplicationPage>
      </ApplicationSessionsProvider>
    </CatalogFixture>);
  const events = await attemptCreate(spy);
  expect(events).toEqual([]);
  // Authorized presentation and reads still work in preview...
  expect(screen.getByText("1–2 of 2")).toBeTruthy();
  // ...while the actions port is replaced by the preview boundary notice.
  expect(screen.getByText("Actions do not run while you compose.")).toBeTruthy();
});

test("an embedded published page inherits its author's preview boundary", async () => {
  const spy = makeSpy(), contentVersion = `page.sha256.${"a".repeat(64)}`;
  class EmbeddedClient extends RecordingClient {
    override async pageContent() { return { ...samplePage, contentVersion, page }; }
  }
  const parent = { ...page, sections: [{ id: "embedded", widget: "embedded-page", configVersion: 1, title: "Nested workspace",
    embedding: { kind: "module", page: { ref: samplePage.ref, sourceVersion: samplePage.version }, contentVersion, interfaceVersion: 0 } }] };
  await renderedPage(
    <CatalogFixture client={new EmbeddedClient(spy.events)} decide={spy.decide} definitions={[...sampleDefinitions, appDefinition]}>
      <ApplicationSessionsProvider><ApplicationPage pageRef={samplePage.ref} route={previewRoute} preview>
        <ComposedPage page={parent} />
      </ApplicationPage></ApplicationSessionsProvider>
    </CatalogFixture>);
  expect(await attemptCreate(spy)).toEqual([]);
  expect(screen.getByText("Actions do not run while you compose.")).toBeTruthy();
});

function EffectHost({ children }: { children: ReactNode }) {
  const host = useHost();
  const catalog = host.catalog.map(action => ({ ...action, payload: [] }));
  return <HostContext.Provider value={{ ...host, catalog, action: schema => catalog.find(action => action.schema === schema) }}>{children}</HostContext.Provider>;
}

function EffectChain({ live, withForm = false }: { live: boolean; withForm?: boolean }) {
  const [finished, setFinished] = useState(false);
  const effects = usePageEffects(page, live, {}, () => {});
  const action: Api.PageEffect = { kind: "action", action: { ref: { app: "catalog", kind: "action", name: "catalog.sample.review" }, recordVariable: "row" } };
  return <><button onClick={() => void effects.run({ source: "run", event: "click", effects: [action, withForm ? { kind: "set", target: "finished", value: true } : action] }, {
    values: {}, set: () => setFinished(true), isActive: () => true, record: () => ({ id: "SAMPLE-001", revision: 1 }),
  })}>Run effect chain</button>{effects.dialog}{finished && <span>Effect chain finished</span>}</>;
}

test("StrictMode keeps live page effects callable after its effect cleanup rehearsal", async () => {
  const events: string[] = [];
  render(<StrictMode><CatalogFixture decide={async schema => { events.push(schema); return true; }}><EffectHost><EffectChain live /></EffectHost></CatalogFixture></StrictMode>);
  fireEvent.click(screen.getByRole("button", { name: "Run effect chain" }));
  await waitFor(() => expect(events).toEqual(["catalog.sample.review", "catalog.sample.review"]));
});

test("a delayed accepted action cannot resume its chain after live mode was revoked and restored", async () => {
  const events: string[] = [];
  let finish!: (accepted: boolean) => void;
  const accepted = new Promise<boolean>(resolve => { finish = resolve; });
  const decide: Host["decide"] = async schema => { events.push(schema); return accepted; };
  const ui = (live: boolean) => <CatalogFixture decide={decide}><EffectHost><EffectChain live={live} /></EffectHost></CatalogFixture>;
  const view = render(ui(true));
  fireEvent.click(screen.getByRole("button", { name: "Run effect chain" }));
  expect(events).toEqual(["catalog.sample.review"]);
  view.rerender(ui(false));
  view.rerender(ui(true));
  await act(async () => { finish(true); await accepted; });
  expect(events).toEqual(["catalog.sample.review"]);
});

test("an accepted action form resumes its page effect chain", async () => {
  const spy = makeSpy();
  render(<CatalogFixture decide={async (schema, ...rest) => { await spy.decide(schema, ...rest); return true; }}><EffectChain live withForm /></CatalogFixture>);
  fireEvent.click(screen.getByRole("button", { name: "Run effect chain" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText(/Review note/), { target: { value: "Confirmed original task" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Review" }));
  await screen.findByText("Effect chain finished");
  expect(spy.events).toEqual(["decide:catalog.sample.review"]);
});

test("revoking live mode retires an unsubmitted action form", async () => {
  const spy = makeSpy();
  const ui = (live: boolean) => <CatalogFixture decide={spy.decide}><EffectChain live={live} withForm /></CatalogFixture>;
  const view = render(ui(true));
  fireEvent.click(screen.getByRole("button", { name: "Run effect chain" }));
  await screen.findByRole("dialog");
  view.rerender(ui(false));
  expect(screen.queryByRole("dialog")).toBeNull();
  view.rerender(ui(true));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.queryByText("Effect chain finished")).toBeNull();
  expect(spy.events).toEqual([]);
});

function PortHost({ ports, children }: { ports: Partial<Host>; children: ReactNode }) {
  return <HostContext.Provider value={{ ...useHost(), ...ports }}>{children}</HostContext.Provider>;
}

test("an upload begun in live mode cannot attach its delayed bytes after preview revokes writes", async () => {
  const spy = makeSpy(), client: EdgeClient = new FixtureClient({}), selected: string[] = [];
  let finish!: (bytes: Awaited<ReturnType<EdgeClient["upload"]>>) => void;
  client.upload = () => { spy.events.push("upload"); return new Promise(resolve => { finish = resolve; }); };
  const ui = (live: boolean) => <CatalogFixture client={client} decide={spy.decide}><PortHost ports={{ can: () => true }}>
    <RecordCollaborationRenderer kind="record-uploader" label="Original attachment" record={sampleRecords[0]} reference={{ object: sampleObject.type, id: sampleRecords[0]!.id }} status="value" slot="original" captureRecordLease={() => () => true} live={live} onFileID={id => selected.push(id)} />
  </PortHost></CatalogFixture>;
  const view = render(ui(false)), file = new File(["original bytes"], "note.txt", { type: "text/plain" });
  fireEvent.change(view.container.querySelector('input[type="file"]')!, { target: { files: [file] } });
  expect(screen.queryByRole("button", { name: "Upload and attach" })).toBeNull();
  expect(spy.events).toEqual([]);
  view.rerender(ui(true));
  fireEvent.change(view.container.querySelector('input[type="file"]')!, { target: { files: [file] } });
  fireEvent.click(screen.getByRole("button", { name: "Upload and attach" }));
  expect(spy.events).toEqual(["upload"]);
  view.rerender(ui(false));
  await act(async () => { finish({ hash: "a".repeat(64), name: file.name, size: file.size, contentType: file.type }); });
  expect(spy.events).toEqual(["upload"]);
  expect(selected).toEqual([]);
  view.rerender(ui(true));
  expect((screen.getByRole("button", { name: "Upload and attach" }) as HTMLButtonElement).disabled).toBe(false);
});

test("an authoring overlay keeps its create form inside the preview boundary", async () => {
  const spy = makeSpy();
  const overlayPage = { ...page, sections: [{ id: "create", widget: "form", configVersion: 1, fields: ["title"] }], document: {
    formatVersion: 2, uiProfile: pageUIManifest.uiProfile, root: "root", nodes: { root: { kind: "rows", children: [] }, panel: { kind: "rows", children: ["create"] }, create: { kind: "widget", section: "create" } },
    variables: { open: { scope: "page", type: "boolean", mode: "state", initial: true } }, overlays: { original: { kind: "dialog", title: "Original overlay", root: "panel", openVariable: "open" } },
  } };
  render(<CatalogFixture client={new RecordingClient(spy.events)} decide={spy.decide}>
    <ApplicationSessionsProvider><ApplicationPage pageRef={samplePage.ref} route={previewRoute} preview><ComposedPage page={overlayPage} live /></ApplicationPage></ApplicationSessionsProvider>
  </CatalogFixture>);
  const overlay = await screen.findByRole("dialog", { name: "Original overlay" });
  const submit = await within(overlay).findByRole("button", { name: "Create" });
  expect(submit.matches(":disabled")).toBe(true); fireEvent.click(submit);
  fireEvent.keyDown(overlay, { key: "Escape" });
  expect(spy.events).toEqual([]);
});

test("a create form queued for validation cannot submit after its page becomes preview", async () => {
  const spy = makeSpy();
  const ui = (live: boolean) => <CatalogFixture decide={spy.decide}><CreateFormRenderer object={sampleObject.type} parentObject={sampleObject.type} config={{ fields: ["title"] }} live={live} /></CatalogFixture>;
  const view = render(ui(true));
  await waitFor(() => expect((screen.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.change(screen.getByLabelText("Title *"), { target: { value: "Queued create" } });
  act(() => { fireEvent.submit(view.container.querySelector("form")!); view.rerender(ui(false)); });
  await act(async () => { await Promise.resolve(); });
  expect(spy.events).toEqual([]);
});

test("a generated action queued for validation cannot submit after preview closes its form", async () => {
  const spy = makeSpy();
  const ui = (live: boolean) => <CatalogFixture decide={spy.decide}><ComposedPage page={page} live={live} /></CatalogFixture>;
  const view = await renderedPage(ui(true));
  fireEvent.click(screen.getByRole("button", { name: "Create" }));
  const form = await screen.findByRole("dialog");
  fireEvent.change(within(form).getByLabelText("Title *"), { target: { value: "Queued action" } });
  act(() => { fireEvent.submit(form.querySelector("form")!); view.rerender(ui(false)); });
  await act(async () => { await Promise.resolve(); });
  expect(spy.events).toEqual([]);
});

test("preview blocks Work decisions, including a live click queued before its mode changed", async () => {
  const spy = makeSpy(), client = new FixtureClient({ "/v1/notifications": [{ id: "NOTICE", member: "catalog-fixture", title: "Original notice", at: "2026-10-24T12:00:00Z", read: false }] });
  const ui = (live: boolean) => <CatalogFixture client={client} decide={spy.decide}><PortHost ports={{ can: () => true }}>
    <WorkViewsRenderer kind="notification-feed" label="Original notices" live={live} />
  </PortHost></CatalogFixture>;
  const view = render(ui(false));
  const mark = await screen.findByRole("button", { name: "Mark read" });
  fireEvent.click(mark); expect(spy.events).toEqual([]);
  view.rerender(ui(true));
  act(() => { fireEvent.click(screen.getByRole("button", { name: "Mark read" })); view.rerender(ui(false)); });
  await act(async () => { await Promise.resolve(); });
  expect(spy.events).toEqual([]);
  view.rerender(ui(true));
  fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
  await waitFor(() => expect(spy.events).toEqual(["decide:platform.notification.read"]));
});

test("preview denies AI and inline actions and a delayed AI proposal cannot invoke adoption", async () => {
  const spy = makeSpy(); let callID = "", finish!: (accepted: boolean) => void;
  const accepted = new Promise<boolean>(resolve => { finish = resolve; });
  const decide: Host["decide"] = async (schema, target) => { spy.events.push(`decide:${schema}`); callID = target.id; return accepted; };
  const source = { ...sampleSource, get: async (type: string, id: string) => type !== "build.function-call" ? sampleSource.get(type, id) : {
    record: { ...sampleRecords[0]!, id: callID, app: "catalog", function: "advice", version: 1, member: "catalog-fixture", source: `${sampleObject.type}/${sampleRecords[0]!.id}`, state: "ready", decision: "pending", output: JSON.stringify({ summary: "Review the retained record" }),
      contract: { name: "advice", object: sampleObject.type, proposal: { expiresAfterSeconds: 300 }, output: [{ name: "summary", type: "string" }] } },
    history: [], related: [], linked: [], activity: [], processes: [], approvals: [], tasks: [], files: [], comments: [], following: false,
  } };
  const adoption = { schema: "catalog.sample.adopt", target: sampleObject.type, capability: "records", title: "Adopt advice", description: "Review a retained proposal.", payload: [{ name: "note", type: "string", description: "Review note" }, { name: "proposal", type: "string", description: "Original proposal" }], adoption: { function: { app: "catalog", name: "advice", version: 1 }, input: "proposal", mapping: [{ output: "summary", input: "note" }] } };
  const ui = (live: boolean) => <CatalogFixture decide={decide}><PortHost ports={{ can: () => true, source, catalog: [adoption] }}>
    <AIWidget config={{ kind: "analyst" }} functionBinding={{ ref: { app: "catalog", kind: "function", name: "advice" }, sourceVersion: "1.function-1" }} record={sampleRecords[0]} status="value" identity="original" active={() => true} live={live} />
    <InlineActionForm type={sampleObject.type} schema={adoption.schema} record={sampleRecords[0]} live={live} />
  </PortHost></CatalogFixture>;
  const view = render(ui(false));
  fireEvent.click(screen.getByRole("button", { name: "Request original analysis" }));
  fireEvent.click(screen.getByRole("button", { name: "Adopt advice" }));
  expect(spy.events).toEqual([]);
  view.rerender(ui(true));
  fireEvent.click(screen.getByRole("button", { name: "Request original analysis" }));
  expect(spy.events).toEqual(["decide:build.function-call.start"]);
  view.rerender(ui(false));
  await act(async () => { finish(true); await accepted; });
  const proposal = await screen.findByRole("region", { name: "Proposal review" });
  const adopt = within(proposal).getByRole("button", { name: "Adopt proposal" });
  expect((adopt as HTMLButtonElement).disabled).toBe(true); fireEvent.click(adopt);
  expect(spy.events).toEqual(["decide:build.function-call.start"]);
});

test("preview never invokes a code capability and a late live result does not start another call", async () => {
  const spy = makeSpy(), client: EdgeClient = new FixtureClient({});
  let finish!: (reply: { ok: boolean; status: number; body: Api.CapabilityResult }) => void;
  const reply = new Promise<{ ok: boolean; status: number; body: Api.CapabilityResult }>(resolve => { finish = resolve; });
  client.call = <T,>(method: string, path: string) => { spy.events.push(`${method}:${path}`); return reply as Promise<{ ok: boolean; status: number; body: T }>; };
  const ui = (live: boolean) => <CatalogFixture client={client}><ComputeCall binding={{ ref: { app: "catalog", kind: "compute", name: "sample" }, sourceVersion: "1.compute-1" }} recordType={sampleObject.type} live={live} /></CatalogFixture>;
  const view = render(ui(false)); expect(screen.queryByRole("button", { name: "Calculate" })).toBeNull(); expect(spy.events).toEqual([]);
  view.rerender(ui(true));
  await waitFor(() => expect((screen.getByRole("button", { name: "Calculate" }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole("button", { name: "Calculate" })); expect(spy.events).toEqual(["POST:/v1/capabilities/invoke"]);
  view.rerender(ui(false));
  await act(async () => { finish({ ok: true, status: 200, body: { ref: { app: "catalog", kind: "compute", name: "sample" }, state: "completed", call: "ORIGINAL" } }); });
  expect(screen.queryByRole("button", { name: "Calculate" })).toBeNull(); expect(spy.events).toEqual(["POST:/v1/capabilities/invoke"]);
});

test("an automatic compute resource cannot invoke after its delayed descriptor returns to preview", async () => {
  const spy = makeSpy(), client: EdgeClient = new FixtureClient({}), binding: Api.AssetBinding = { ref: { app: "catalog", kind: "compute", name: "sample" }, sourceVersion: "1.compute-1" };
  const computed: Api.Page = { ...samplePage.page, layout: "composed", sections: [{ id: "records", widget: "table", configVersion: 1 }, { id: "result", widget: "gauge", configVersion: 1, gaugeValueVariable: "computed", gauge: { max: 100 } }], document: {
    formatVersion: 2, uiProfile: pageUIManifest.uiProfile, root: "root", nodes: { root: { kind: "rows", children: ["records", "result"] }, records: { kind: "widget", section: "records" }, result: { kind: "widget", section: "result" } }, variables: {
      selected: { scope: "page", mode: "resource", type: "record", source: { kind: "record", section: "records" } }, computed: { scope: "page", mode: "resource", type: "number", source: { kind: "compute", compute: { operation: binding, recordVariable: "selected", inputs: { quantity: { source: "subject", path: ["quantity"] } } } } },
    },
  } };
  const slot = recordResourceSlot(computed, "selected")!, session = new PageSessionStore(sampleSource, { objects: new Map([[slot, sampleObject.type]]), children: new Map(), queryParents: new Map() });
  session.select(slot, sampleRecords[0]); await waitFor(() => expect(session.confirmedSelected(slot)).toBeTruthy());
  let finish!: (descriptor: Api.CapabilityDescriptor) => void;
  const descriptor = new Promise<Api.CapabilityDescriptor>(resolve => { finish = resolve; });
  client.get = <T,>(path: string) => { spy.events.push(`read:${path}`); return descriptor as Promise<T>; };
  client.call = async <T,>(method: string, path: string) => { spy.events.push(`${method}:${path}`); return { ok: false, status: 500, body: undefined as T }; };
  function ComputedOwner({ live }: { live: boolean }) { const values = usePageComputations(computed, session, session.snapshot(), { resources: {}, recordReferences: {} }, live); return <output>{values.computed?.status}</output>; }
  const ui = (live: boolean) => <CatalogFixture client={client}><ComputedOwner live={live} /></CatalogFixture>;
  const view = render(ui(false)); expect(spy.events).toEqual([]);
  view.rerender(ui(true)); await waitFor(() => expect(spy.events).toHaveLength(1));
  view.rerender(ui(false));
  await act(async () => { finish({ ref: binding.ref, version: binding.sourceVersion, revision: 1, kind: "compute", side: "server", title: "Original computation", description: "Pinned owner descriptor", group: "compute", icon: "", tone: "", source: "catalog-fixture", effects: [], ports: [] }); await descriptor; });
  expect(spy.events.every(event => event.startsWith("read:"))).toBe(true);
  expect(screen.getByText("empty")).toBeTruthy(); session.dispose();
});

test("a nested published application cannot make an authoring preview live", async () => {
  const spy = makeSpy();
  await renderedPage(
    <CatalogFixture client={new RecordingClient(spy.events)} decide={spy.decide} definitions={[...sampleDefinitions, appDefinition]}>
      <ApplicationSessionsProvider>
        <ApplicationPage pageRef={samplePage.ref} route={previewRoute} preview>
          <ApplicationPage pageRef={samplePage.ref} route={publishedRoute} preview={false}>
            <ComposedPage page={page} live />
          </ApplicationPage>
        </ApplicationPage>
      </ApplicationSessionsProvider>
    </CatalogFixture>);
  expect(await attemptCreate(spy)).toEqual([]);
  expect(screen.getByText("Actions do not run while you compose.")).toBeTruthy();
});
