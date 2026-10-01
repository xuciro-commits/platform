import { useState } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { Button, Dialog, Sheet, FlowLayout, Input, setLanguage } from "./index";

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

test("an unavailable overlay caller returns focus to the page fallback", async () => {
  const fallback = document.createElement("button"), caller = document.createElement("button");
  document.body.append(fallback, caller); caller.disabled = true;
  const { rerender } = render(<Dialog open onOpenChange={() => {}} title="Fallback" returnFocus={caller} fallbackFocus={fallback}><Input aria-label="Value" /></Dialog>);
  rerender(<Dialog open={false} onOpenChange={() => {}} title="Fallback" returnFocus={caller} fallbackFocus={fallback}><Input aria-label="Value" /></Dialog>);
  await waitFor(() => expect(document.activeElement).toBe(fallback));
  fallback.remove(); caller.remove();
});
