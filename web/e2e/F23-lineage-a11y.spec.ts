import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000, viewport: { width: 1600, height: 1000 } });

test("F23 a builder completes the Data Lineage task from the overview table and resource list using only the keyboard", async ({ page }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const project = unique("f23p");
  const dataset = unique("f23in");
  const object = unique("f23object");
  const datasetTitle = "Accessible input dataset";
  const objectTitle = "Accessible lineage object";
  const objectName = `${object}record`;
  const resource = (directory: { resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }, draft: string) =>
    directory.resources.find(item => item.draft === draft);

  await test.step("seed an accessible dataset-to-object lineage", async () => {
    await submit("build.app", project, "create", { name: project, title: "F23 project" }, 0);
    await submit("build.dataset", dataset, "create", { name: `${dataset}data`, title: datasetTitle, key: "id", project }, 0);
    await submit("build.object", object, "create", {
      project,
      name: objectName, title: objectTitle, plural: "F23 objects", display: "note",
      fields: [{ name: "note", title: "Note", type: "text" }],
      datasources: [{ kind: "dataset", source: dataset, key: "id", columns: [{ column: "id", attribute: "note" }] }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => resource(await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toEqual(expect.stringMatching(/^ri\./));
  });

  const directory = await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources");
  const projectRID = resource(directory, project)!.id;

  await signIn(page, "admin", "/lineage");
  await expect(page.getByRole("heading", { name: "Data Lineage", level: 1 })).toBeVisible();
  const projectRow = page.getByRole("row").filter({ hasText: projectRID });
  await expect(projectRow).toBeVisible();
  await projectRow.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`project=${encodeURIComponent(projectRID).replaceAll(".", "\\.")}`));
  await expect(page.getByRole("region", { name: "Data lineage graph" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Filters", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Kind", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Status", exact: true })).toBeVisible();

  const inspectorToggle = page.getByRole("button", { name: "Toggle inspector panel", exact: true });
  if (await page.getByRole("region", { name: "Inspector", exact: true }).isVisible().catch(() => false)) {
    await inspectorToggle.focus();
    await page.keyboard.press("Enter");
  }
  await expect(page.getByRole("region", { name: "Inspector", exact: true })).toBeHidden();

  const search = page.getByRole("textbox", { name: "Find in graph", exact: true });
  await search.focus();
  await page.keyboard.type(datasetTitle);
  await page.keyboard.press("Enter");

  const resourceRow = page.getByRole("button", { name: new RegExp(datasetTitle) });
  await expect(resourceRow).toBeVisible();
  await expect(page.getByRole("list", { name: "Graph resources", exact: true })).toBeVisible();
  await expect(resourceRow).toBeFocused();
  await expect(resourceRow).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText(`Selected ${datasetTitle}`);

  await page.keyboard.press("Enter");
  await expect(page.getByRole("region", { name: "Inspector", exact: true })).toBeVisible();
  const details = page.getByRole("group", { name: "Selected resource details", exact: true });
  await expect(details).toBeFocused();
  await expect(details.getByRole("heading", { name: datasetTitle, exact: true })).toBeVisible();

  await page.keyboard.press("f");
  await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(`dataset:${dataset}`)}`));
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText(`Focused on ${datasetTitle}`);

  await page.keyboard.press("x");
  await expect(page.getByRole("combobox", { name: "Depth", exact: true })).toHaveValue("2");
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText("Expanded graph to depth 2");

  await search.focus();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.type(objectTitle);
  await page.keyboard.press("Enter");
  const objectRow = page.getByRole("button", { name: new RegExp(objectTitle) });
  await expect(objectRow).toBeFocused();
  await page.keyboard.press("i");
  await expect(page.getByText(/Isolated path · .* resources shown/)).toBeVisible();
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText(`Isolated path shown for ${objectTitle}`);

  await page.keyboard.press("Escape");
  await expect(page.getByText(/Isolated path · .* resources shown/)).toHaveCount(0);
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText("Isolation cleared.");

  await page.keyboard.press("Escape");
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText("Selection returned to the focus resource.");

  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(new RegExp(`project=${encodeURIComponent(projectRID).replaceAll(".", "\\.")}`));
  await expect(page).not.toHaveURL(/focus=/);
  await expect(page.getByRole("status", { name: "Lineage status", exact: true })).toContainText("Returned to the project graph.");

  const breadcrumb = page.getByRole("navigation", { name: "Breadcrumbs", exact: true }).getByRole("button", { name: "Data Lineage", exact: true });
  await breadcrumb.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Data Lineage", level: 1 })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: projectRID })).toBeVisible();
});
