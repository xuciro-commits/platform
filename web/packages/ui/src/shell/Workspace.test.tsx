import { StrictMode, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { DockviewApi, IDockviewReactProps } from "dockview-react";
import { toast } from "sonner";
import { Workspace, useUnsavedChanges, useWorkspace, type View } from "./Workspace";
import { InputDraftProvider, useDraftInput } from "../fields/draft";
import { routeKey, type Route } from "./route";
import { setLanguage } from "../i18n";

// The real Dockview model/renderer runs in jsdom. Only ResizeObserver and the
// OS-window call are stubbed; these are logic checks, not viewport/browser UX.
const observed = vi.hoisted(() => ({ dock: undefined as DockviewApi | undefined }));
vi.mock("dockview-react", async (load) => {
  const actual = await load<typeof import("dockview-react")>();
  return { ...actual, DockviewReact: (props: IDockviewReactProps) => <actual.DockviewReact {...props} onReady={(event) => {
    observed.dock = event.api; props.onReady?.(event);
  }} /> };
});

let api: ReturnType<typeof useWorkspace>;
const discard = vi.fn();
function Draft({ id }: { id: string }) {
  api = useWorkspace();
  const [value, setValue] = useState("saved");
  const raw = useDraftInput("timeout", "0");
  useUnsavedChanges(value !== "saved", () => { discard(id); setValue("saved"); });
  return <div>
    <input aria-label={`Draft ${id}`} value={value} onChange={(event) => setValue(event.target.value)} />
    <input aria-label={`Raw timeout ${id}`} value={raw.text} onChange={(event) => raw.write(event.target.value, "Invalid number")} />
  </div>;
}
function Entry({ title }: { title: string }) { api = useWorkspace(); return <h1>{title}</h1>; }
const views: View[] = [
  { id: "flow", title: (params) => `Flow ${params.id ?? "list"}`, render: (params) => <InputDraftProvider isolated scope={params.id}><Draft id={params.id ?? "list"} /></InputDraftProvider> },
  { id: "knowledge", title: () => "Documents", render: () => <Entry title="Documents" /> },
  { id: "home", title: () => "Home", render: () => <Entry title="Home" /> },
];
const scopeOf = (route: Route) => route.view === "flow" ? "build" : route.view === "knowledge" ? "knowledge" : undefined;
const apps = [
  { id: "build", title: "Builder", category: "build", home: { view: "flow", params: { id: "A" } } },
  { id: "knowledge", title: "Knowledge", category: "business", home: { view: "knowledge" } },
];
function Shell({ resolveRoute, onSelect = () => {} }: {
  resolveRoute?: (route: Route) => Route | Promise<Route>; onSelect?: (id: string) => void;
}) {
  const [current, setCurrent] = useState("build");
  return <Workspace product="Fixture" storageKey="workspace:test" layoutScope={current} scopeOf={scopeOf} resolveRoute={resolveRoute}
    views={views} home={{ view: "home" }} nav={[]}
    applications={{ apps, categories: [{ id: "build", label: "Build" }, { id: "business", label: "Business" }], current,
      onSelect: (id) => { onSelect(id); setCurrent(id); } }}
    onActiveRoute={(route) => { const owner = route && scopeOf(route); if (owner && owner !== current) setCurrent(owner); }} />;
}

beforeEach(() => {
  localStorage.clear(); history.replaceState(null, "", "/#/flow?id=A"); setLanguage("en"); discard.mockClear();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); toast.dismiss(); vi.restoreAllMocks(); vi.unstubAllGlobals(); observed.dock = undefined; });

const start = async (props: Parameters<typeof Shell>[0] = {}) => {
  render(<Shell {...props} />);
  return await screen.findByRole("textbox", { name: "Draft A" });
};
const choose = (id: string) => act(() => api.applications!.onSelect(id));

