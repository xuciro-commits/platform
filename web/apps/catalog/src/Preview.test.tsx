// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { CatalogPreview } from "./Preview";

vi.mock("./gen/previews", () => ({ previewLoaders: {
  broken: async () => ({ default: () => { throw new Error("Broken fixture"); } }),
  healthy: async () => ({ default: () => <p>Healthy example</p> }),
} }));

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

test("a broken example preserves navigation and other examples", async () => {
  vi.spyOn(console, "error").mockImplementation(() => undefined);
  const view = render(<><p>Asset navigation</p><CatalogPreview key="broken" id="broken" /></>);
  await screen.findByRole("alert");
  expect(screen.getByText("Asset navigation")).toBeTruthy();
  view.rerender(<><p>Asset navigation</p><CatalogPreview key="healthy" id="healthy" /></>);
  expect(await screen.findByText("Healthy example")).toBeTruthy();
  expect(screen.getByText("Asset navigation")).toBeTruthy();
});
