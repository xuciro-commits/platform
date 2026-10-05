import { expect, test } from "@playwright/test";
import { decide, fresh, open, pageUIProfile } from "./host";

test("application declarations bind two released pages and isolate and close explicit instances", async ({ page, request }, testInfo) => {
  test.setTimeout(60_000);
  const name = fresh("shared").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`, objectID = fresh("OBJ"), appID = fresh("APP"), firstID = fresh("PAGE"), secondID = fresh("PAGE");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, { name, title: "Shared notes", fields: [{ name: "note", title: "Note", type: "text" }] });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  const document = (target: string, shared = false) => ({ formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["input", "text", "next"] }, input: { kind: "widget", section: "input", valueVariable: "draft" }, text: { kind: "widget", section: "text", visibleWhen: "ready" }, next: { kind: "widget", section: "next" } }, variables: shared ? { draft: { title: "Page draft", scope: "application", type: "string", mode: "shared", writable: true, source: { kind: "application", variable: "draft" } }, ready: { title: "Page condition", scope: "application", type: "boolean", mode: "shared", source: { kind: "application", variable: "ready" } } } : { draft: { title: "Page draft", scope: "page", type: "string", mode: "state", initial: "" }, ready: { title: "Page condition", scope: "page", type: "boolean", mode: "constant", initial: false } }, events: [{ source: "next", event: "click", target: "", navigate: { page: { app: "build", kind: "page", name: target }, interfaceVersion: 0 } }] });
  // Bootstrap ordinary pages before declaring application membership.
  for (const [id, suffix, title] of [[firstID, "first", "Shared first"], [secondID, "second", "Shared second"]]) {
    await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, { name: `${name}${suffix}`, title, object: type, sections: [{ id: "input", widget: "input", configVersion: 1, title: "Shared text" }, { id: "text", widget: "text", configVersion: 1, text: "Shared condition active" }, { id: "next", widget: "button", configVersion: 1, title: "Next shared page" }], document: document(`${name}${suffix}`) });
    await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id }, {});
  }
  const declarations = (initial: string) => ({ draft: { title: "Shared draft", scope: "application", type: "string", mode: "state", initial }, ready: { title: "Shared condition", scope: "application", type: "boolean", mode: "derived", expression: { op: "equal", args: [{ variable: "draft" }, { literal: "ready" }] } } });
  await decide(request, "manager", "build", "build.app.create", { type: "build.app", id: appID }, { name: `${name}app`, title: "Shared desk", pages: [`${name}first`, `${name}second`], uiProfile: pageUIProfile, variables: declarations("initial") });
  await decide(request, "manager", "build", "build.app.publish", { type: "build.app", id: appID }, {});
  await open(page, "manager", `/application?id=${appID}`);
  // A page opens in the editor that maintains it, with the application kept in context (ADR-0047 §6.2).
  await page.getByRole("button", { name: "Open Shared first", exact: true }).click();
  await expect(page.getByRole("button", { name: "Back to application", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back to application", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save application", exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Choose application variable", exact: true }).selectOption("draft");
  await page.getByLabel("Initial value", { exact: true }).fill("start");
  await page.getByRole("button", { name: "Save application", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save application", exact: true })).toBeDisabled();
  await decide(request, "manager", "build", "build.app.publish", { type: "build.app", id: appID }, {});
  await open(page, "manager", `/compose?id=${firstID}`);
  const tree = page.getByRole("region", { name: "Widgets and layout", exact: true }), inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  await tree.getByRole("button", { name: "Page variables", exact: true }).click();
  for (const id of ["draft", "ready"]) {
    await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).selectOption(id);
    await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("shared");
    await inspector.getByRole("combobox", { name: "Application variable", exact: true }).selectOption(id);
  }
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
  await decide(request, "manager", "build", "build.page.edit", { type: "build.page", id: secondID }, { document: document(`${name}first`, true) });
  await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id: secondID }, {});
  // The first page navigates to the second, through the same application context.
  await decide(request, "manager", "build", "build.page.edit", { type: "build.page", id: firstID }, { document: document(`${name}second`, true) });
  await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id: firstID }, {});
  await open(page, "manager", `/application?id=${appID}`);
  await page.getByRole("button", { name: "Review application release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  // A later mutable application draft must not alter this candidate's initial values.
  await decide(request, "manager", "build", "build.app.edit", { type: "build.app", id: appID }, { variables: declarations("later draft") });
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const operation = await page.context().newPage(), application = `build:${name}app`, route = `/page?app=build&kind=page&name=${name}first&application=${application}&instance=main`;
  await open(operation, "desk", route);
  const text = operation.getByRole("textbox", { name: "Shared text", exact: true });
  await expect(text).toHaveValue("start"); await text.fill("ready");
  await expect(operation.getByText("Shared condition active", { exact: true })).toBeVisible();
  await operation.getByRole("button", { name: "Next shared page", exact: true }).click();
  await expect(operation.getByRole("heading", { name: "Shared second", exact: true })).toBeVisible();
  await expect(text).toHaveValue("ready");
  expect(new URL(operation.url()).hash).toContain("instance=main");
  if (process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({ path: testInfo.outputPath("application-shared-state.png"), fullPage: true });
  await operation.getByRole("button", { name: "New application instance", exact: true }).click();
  await expect(text).toHaveValue("start"); await expect(operation.getByText("Shared condition active", { exact: true }).filter({visible:true})).toHaveCount(0);
  const independent = new URL(operation.url()).hash.slice(1);
  await text.fill("other instance");
  // Application navigation retains the independent instance.
  await operation.getByRole("button", { name: "Shared first", exact: true }).click();
  await expect(text).toHaveValue("other instance");
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(text).toHaveValue("ready");
  await operation.getByRole("button", { name: "Close application instance", exact: true }).click();
  await operation.evaluate((route) => { location.hash = route; }, independent);
  await expect(text).toHaveValue("other instance");
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(text).toHaveValue("start");
  if (process.env.PLATFORM_SCREENSHOTS) { await operation.setViewportSize({ width: 390, height: 844 }); await operation.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))); await operation.screenshot({ path: testInfo.outputPath("application-instance-narrow.png"), fullPage: true }); }
  await operation.reload(); await expect(text).toHaveValue("start");
  await text.fill("member private");
  await operation.getByRole("button", { name: /desk-1/ }).click();
  await operation.getByRole("menuitemradio", { name: /manager-1/ }).click();
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(text).toHaveValue("start");
  await text.fill("before version");
  await operation.evaluate((route) => { location.hash = route; }, `/compose?id=${firstID}`);
  const preview = operation.getByRole("region", { name: "The page", exact: true }).getByRole("textbox", { name: "Shared text", exact: true });
  await expect(preview).toHaveValue("start"); await preview.fill("preview private");
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(text).toHaveValue("before version");
  await operation.evaluate((route) => { location.hash = route; }, `/application?id=${appID}`);
  await operation.getByRole("combobox", { name: "Choose application variable", exact: true }).selectOption("draft");
  await operation.getByLabel("Initial value", { exact: true }).fill("new version");
  await operation.getByRole("button", { name: "Save application", exact: true }).click();
  await expect(operation.getByRole("button", { name: "Save application", exact: true })).toBeDisabled();
  await operation.getByRole("button", { name: "Review application release", exact: true }).click();
  await operation.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await operation.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await operation.getByRole("button", { name: "Activate release", exact: true }).click();
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(text).toHaveValue("new version");
});
