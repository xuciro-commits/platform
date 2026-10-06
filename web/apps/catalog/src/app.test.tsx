import { expect, test } from "vitest";
import catalogApp, { catalogNavigation } from "./app";

test("Catalog has one primary entry per task, with no second page composer", () => {
  const routes = catalogNavigation().flatMap((section) => section.items.map((item) => item.route));
  expect(routes.map((route) => route.view)).toEqual(["catalog", "sandbox"]);
  const views = new Set(catalogApp.views.map((view) => view.id));
  for (const route of routes) expect(views.has(route.view)).toBe(true);
  expect(views.has("page-builder")).toBe(false);
});
