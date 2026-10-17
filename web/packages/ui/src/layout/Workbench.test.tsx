import { useState } from "react";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { Workbench } from "./Workbench";
import { setLanguage } from "../i18n";
import { ViewVisibilityContext } from "../shell/ViewVisibility";

beforeEach(() => {
  localStorage.clear();
  setLanguage("en");
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const dock = {
  label: "Flow dock",
  tabs: [
    { id: "problems", title: "Problems", content: <p>No problems</p> },
    { id: "test", title: "Test", content: <input aria-label="Synthetic test input" defaultValue="initial" /> },
  ],
};

test("selecting an active dock tab does not unexpectedly close its task", () => {
  render(<Workbench storageKey="flow" dock={dock}><p>Canvas</p></Workbench>);
  const tab = screen.getByRole("tab", { name: "Test" });
  fireEvent.click(tab);
  expect(screen.getByLabelText("Synthetic test input")).toBeTruthy();
  fireEvent.click(tab);
  expect(screen.getByLabelText("Synthetic test input")).toBeTruthy();
  expect(tab.getAttribute("aria-selected")).toBe("true");
});

test("dock close and tab changes preserve the isolated test draft", () => {
  render(<Workbench storageKey="flow" dock={dock}><p>Canvas</p></Workbench>);
  fireEvent.click(screen.getByRole("tab", { name: "Test" }));
  const input = screen.getByLabelText("Synthetic test input") as HTMLInputElement;
  fireEvent.change(input, { target: { value: "retained mock input" } });
  fireEvent.click(screen.getByRole("button", { name: "Toggle dock" }));
  expect(screen.queryByRole("textbox", { name: "Synthetic test input" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Toggle dock" }));
  expect((screen.getByLabelText("Synthetic test input") as HTMLInputElement).value).toBe("retained mock input");
  fireEvent.click(screen.getByRole("tab", { name: "Problems" }));
  fireEvent.click(screen.getByRole("tab", { name: "Test" }));
  expect((screen.getByLabelText("Synthetic test input") as HTMLInputElement).value).toBe("retained mock input");
});

test("inspector visibility preserves its active tab and local editing context", () => {
  render(<Workbench storageKey="page" right={{ label: "Inspector", tabs: [
    { id: "properties", title: "Properties", content: <p>Widget properties</p> },
    { id: "data", title: "Data", content: <input aria-label="Mock binding draft" defaultValue="initial" /> },
  ] }}><p>Canvas</p></Workbench>);
  const inspector = screen.getByRole("region", { name: "Inspector" });
  fireEvent.click(within(inspector).getByRole("tab", { name: "Data" }));
  fireEvent.change(screen.getByLabelText("Mock binding draft"), { target: { value: "same selected widget" } });
  fireEvent.click(screen.getByRole("button", { name: "Toggle inspector panel" }));
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Toggle inspector panel" }));
  expect(screen.getByRole("tab", { name: "Data" }).getAttribute("aria-selected")).toBe("true");
  expect((screen.getByLabelText("Mock binding draft") as HTMLInputElement).value).toBe("same selected widget");
});

test("inspector tabs support keyboard selection and identify their panel", () => {
  render(<Workbench storageKey="page" right={{ label: "Inspector", tabs: [
    { id: "properties", title: "Properties", content: <p>Widget properties</p> },
    { id: "data", title: "Data", content: <p>Widget bindings</p> },
  ] }}><p>Canvas</p></Workbench>);
  const first = screen.getByRole("tab", { name: "Properties" });
  first.focus();
  fireEvent.keyDown(first, { key: "ArrowRight" });
  const second = screen.getByRole("tab", { name: "Data" });
  expect(document.activeElement).toBe(second);
  expect(second.getAttribute("aria-selected")).toBe("true");
  const panel = screen.getByRole("tabpanel", { name: "Data" });
  expect(second.getAttribute("aria-controls")).toBe(panel.id);
  fireEvent.keyDown(second, { key: "Home" });
  expect(document.activeElement).toBe(first);
});


test("repeating a programmatic dock command reopens its already selected task", () => {
  function Example() {
    const [value, setValue] = useState("problems"), [open, setOpen] = useState<boolean>();
    return <Workbench storageKey="flow" actions={<button onClick={() => { setValue("test"); setOpen(true); }}>Review test</button>}
      dock={{ ...dock, value, onChange: setValue, open, onOpenChange: setOpen }}><p>Canvas</p></Workbench>;
  }
  render(<Example />);
  const review = screen.getByRole("button", { name: "Review test" });
  fireEvent.click(review);
  fireEvent.change(screen.getByLabelText("Synthetic test input"), { target: { value: "retained review" } });
  fireEvent.click(screen.getByRole("button", { name: "Close dock" }));
  expect(screen.queryByRole("textbox", { name: "Synthetic test input" })).toBeNull();
  fireEvent.click(review);
  expect((screen.getByLabelText("Synthetic test input") as HTMLInputElement).value).toBe("retained review");
});

test("closing the shared inspector frame keeps the selected item, tab and draft", () => {
  function Example() {
    const [open, setOpen] = useState<boolean>();
    return <Workbench storageKey="flow" actions={<button onClick={() => setOpen(true)}>Select the same block</button>}
      right={{ label: "Inspector", heading: "Selected block", open, onOpenChange: setOpen, tabs: [
        { id: "input", title: "Input", content: <p>Bindings</p> },
        { id: "settings", title: "Settings", content: <input aria-label="Mock timeout draft" defaultValue="0" /> },
      ] }}><p>Canvas</p></Workbench>;
  }
  render(<Example />);
  fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
  fireEvent.change(screen.getByLabelText("Mock timeout draft"), { target: { value: "-" } });
  fireEvent.click(screen.getByRole("button", { name: "Close inspector panel" }));
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Select the same block" }));
  expect(screen.getByRole("heading", { name: "Selected block" })).toBeTruthy();
  expect(screen.getByRole("tab", { name: "Settings" }).getAttribute("aria-selected")).toBe("true");
  expect((screen.getByLabelText("Mock timeout draft") as HTMLInputElement).value).toBe("-");
});

test("a structure command reveals and focuses its retained draft in a compact editor", async () => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(640);
  function Example() {
    const [open, setOpen] = useState<boolean>(), [preview, setPreview] = useState(true);
    const [selected, setSelected] = useState("page");
    return <Workbench storageKey="page" actions={<button onClick={() => { setPreview(false); setOpen(true); }}>Add widget</button>}
      left={{ label: "Library", open, onOpenChange: setOpen, content: <><input aria-label="Widget search" /><button onClick={() => setSelected("chart")}>Insert chart</button></> }}
      right={preview ? undefined : { label: "Inspector", scope: selected, content: <p>Page settings</p> }}
      dock={dock}><p>Canvas</p></Workbench>;
  }
  render(<Example />);
  fireEvent.click(screen.getByRole("button", { name: "Add widget" }));
  const library = screen.getByRole("region", { name: "Library" });
  await waitFor(() => expect(document.activeElement).toBe(library));
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull();
  fireEvent.change(screen.getByRole("textbox", { name: "Widget search" }), { target: { value: "chart" } });
  fireEvent.click(screen.getByRole("button", { name: "Insert chart" }));
  expect(screen.getByRole("region", { name: "Inspector" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Add widget" }));
  await waitFor(() => expect(document.activeElement).toBe(library));
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull();
  expect((screen.getByRole("textbox", { name: "Widget search" }) as HTMLInputElement).value).toBe("chart");
  fireEvent.click(screen.getByRole("button", { name: "Toggle dock" }));
  expect(screen.getByRole("region", { name: "Editor" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Add widget" }));
  expect(screen.getByRole("region", { name: "Library" })).toBeTruthy();
});

test("an initially open structure panel is shown in a compact editor", () => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(640);
  render(<Workbench storageKey="page" left={{ label: "Library", open: true, content: <p>Widgets</p> }}><p>Canvas</p></Workbench>);
  expect(screen.getByRole("region", { name: "Library" })).toBeTruthy();
});

test("a hidden workspace does not eagerly mount unvisited dock tasks", () => {
  let mounted = 0;
  function Task() { mounted++; return <p>Isolated task</p>; }
  const view = (visible: boolean) => <ViewVisibilityContext.Provider value={visible}><Workbench storageKey="flow"
    dock={{ label: "Dock", open: true, tabs: [{ id: "test", title: "Test", content: <Task /> }] }}><p>Canvas</p></Workbench></ViewVisibilityContext.Provider>;
  const { rerender } = render(view(false));
  expect(mounted).toBe(0);
  rerender(view(true));
  expect(screen.getByText("Isolated task")).toBeTruthy();
});

test("removed remembered dock tabs fall back to an available task", () => {
  localStorage.setItem("workbench:flow", JSON.stringify({ dockOpen: true, dockTab: "retired", leftOpen: "false", right: "invalid" }));
  render(<Workbench storageKey="flow" dock={dock}><p>Canvas</p></Workbench>);
  expect(screen.getByRole("tab", { name: "Problems" }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("tabpanel", { name: "Problems" }).textContent).toBe("No problems");
});

for (const lang of ["en", "zh-CN"]) test(`dock keyboard commands and close names remain usable in ${lang}`, () => {
  setLanguage(lang);
  render(<Workbench storageKey="flow" dock={dock}><p>Canvas</p></Workbench>);
  const tab = screen.getByRole("tab", { name: "Problems" });
  tab.focus(); fireEvent.keyDown(tab, { key: "ArrowRight" });
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Test" }));
  expect(screen.getByLabelText("Synthetic test input")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: lang === "en" ? "Close dock" : "关闭底栏" }));
  fireEvent.keyDown(screen.getByRole("tab", { name: "Test" }), { key: "j", ctrlKey: true });
  expect(screen.getByRole("textbox", { name: "Synthetic test input" })).toBeTruthy();
});
