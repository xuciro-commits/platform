import { useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { MarkdownEditor } from "./Markdown";
import { markdown } from "../fields/types";

afterEach(cleanup);

test("Markdown editor tabs navigate by keyboard and preserve the controlled draft", () => {
  function Editor() {
    const [value, setValue] = useState<string | undefined>("# Original");
    return <MarkdownEditor value={value} onChange={setValue} />;
  }
  render(<Editor />);
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "# Edited draft" } });
  const write = screen.getByRole("tab", { name: "Write" });
  const preview = screen.getByRole("tab", { name: "Preview" });
  write.focus();
  fireEvent.keyDown(write, { key: "ArrowRight" });
  expect(document.activeElement).toBe(preview);
  expect(preview.getAttribute("aria-selected")).toBe("true");
  expect(write.tabIndex).toBe(-1);
  expect(screen.getByRole("heading", { name: "Edited draft" })).toBeTruthy();
  expect(screen.queryByRole("textbox")).toBeNull();
  const panel = screen.getByRole("tabpanel");
  expect(panel.id).toBe(preview.getAttribute("aria-controls"));
  expect(panel.getAttribute("aria-labelledby")).toBe(preview.id);
  fireEvent.keyDown(preview, { key: "Home" });
  expect(document.activeElement).toBe(write);
  expect(screen.getByDisplayValue("# Edited draft")).toBeTruthy();
  fireEvent.keyDown(write, { key: "End" });
  expect(document.activeElement).toBe(preview);
  fireEvent.keyDown(preview, { key: "ArrowLeft" });
  expect(document.activeElement).toBe(write);
});

test("Markdown field forwards help and error descriptions to its input", () => {
  const field = markdown({ label: "Notes" });
  render(<><label htmlFor="notes">Notes</label><p id="notes-help">Describe the change.</p><p id="notes-error">Notes are required.</p>
    {field.editor!({ id: "notes", value: "", onChange: vi.fn(), invalid: true, describedBy: "notes-help notes-error" })}</>);
  const input = screen.getByRole("textbox", { name: "Notes" });
  expect(input.getAttribute("aria-describedby")).toBe("notes-help notes-error");
  expect(input.getAttribute("aria-invalid")).toBe("true");
});