test("switching applications asks once for all local drafts; cancellation keeps raw input, selection and URL", async () => {
  const selected = vi.fn();
  const input = await start({ onSelect: selected });
  fireEvent.change(input, { target: { value: "keep A" } });
  act(() => api.open({ view: "flow", params: { id: "B" } }));
  fireEvent.change(await screen.findByLabelText("Raw timeout B"), { target: { value: "-" } });
  choose("knowledge");
  const dialog = await screen.findByRole("dialog", { name: "Unsaved changes" });
  expect(within(dialog).getByText("Flow A")).toBeTruthy();
  expect(within(dialog).getByText("Flow B")).toBeTruthy();
  expect(selected).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Keep editing" }));
  expect((screen.getByLabelText("Draft A") as HTMLInputElement).value).toBe("keep A");
  act(() => api.open({ view: "flow", params: { id: "B" } }));
  expect((screen.getByLabelText("Raw timeout B") as HTMLInputElement).value).toBe("-");
  expect(location.hash).toBe("#/flow?id=B");
  expect(api.applications!.current).toBe("build");
  expect(discard).not.toHaveBeenCalled();
});

test("confirmed app switching discards only those drafts and restores the old active entity on return", async () => {
  const selected = vi.fn();
  await start({ onSelect: selected });
  act(() => api.open({ view: "flow", params: { id: "B", surface: "studio" } }));
  fireEvent.change(await screen.findByLabelText("Draft B"), { target: { value: "discard B" } });
  choose("knowledge");
  fireEvent.click(await screen.findByRole("button", { name: "Discard changes" }));
  expect(await screen.findByRole("heading", { name: "Documents" })).toBeTruthy();
  expect(selected).toHaveBeenCalledWith("knowledge");
  expect(discard).toHaveBeenCalledWith("B");
  expect(observed.dock!.getPanel("flow?id=A")).toBeUndefined();
  choose("build");
  expect(await screen.findByRole("textbox", { name: "Draft B" })).toBeTruthy();
  expect((screen.getByLabelText("Draft B") as HTMLInputElement).value).toBe("saved");
  expect(location.hash).toBe("#/flow?id=B&surface=studio");
  expect(api.applications!.current).toBe("build");
});

test("a cross-application hash cannot tear down a dirty editor before confirmation", async () => {
  const input = await start();
  fireEvent.change(input, { target: { value: "keep through Back" } });
  act(() => { location.hash = "#/knowledge"; dispatchEvent(new HashChangeEvent("hashchange")); });
  fireEvent.click(await screen.findByRole("button", { name: "Keep editing" }));
  expect(location.hash).toBe("#/flow?id=A");
  expect((screen.getByLabelText("Draft A") as HTMLInputElement).value).toBe("keep through Back");
  expect(api.applications!.current).toBe("build");
  act(() => api.open({ view: "knowledge" }));
  fireEvent.click(await screen.findByRole("button", { name: "Discard changes" }));
  expect(await screen.findByRole("heading", { name: "Documents" })).toBeTruthy();
});

test("shared shell tabs stay out of app memory and never replace a new app's home", async () => {
  await start();
  act(() => api.open({ view: "home" }));
  choose("knowledge");
  expect(await screen.findByRole("heading", { name: "Documents" })).toBeTruthy();
  const build = JSON.parse(localStorage.getItem("workspace:test:build")!);
  const shell = JSON.parse(localStorage.getItem("workspace:test:shell")!);
  expect(build.panels.home).toBeUndefined();
  expect(shell.panels.home.params.route).toEqual({ view: "home" });
  expect(observed.dock!.getPanel("home")).toBeTruthy();
  choose("build");
  expect(await screen.findByRole("textbox", { name: "Draft A" })).toBeTruthy();
  expect(location.hash).toBe("#/flow?id=A");
});

