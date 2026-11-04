import { acceptedClient, expect, signIn, test, unique } from "./kit";
import type { Page } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F12 command palette and Search share one grouped result contract and open path", async ({ page, browser }) => {
  test.setTimeout(180_000);
  const token = unique("needle");
  const memberID = `${token}-member`;
  const scopeProject = unique("f12scope"), scopeObject = unique("f12object");
  const scopeProjectTitle = `Scope project ${scopeProject}`, scopeObjectName = `${scopeObject}record`;
  const objectType = "knowledge.document";
  const objectID = `${token.toUpperCase()}-OBJ`;
  const knowledgeID = `${token.toUpperCase()}-KDOC`;
  const resourceEntries = Array.from({ length: 13 }, (_, index) => {
    const number = String(index + 1).padStart(2, "0");
    const resource = {
      id: `ri.${token}.dataset.${number}`,
      revision: 1,
      created: "2026-11-04T00:00:00Z",
      changed: "2026-11-04T00:00:00Z",
      ref: { app: "build", kind: "dataset", name: `${token}${number}` },
      type: "build.dataset",
      draft: `${token.toUpperCase()}-DS-${number}`,
      title: `${token} resource ${number}`,
      project: "f12-project",
      version: 1,
      state: "current",
      path: ["F12 project"],
      canOpen: index > 1,
      canEdit: false,
    };
    return { type: "build.dataset", id: resource.draft, title: resource.title, group: "resource", resource };
  });
  const hits = [
    ...resourceEntries,
    { type: objectType, id: objectID, title: `${token} object`, group: "object" },
  ];
  const passages = [{ document: `${objectType}/${knowledgeID}`, title: `${token} knowledge`, chunk: 0, text: `${token} passage`, score: 1 }];

  const mockSearch = async (target: Page) => {
    await target.route("**/v1/search?*", async route => {
      const query = new URL(route.request().url()).searchParams.get("q") ?? "";
      await route.fulfill({ status: 200, json: query.includes(token) ? hits : [] });
    });
    await target.route("**/v1/knowledge?*", async route => {
      const query = new URL(route.request().url()).searchParams.get("q") ?? "";
      await route.fulfill({ status: 200, json: query.includes(token) ? passages : [] });
    });
  };
  const openPalette = async (query: string) => {
    await page.keyboard.press("ControlOrMeta+K");
    const palette = page.getByRole("dialog", { name: "Command palette" });
    await expect(palette).toBeVisible();
    await palette.getByRole("combobox").fill(query);
    return palette;
  };
  const searchPageGroups = () => page.getByRole("main").locator("section[aria-label]").evaluateAll(
    nodes => nodes.map(node => node.getAttribute("aria-label") ?? ""),
  );
  const paletteGroups = (palette: ReturnType<Page["getByRole"]>) => palette.getByRole("group").evaluateAll(
    nodes => nodes.map(node => {
      const label = node.getAttribute("aria-labelledby");
      return node.getAttribute("aria-label") ?? (label ? document.getElementById(label)?.textContent?.trim() ?? "" : "");
    }),
  );
  const runSearchPage = async (query: string) => {
    const input = page.getByRole("textbox", { name: "Search", exact: true });
    await input.fill(query);
    await input.press("Enter");
  };

  await test.step("setup: an administrator can see members; a builder cannot", async () => {
    await signIn(page, "admin", "/members");
    await expect(page.getByRole("heading", { name: "Members and access", level: 1 })).toBeVisible();
    await acceptedClient(page, "admin").submit("platform.member", memberID, "invite", { subject: `user:${memberID}@example.test` }, 0);
    const allowed = await page.request.get("/v1/members", { headers: { authorization: "Bearer admin" } });
    expect(allowed.ok(), await allowed.text()).toBeTruthy();
    expect((await allowed.json() as { id: string }[]).some(member => member.id === memberID)).toBe(true);
    const denied = await page.request.get("/v1/members", { headers: { authorization: "Bearer builder" } });
    expect(denied.status()).toBe(403);
    const admin = acceptedClient(page, "admin");
    await admin.submit("build.app", scopeProject, "create", { name: scopeProject, title: scopeProjectTitle });
    await admin.submit("build.object", scopeObject, "create", { project: scopeProject, name: scopeObjectName, title: `Scope object ${scopeObject}`, fields: [{ name: "name", title: "Name", type: "text" }] });
    await expect.poll(async () => {
      const directory = await admin.read<{ resources: { draft?: string; ref: { kind: string } }[] }>("/v1/resources");
      return directory.resources.some(resource => resource.draft === scopeObject && resource.ref.kind === "object");
    }).toBe(true);
  });

  await mockSearch(page);

  await test.step("commands open the same URL from both entries", async () => {
    let palette = await openPalette("Inbox");
    let command = palette.getByRole("group", { name: "Commands" }).getByRole("option", { name: /^Inbox\b/ });
    await expect(command).toBeVisible();
    await command.click();
    await expect(page).toHaveURL(/#\/inbox$/);
    await page.goto("/#/search");
    await runSearchPage("Inbox");
    const commands = page.getByRole("region", { name: "Commands" });
    command = commands.getByRole("button", { name: /^Inbox\b/ });
    await expect(command).toBeVisible();
    await command.click();
    await expect(page).toHaveURL(/#\/inbox$/);
  });

  await test.step("applications open the same URL from both entries", async () => {
    let palette = await openPalette("Object Explorer");
    let application = palette.getByRole("group", { name: "Applications" }).getByRole("option", { name: /^Object Explorer/ });
    await expect(application).toBeVisible();
    await application.click();
    await expect(page).toHaveURL(/#\/explorer$/);
    await page.goto("/#/search");
    await runSearchPage("Object Explorer");
    const applications = page.getByRole("region", { name: "Applications" });
    application = applications.getByRole("button", { name: /^Object Explorer/ });
    await expect(application).toBeVisible();
    await application.click();
    await expect(page).toHaveURL(/#\/explorer$/);
  });

  await test.step("the current application keeps its navigation on the Search page", async () => {
    await page.goto("/#/members");
    await expect(page.getByRole("heading", { name: "Members and access", level: 1 })).toBeVisible();
    let palette = await openPalette("Roles and permissions");
    let current = palette.getByRole("group", { name: "Control Panel" }).getByRole("option", { name: /^Roles and permissions/ });
    await expect(current).toBeVisible();
    await current.click();
    await expect(page).toHaveURL(/#\/permissions$/);
    await page.goto("/#/search");
    await runSearchPage("Roles and permissions");
    const panel = page.getByRole("region", { name: "Control Panel" });
    current = panel.getByRole("button", { name: /^Roles and permissions/ });
    await expect(current).toBeVisible();
    await current.click();
    await expect(page).toHaveURL(/#\/permissions$/);
  });

  await test.step("resources, objects, members and knowledge use the same groups and truncation", async () => {
    await page.goto("/#/search");
    await runSearchPage(token);
    await expect(page.getByRole("region", { name: "Resources" })).toBeVisible();
    expect(await searchPageGroups()).toEqual(["Resources", "Objects", "Members", "Knowledge"]);
    const resources = page.getByRole("region", { name: "Resources" });
    const objects = page.getByRole("region", { name: "Objects" });
    expect(await resources.getByRole("button").count()).toBe(12);
    expect(await objects.getByRole("button").count()).toBe(1);
    await expect(resources.getByRole("button").first()).toBeDisabled();
    await expect(page.getByRole("region", { name: "Members" }).getByRole("button", { name: new RegExp(memberID) })).toBeVisible();
    await expect(page.getByRole("region", { name: "Knowledge" }).getByRole("button", { name: new RegExp(token) })).toBeVisible();

    const palette = await openPalette(token);
    await expect(palette.getByRole("group", { name: "Resources" })).toBeVisible();
    expect(await paletteGroups(palette)).toEqual(["Resources", "Objects", "Members", "Knowledge"]);
    expect(await palette.getByRole("group", { name: "Resources" }).getByRole("option").count()).toBe(12);
    expect(await palette.getByRole("group", { name: "Objects" }).getByRole("option").count()).toBe(1);
    await expect(palette.getByRole("group", { name: "Resources" }).getByRole("option").first()).toBeDisabled();
    await expect(palette.getByRole("group", { name: "Members" }).getByRole("option", { name: new RegExp(memberID) })).toBeVisible();
    await expect(palette.getByRole("group", { name: "Knowledge" }).getByRole("option", { name: new RegExp(token) })).toBeVisible();

    await palette.getByRole("group", { name: "Objects" }).getByRole("option", { name: new RegExp(token) }).click();
    const objectURL = page.url();
    expect(objectURL).toMatch(new RegExp(`/#/objects/${objectType.replaceAll(".", "\\.")}/${objectID}$`));
    await page.goto("/#/search");
    await runSearchPage(token);
    await page.getByRole("region", { name: "Objects" }).getByRole("button", { name: new RegExp(token) }).click();
    await expect(page).toHaveURL(objectURL);

    await page.goto("/#/search");
    await runSearchPage(token);
    await page.getByRole("region", { name: "Members" }).getByRole("button", { name: new RegExp(memberID) }).click();
    await expect(page).toHaveURL(new RegExp(`/#/member\\?id=${memberID}$`));
  });

  await test.step("@ reaches the member provider from both entries instead of returning empty", async () => {
    await page.goto("/#/search");
    await runSearchPage(`@${memberID}`);
    await expect(page.getByRole("region", { name: "Members" }).getByRole("button", { name: new RegExp(memberID) })).toBeVisible();
    expect(await searchPageGroups()).toEqual(["Members"]);

    const palette = await openPalette(`@${memberID}`);
    await expect(palette.getByRole("group", { name: "Members" }).getByRole("option", { name: new RegExp(memberID) })).toBeVisible();
    expect(await paletteGroups(palette)).toEqual(["Members"]);
    await page.keyboard.press("Escape");
  });

  await test.step("# and : narrow the shared provider to projects and object types", async () => {
    await page.goto("/#/search");
    await runSearchPage(`#${scopeProject}`);
    await expect(page.getByRole("region", { name: "Resources" }).getByRole("button", { name: new RegExp(scopeProjectTitle) })).toBeVisible();
    expect(await searchPageGroups()).toEqual(["Resources"]);
    await page.getByRole("region", { name: "Resources" }).getByRole("button", { name: new RegExp(scopeProjectTitle) }).click();
    await expect(page).toHaveURL(/#\/project\/ri\./);

    await page.goto("/#/search");
    await runSearchPage(`:${scopeObjectName}`);
    await expect(page.getByRole("region", { name: "Objects" }).getByRole("button", { name: new RegExp(scopeObject) })).toBeVisible();
    expect(await searchPageGroups()).toEqual(["Objects"]);
    await page.getByRole("region", { name: "Objects" }).getByRole("button", { name: new RegExp(scopeObject) }).click();
    await expect(page).toHaveURL(/#\/object-type\/ri\./);
  });

  await test.step("a builder cannot see member results", async () => {
    const memberPage = await (await browser.newContext()).newPage();
    await signIn(memberPage, "builder", "/search");
    await mockSearch(memberPage);
    await memberPage.keyboard.press("ControlOrMeta+K");
    const palette = memberPage.getByRole("dialog", { name: "Command palette" });
    await palette.getByRole("combobox").fill(token);
    await expect(palette.getByRole("group", { name: "Resources" })).toBeVisible();
    await expect(palette.getByRole("group", { name: "Members" })).toHaveCount(0);
    await expect(palette.getByRole("option", { name: new RegExp(memberID) })).toHaveCount(0);
    await memberPage.context().close();
  });
});
