import { useState } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { ViewVisibilityContext } from "../shell/ViewVisibility";
import { Button, Dialog, Sheet, FlowLayout, Input, setLanguage } from "../index";

afterEach(cleanup);

for (const Frame of [Dialog, Sheet]) test(`${Frame.name} restores caller focus and unmounts local input`, async () => {
  setLanguage("en");
  function Example() {
    const [open, change] = useState(false);
    return <><Button onClick={() => change(true)}>Open task</Button><Frame open={open} onOpenChange={change} title="Task">{open && <Input aria-label="Draft note" defaultValue="" />}</Frame></>;
  }
  render(<Example />);
  const caller = screen.getByRole("button", { name: "Open task" }); caller.focus(); fireEvent.click(caller);
  fireEvent.change(await screen.findByLabelText("Draft note"), { target: { value: "unsaved" } });
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(caller));
  fireEvent.click(caller);
  expect((await screen.findByLabelText("Draft note") as HTMLInputElement).value).toBe("");
});

test("toolbar arrows skip disabled controls and stay inside their toolbar", () => {
  render(<><FlowLayout toolbar label="Commands"><Button>A</Button><Button disabled>B</Button><Button>C</Button></FlowLayout><Button>Outside</Button></>);
  const a = screen.getByRole("button", { name: "A" }), c = screen.getByRole("button", { name: "C" });
  a.focus(); fireEvent.keyDown(a, { key: "ArrowRight" }); expect(document.activeElement).toBe(c);
  fireEvent.keyDown(c, { key: "ArrowRight" }); expect(document.activeElement).toBe(a);
  fireEvent.keyDown(a, { key: "End" }); expect(document.activeElement).toBe(c);
});

test("toolbar leaves modified arrow shortcuts to its editor", () => {
  const shortcuts: string[] = [];
  render(<div onKeyDown={event => { if (!event.defaultPrevented) shortcuts.push(event.key); }}>
    <FlowLayout toolbar label="Commands"><Button>A</Button><Button>C</Button></FlowLayout>
  </div>);
  const a = screen.getByRole("button", { name: "A" }); a.focus();
  fireEvent.keyDown(a, { key: "ArrowDown", altKey: true });
  fireEvent.keyDown(a, { key: "ArrowRight", altKey: true });
  expect(shortcuts).toEqual(["ArrowDown", "ArrowRight"]);
  expect(document.activeElement).toBe(a);
});

test("an unavailable overlay caller returns focus to the page fallback", async () => {
  const fallback = document.createElement("button"), caller = document.createElement("button");
  document.body.append(fallback, caller); caller.disabled = true;
  const { rerender } = render(<Dialog open onOpenChange={() => {}} title="Fallback" returnFocus={caller} fallbackFocus={fallback}><Input aria-label="Value" /></Dialog>);
  rerender(<Dialog open={false} onOpenChange={() => {}} title="Fallback" returnFocus={caller} fallbackFocus={fallback}><Input aria-label="Value" /></Dialog>);
  await waitFor(() => expect(document.activeElement).toBe(fallback));
  fallback.remove(); caller.remove();
});

for (const Frame of [Dialog, Sheet]) test(`${Frame.name} suspends its global layer without resetting content and resets on a real close`, async () => {
  const { rerender } = render(<Frame open title="Paused task" onOpenChange={() => {}}><Input aria-label="Paused draft" defaultValue="initial" /></Frame>);
  const input = await screen.findByLabelText("Paused draft");
  fireEvent.change(input, { target: { value: "keep" } });
  rerender(<Frame open suspended title="Paused task" onOpenChange={() => {}}><Input aria-label="Paused draft" defaultValue="initial" /></Frame>);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(input.isConnected).toBe(false);
  expect(document.body.style.pointerEvents).not.toBe("none");
  rerender(<Frame open title="Paused task" onOpenChange={() => {}}><Input aria-label="Paused draft" defaultValue="initial" /></Frame>);
  expect(await screen.findByLabelText("Paused draft")).toBe(input);
  expect((input as HTMLInputElement).value).toBe("keep");
  rerender(<Frame open={false} title="Paused task" onOpenChange={() => {}}><Input aria-label="Paused draft" defaultValue="initial" /></Frame>);
  rerender(<Frame open title="Paused task" onOpenChange={() => {}}><Input aria-label="Paused draft" defaultValue="initial" /></Frame>);
  expect((await screen.findByLabelText("Paused draft") as HTMLInputElement).value).toBe("initial");
});


for (const Frame of [Dialog, Sheet]) test(`${Frame.name} releases global layers automatically when its owning pane is hidden`, async () => {
  const view = (visible: boolean) => <ViewVisibilityContext.Provider value={visible}><Frame open title="Retained pane task" onOpenChange={() => {}}><Input aria-label="Retained pane input" defaultValue="initial" /></Frame></ViewVisibilityContext.Provider>;
  const { rerender } = render(view(true));
  fireEvent.change(await screen.findByLabelText("Retained pane input"), { target: { value: "mock draft" } });
  rerender(view(false));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(document.body.style.pointerEvents).not.toBe("none");
  rerender(view(true));
  expect((await screen.findByLabelText("Retained pane input") as HTMLInputElement).value).toBe("mock draft");
});

test("a call from inside an overlay opens beside it, never a stacked modal", async () => {
  setLanguage("en");
  function Example() {
    const [outer, changeOuter] = useState(false), [inner, changeInner] = useState(false);
    return <>
      <Button onClick={() => changeOuter(true)}>Open task</Button>
      <Dialog open={outer} onOpenChange={changeOuter} title="Outer task">
        {outer && <Button onClick={() => changeInner(true)}>Open one more field</Button>}
        {inner && <Sheet open={inner} onOpenChange={changeInner} title="One more field"><p>Side content</p></Sheet>}
      </Dialog>
    </>;
  }
  render(<Example />);
  fireEvent.click(screen.getByRole("button", { name: "Open task" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open one more field" }));
  // The open sheet is the one live dialog; Radix hides the covered modal
  // from the accessibility tree, so the covered frame needs hidden: true.
  const frames = await screen.findAllByRole("dialog", { hidden: true });
  expect(frames).toHaveLength(2);
  expect(frames.filter(frame => frame.getAttribute("data-frame") === "modal")).toHaveLength(1);
  expect(frames.filter(frame => frame.getAttribute("data-frame") === "sheet")).toHaveLength(1);
  await screen.findByText("Side content");
});
