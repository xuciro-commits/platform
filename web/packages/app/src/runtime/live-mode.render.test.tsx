import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { ApplicationPage, ApplicationSessionsProvider, ComposedPage, type Definition, type Host } from "../index";
import { CatalogFixture, FixtureClient, sampleDefinitions, samplePage } from "../catalog.fixtures";

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
  render(ui);
  await waitFor(() => expect(screen.getByText("1\u20132 of 2")).toBeTruthy(), { timeout: 10000 });
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
  ["standalone new page", standaloneRoute, sampleDefinitions],
  ["new page of an unpublished application", previewRoute, sampleDefinitions],
  ["unpublished page added to a published application", previewRoute, [...sampleDefinitions, appDefinition]],
  ["existing page of a published application", previewRoute, [...sampleDefinitions, appDefinition]],
])("authoring preview of a %s records no host or client write", async (_label, route, definitions) => {
  const spy = makeSpy();
  await renderedPage(
    <CatalogFixture client={new RecordingClient(spy.events)} decide={spy.decide} definitions={definitions}>
      <ApplicationSessionsProvider>
        <ApplicationPage pageRef={samplePage.ref} route={route} preview><ComposedPage page={page} /></ApplicationPage>
      </ApplicationSessionsProvider>
    </CatalogFixture>);
  const events = await attemptCreate(spy);
  expect(events).toEqual([]);
  // Authorized presentation and reads still work in preview...
  expect(screen.getByText("1–2 of 2")).toBeTruthy();
  // ...while the actions port is replaced by the preview boundary notice.
  expect(screen.getByText("Actions do not run while you compose.")).toBeTruthy();
});
