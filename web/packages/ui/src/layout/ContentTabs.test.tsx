import { fireEvent, render, screen, cleanup } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { useState } from "react";
import { ContentTabs } from "./ContentTabs";
afterEach(cleanup);
test("tabs mount lazily, retain input and navigate using stable identities", () => {
  function Example() {
    const [value, setValue] = useState("a");
    return <ContentTabs label="Panels" value={value} onChange={setValue} items={[
      { id: "a", title: "First", content: <input aria-label="Draft" /> },
      { id: "b", title: "Second", content: <p>Second content</p> },
    ]} />;
  }
  render(<Example />);
  expect(screen.queryByText("Second content")).toBeNull();
  fireEvent.change(screen.getByLabelText("Draft"), { target: { value: "Keep this input" } });
  fireEvent.keyDown(screen.getByRole("tab", { name: "First" }), { key: "ArrowRight" });
  expect(screen.getByRole("tab", { name: "Second" }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByText("Second content")).toBeTruthy();
  fireEvent.keyDown(screen.getByRole("tab", { name: "Second" }), { key: "Home" });
  expect((screen.getByLabelText("Draft") as HTMLInputElement).value).toBe("Keep this input");
});