test("opening an already-open view beside moves the same panel and retains local and raw drafts", async () => {
  await start();
  act(() => api.open({ view: "flow", params: { id: "B" } }));
  fireEvent.change(await screen.findByLabelText("Raw timeout B"), { target: { value: "-" } });
  const panel = observed.dock!.getPanel("flow?id=B")!;
  const input = screen.getByLabelText("Raw timeout B");
  const origin = panel.group;
  act(() => api.open({ view: "flow", params: { id: "A" } }));
  act(() => api.open({ view: "flow", params: { id: "B" } }, { window: "beside" }));
  expect(observed.dock!.getPanel("flow?id=B")).toBe(panel);
  expect(panel.group).not.toBe(origin);
  expect(observed.dock!.getPanel("flow?id=A")!.group).toBe(origin);
  expect(screen.getByLabelText("Raw timeout B")).toBe(input);
  expect((input as HTMLInputElement).value).toBe("-");
  expect(screen.queryByRole("dialog", { name: "Unsaved changes" })).toBeNull();
  expect(discard).not.toHaveBeenCalled();
});

test("a sole tab cannot create a collapsing pseudo-split", async () => {
  await start();
  const panel = observed.dock!.getPanel("flow?id=A")!, group = panel.group;
  act(() => api.open({ view: "flow", params: { id: "A" } }, { window: "beside" }));
  expect(panel.group).toBe(group);
  expect(observed.dock!.groups).toEqual([group]);
});

test("bulk close shares the same guard, clears stale URLs and offers an empty-workspace entry", async () => {
  const input = await start();
  fireEvent.change(input, { target: { value: "draft" } });
  act(() => api.closeAll());
  fireEvent.click(await screen.findByRole("button", { name: "Keep editing" }));
  expect((screen.getByLabelText("Draft A") as HTMLInputElement).value).toBe("draft");
  act(() => api.closeAll());
  fireEvent.click(await screen.findByRole("button", { name: "Discard changes" }));
  expect(await screen.findByText("No open tabs.")).toBeTruthy();
  expect(location.hash).toBe("#/");
  fireEvent.click(screen.getByRole("button", { name: "Open home" }));
  expect((await screen.findByRole("textbox", { name: "Draft A" }) as HTMLInputElement).value).toBe("saved");
});

test("a blocked popout does not discard a draft and reports the real window failure", async () => {
  const input = await start();
  fireEvent.change(input, { target: { value: "keep on move" } });
  const windowCall = vi.spyOn(observed.dock!, "addPopoutGroup").mockResolvedValue(false);
  act(() => api.open({ view: "flow", params: { id: "A" } }, { window: "popout" }));
  await waitFor(() => expect(windowCall).toHaveBeenCalledWith(observed.dock!.getPanel("flow?id=A"), expect.objectContaining({ onDidOpen: expect.any(Function) })));
  expect(await screen.findByText("Could not open a new window. Allow pop-ups and try again.")).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Unsaved changes" })).toBeNull();
  expect((screen.getByLabelText("Draft A") as HTMLInputElement).value).toBe("keep on move");
  expect(discard).not.toHaveBeenCalled();
});

test("a slow incoming alias cannot undo a newer explicit application choice", async () => {
  let finish!: (route: Route) => void;
  const pending = new Promise<Route>((resolve) => { finish = resolve; });
  await start({ resolveRoute: (route) => route.view === "old" ? pending : route });
  act(() => { location.hash = "#/old"; dispatchEvent(new HashChangeEvent("hashchange")); });
  choose("knowledge");
  expect(await screen.findByRole("heading", { name: "Documents" })).toBeTruthy();
  await act(async () => finish({ view: "flow", params: { id: "B" } }));
  expect(api.applications!.current).toBe("knowledge");
  expect(observed.dock!.getPanel("flow?id=B")).toBeUndefined();
  expect(location.hash).toBe("#/knowledge");
});

test("strict effect replay still opens the incoming entity", async () => {
  render(<StrictMode><Shell /></StrictMode>);
  expect(await screen.findByRole("textbox", { name: "Draft A" })).toBeTruthy();
  expect(observed.dock!.activePanel?.id).toBe(routeKey({ view: "flow", params: { id: "A" } }));
});
