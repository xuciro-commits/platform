import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

// M3 (ADR-0047 §10.2, ADR-0048 D1): a new object, the page over it and the
// application holding the page are delivered as one joint candidate, without
// publishing any of them first. Nothing is installed until the candidate is
// activated, and the review names the drafts it delivers.
for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", title: "Joint intake" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", title: "Joint inspection" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("deliver a new object, its page and its application as one candidate", async ({ page, request }) => {
      const name = fresh("joint").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
      const objectID = fresh("OBJ"), pageID = fresh("PAGE"), applicationID = fresh("APP");
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, plural: fixture.title,
        fields: [{ name: "note", title: "Note", type: "text" }],
      });
      await decide(request, fixture.builder, "build", "build.app.create", { type: "build.app", id: applicationID }, {
        name: `${name}app`, title: `${fixture.title} application`, pages: [],
      });

      // Construction starts with saved drafts, without a preliminary install.
      // Both the page and the qualified Object ref open their original editors.
      await open(page, fixture.builder, `/application?id=${applicationID}`);
      await expect(page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: new RegExp(`${fixture.title} application$`) })).toBeVisible();
      await page.getByRole("button", { name: "+ Create page", exact: true }).click();
      const create = page.getByRole("dialog", { name: "Create page", exact: true });
      await create.getByLabel(/^ID/).fill(pageID);
      await create.getByLabel(/^Name/).fill(`${name}page`);
      await create.getByLabel(/^What people call it/).fill(fixture.title);
      await create.getByLabel(/^Object it shows/).fill(type);
      await create.getByLabel(/^Fields in the list/).fill("note");
      await create.getByLabel(/^Fields in the list/).press("Enter");
      await create.getByLabel(/^Fields in the detail/).fill("note");
      await create.getByLabel(/^Fields in the detail/).press("Enter");
      await create.getByRole("button", { name: "Create", exact: true }).click();
      await expect(page.getByRole("button", { name: "Back to application", exact: true })).toBeVisible();
      await expect(page.getByText("Field choices come from a saved object draft. Business data is available after joint activation.", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Back to application", exact: true }).click();
      await expect(page.getByRole("checkbox", { name: `${fixture.title} · ${name}page · Saved draft`, exact: true })).toBeChecked();
      await page.getByRole("button", { name: "Choose existing resources", exact: true }).click();
      await page.getByRole("group", { name: "Available resources", exact: true }).getByRole("checkbox", { name: new RegExp(fixture.title) }).check();
      await page.getByRole("button", { name: "Save application", exact: true }).click();
      await expect(page.getByRole("button", { name: "Save application", exact: true })).toBeDisabled();
      await page.getByRole("button", { name: `Open ${fixture.title}`, exact: true }).last().click();
      await expect(page.getByRole("button", { name: "Back to application", exact: true })).toBeVisible();
      const [view, params] = new URL(page.url()).hash.split("?");
      expect(view).toBe("#/process");
      expect(new URLSearchParams(params).get("id")).toBe(objectID);
      expect(new URLSearchParams(params).get("application")).toBe(applicationID);
      await page.getByRole("button", { name: "Back to application", exact: true }).click();
      await page.getByRole("button", { name: "Review application release", exact: true }).click();
      await expect(page.getByRole("button", { name: `Add the drafts of ${fixture.title} application`, exact: true })).toBeVisible();

      // The server names the drafts this application draft depends on.
      await page.getByRole("button", { name: "Add the drafts it depends on" }).click();
      await expect(page.getByRole("status").filter({ hasText: "dependent drafts" })).toBeVisible();
      await page.getByRole("button", { name: "Check joint candidate" }).click();
      await expect(page.getByText("Joint candidate ready for review")).toBeVisible();
      await expect(page.getByRole("list", { name: "Draft provenance" })).toContainText(pageID);
      await page.getByRole("button", { name: "Save immutable candidate" }).click();
      await expect(page.getByText("Candidate saved; not active for operators.")).toBeVisible();
      await page.getByRole("button", { name: "Activate release" }).click();
      await expect(page.getByText("Release active for operators.")).toBeVisible();
      await page.getByRole("button", { name: "Back to application", exact: true }).click();
      await page.getByRole("button", { name: "Open business application", exact: true }).click();
      await expect(page.getByRole("heading", { name: fixture.title, exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Switch application", exact: true }).click();
      await expect(page.getByRole("menuitemradio", { name: new RegExp(`${fixture.title} application$`) })).toBeVisible();
      await page.keyboard.press("Escape");

      // Selecting only the dependent page is not the same delivery: while its
      // object is neither installed nor selected, the review refuses the page
      // and there is nothing to save.
      const orphan = fresh("ORPHAN");
      const orphanObject = fresh("OBJ"), orphanPage = fresh("PAGE");
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: orphanObject }, {
        name: orphan.replace(/[^a-z0-9]/gi, "").toLowerCase(), title: fixture.title,
        fields: [{ name: "note", title: "Note", type: "text" }],
      });
      await decide(request, fixture.builder, "build", "build.page.create", { type: "build.page", id: orphanPage }, {
        name: `${orphan}page`, title: fixture.title, object: `build.${orphan.replace(/[^a-z0-9]/gi, "").toLowerCase()}`,
        sections: [{ widget: "table", fields: ["note"] }],
      });
      await open(page, fixture.builder, `/release-review?application=${applicationID}`);
      await page.getByRole("combobox", { name: "Definition kind" }).selectOption("page");
      await page.getByRole("combobox", { name: "Saved draft" }).selectOption(orphanPage);
      await page.getByRole("button", { name: "Check draft and dependencies" }).click();
      await expect(page.locator("[role='alert']").first()).toBeVisible();
      await expect(page.getByRole("button", { name: "Save immutable candidate" })).toHaveCount(0);
    });
  });
}
