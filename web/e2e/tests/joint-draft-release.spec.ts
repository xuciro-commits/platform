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
      // The page names an object that only exists as a draft, and the
      // application holds that page: neither is published first.
      await decide(request, fixture.builder, "build", "build.page.create", { type: "build.page", id: pageID }, {
        name: `${name}page`, title: fixture.title, object: type, sections: [{ widget: "table", fields: ["note"] }],
      });
      await decide(request, fixture.builder, "build", "build.app.create", { type: "build.app", id: applicationID }, {
        name: `${name}app`, title: fixture.title, pages: [`${name}page`],
      });

      // The server names the drafts this application draft depends on.
      await open(page, fixture.builder, "/release-review");
      await page.getByRole("combobox", { name: "Definition kind" }).selectOption("app");
      await page.getByRole("combobox", { name: "Saved draft" }).selectOption(applicationID);
      await page.getByRole("button", { name: "Add the drafts it depends on" }).click();
      await expect(page.getByRole("status").filter({ hasText: "dependent drafts" })).toBeVisible();
      await page.getByRole("button", { name: "Check joint candidate" }).click();
      await expect(page.getByText("Joint candidate ready for review")).toBeVisible();
      await expect(page.getByRole("list", { name: "Draft provenance" })).toContainText(pageID);
      await page.getByRole("button", { name: "Save immutable candidate" }).click();
      await expect(page.getByText("Candidate saved; not active for operators.")).toBeVisible();
      await page.getByRole("button", { name: "Activate release" }).click();
      await expect(page.getByText("Release active for operators.")).toBeVisible();

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
      await page.getByRole("combobox", { name: "Definition kind" }).selectOption("page");
      await page.getByRole("combobox", { name: "Saved draft" }).selectOption(orphanPage);
      await page.getByRole("button", { name: "Check draft and dependencies" }).click();
      await expect(page.locator("[role='alert']").first()).toBeVisible();
      await expect(page.getByRole("button", { name: "Save immutable candidate" })).toHaveCount(0);
    });
  });
}
