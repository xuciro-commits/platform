import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000, viewport: { width: 1600, height: 1000 } });

test("F24 a builder distinguishes focus from graph expansion, returns to the project, and folds node details", async ({ page }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const project = unique("f24p");
  const input = unique("f24in"), output = unique("f24out"), pipeline = unique("f24pipe"), object = unique("f24object");
  const inputTitle = "F24 input dataset", outputTitle = "F24 output dataset", pipelineTitle = "F24 rename pipeline", objectTitle = "F24 lineage object";
  const objectName = `${object}record`;
  const objectNodeID = `object:build.${objectName}`;
  const resource = (directory: { resources: { id: string; draft?: string; project: string }[] }, draft: string) =>
    directory.resources.find(item => item.draft === draft);

  await submit("build.app", project, "create", { name: project, title: `F24 ${project}` }, 0);
  await submit("build.dataset", input, "create", { name: `${input}data`, title: inputTitle, key: "id", project }, 0);
  await submit("build.dataset", output, "create", { name: `${output}data`, title: outputTitle, key: "id", project }, 0);
  await submit("build.pipeline", pipeline, "create", {
    name: `${pipeline}clean`, title: pipelineTitle, input, outputDataset: output, project,
    steps: [{ kind: "rename", from: "note", to: "title" }],
  }, 0);
  await submit("build.object", object, "create", {
    project,
    name: objectName, title: objectTitle, plural: "F24 objects", display: "note",
    fields: [{ name: "note", title: "Note", type: "text" }],
    datasources: [{ kind: "dataset", source: output, key: "id", columns: [{ column: "title", attribute: "note" }] }],
    states: [{ name: "open", title: "Open" }],
  }, 0);
  await expect.poll(async () => resource(await read<{ resources: { id: string; draft?: string; project: string }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toMatch(/^ri\./);

  const directory = await read<{ resources: { id: string; draft?: string; project: string }[] }>("/v1/resources");
  const projectRID = resource(directory, project)!.id;

  await signIn(page, "admin", "/lineage");
  await page.getByRole("row").filter({ hasText: projectRID }).click();
  await expect(page).toHaveURL(new RegExp(`project=${encodeURIComponent(projectRID).replaceAll(".", "\\.")}`));
  await expect(page.getByRole("region", { name: "Data lineage graph", exact: true })).toBeVisible();

  await page.getByText(objectTitle, { exact: true }).first().click();
  await page.getByRole("button", { name: "Focus", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(objectNodeID).replaceAll(".", "\\.")}`));
  await expect(page.getByText(pipelineTitle, { exact: true })).toHaveCount(0);

  await page.getByText(outputTitle, { exact: true }).first().click();
  await page.getByRole("button", { name: "Expand", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(objectNodeID).replaceAll(".", "\\.")}`));
  await expect(page).toHaveURL(/depth=2/);
  await expect(page.getByText(pipelineTitle, { exact: true }).first()).toBeVisible();

  await page.getByRole("button", { name: "Focus", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(`dataset:${output}`).replaceAll(".", "\\.")}`));

  await page.getByRole("button", { name: "Back to project", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`project=${encodeURIComponent(projectRID).replaceAll(".", "\\.")}`));
  await expect(page).not.toHaveURL(/focus=/);

  const outputNode = page.getByRole("region", { name: "Data lineage graph", exact: true }).getByLabel(new RegExp(`^${outputTitle} · Dataset`)).first();
  await expect(outputNode).toHaveAttribute("aria-expanded", "false");
  const collapsedBox = await outputNode.boundingBox();
  await outputNode.getByRole("button", { name: "Expand node details" }).click();
  await expect(outputNode).toHaveAttribute("aria-expanded", "true");
  await expect(outputNode.getByRole("group", { name: "Node details" })).toBeVisible();
  const expandedBox = await outputNode.boundingBox();
  const detailsBox = await outputNode.getByRole("group", { name: "Node details" }).boundingBox();
  expect(expandedBox!.height).toBeGreaterThan(collapsedBox!.height);
  expect(detailsBox!.y).toBeGreaterThanOrEqual(expandedBox!.y);
  expect(detailsBox!.y + detailsBox!.height).toBeLessThanOrEqual(expandedBox!.y + expandedBox!.height);
  await outputNode.getByRole("button", { name: "Collapse node details" }).click();
  await expect(outputNode.getByRole("group", { name: "Node details" })).toHaveCount(0);

  await page.getByRole("button", { name: "Expand all nodes", exact: true }).click();
  await expect(page.getByRole("group", { name: "Node details" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Collapse all nodes", exact: true }).click();
  await expect(page.getByRole("group", { name: "Node details" })).toHaveCount(0);

  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(`dataset:${output}`).replaceAll(".", "\\.")}`));
});
