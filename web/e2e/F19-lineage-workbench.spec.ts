import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F19 a builder opens Data Lineage from an object type, inspects upstream evidence, traces a field and measures downstream impact", async ({ page }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const project = unique("f19p"), projectTitle = `F19 ${project}`;
  const input = unique("f19in"), output = unique("f19out"), pipeline = unique("f19pipe"), object = unique("f19object");
  const objectName = `${object}record`, objectTitle = `F19 object ${object}`;
  const resource = (directory: { resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }, draft: string) =>
    directory.resources.find(item => item.draft === draft);

  await test.step("seed a dataset-to-object path with a declared field rename", async () => {
    await submit("build.app", project, "create", { name: project, title: projectTitle }, 0);
    await submit("build.dataset", input, "create", { name: `${input}data`, title: "Input dataset", key: "id", project }, 0);
    await submit("build.dataset", input, "load", { rows: [{ id: "A1", note: "from input" }] }, null);
    await submit("build.dataset", output, "create", { name: `${output}data`, title: "Output dataset", key: "id", project }, 0);
    await submit("build.dataset", output, "load", { rows: [{ id: "A1", title: "from pipeline" }] }, null);
    await submit("build.pipeline", pipeline, "create", {
      name: `${pipeline}clean`, title: "Rename pipeline", input, outputDataset: output, project,
      steps: [{ kind: "rename", from: "note", to: "title" }],
    }, 0);
    await submit("build.object", object, "create", {
      project,
      name: objectName, title: objectTitle, plural: "F19 objects", display: "note",
      fields: [{ name: "note", title: "Note", type: "text" }, { name: "manual", title: "Manual", type: "text" }],
      datasources: [{ kind: "dataset", source: output, key: "id", columns: [{ column: "title", attribute: "note" }] }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => resource(await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toEqual(expect.stringMatching(/^ri\./));
  });

  const directory = await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources");
  const objectRID = resource(directory, object)!.id;
  const outputRID = resource(directory, output)!.id;

  await test.step("the object type Usage opens the focused graph", async () => {
    await signIn(page, "admin", `/object-type?id=${object}`);
    await expect(page.getByRole("heading", { name: objectTitle, level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "Usage and lineage", exact: true }).click();
    await page.getByRole("button", { name: "Open in Data Lineage" }).click();
    await expect(page).toHaveURL(new RegExp(`focus=${encodeURIComponent(objectRID).replaceAll(".", "\\.")}`));
    await expect(page.getByText(objectTitle, { exact: true }).first()).toBeVisible();
  });

  await test.step("selecting the upstream dataset exposes versions, links and fields", async () => {
    await page.getByText("Output dataset", { exact: true }).first().click();
    await page.getByRole("button", { name: "Expand", exact: true }).click();
    await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    await expect(page.getByText("Inputs (1)", { exact: true })).toBeVisible();
    await expect(page.getByText("Outputs (1)", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Show fields", exact: true }).click();
    await page.getByRole("button", { name: "Toggle dock" }).click();
    await page.getByRole("tab", { name: "Fields", exact: true }).click();
    await expect(page.getByRole("button").filter({ hasText: "Title" }).first()).toBeVisible();
    await page.getByRole("button").filter({ hasText: "Title" }).first().click();
    await expect(page.getByText("title → Note · bind", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Back to resources", exact: true }).click();
  });

  await test.step("focusing the dataset and changing direction recalculates impact", async () => {
    await page.getByText("Output dataset", { exact: true }).first().click();
    if (!(await page.getByRole("region", { name: "Inspector" }).isVisible().catch(() => false))) {
      await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    }
    await page.getByRole("button", { name: "Focus here", exact: true }).click();
    await page.getByRole("button", { name: "Downstream", exact: true }).click();
    await page.getByRole("tab", { name: /Impact/ }).click();
    await expect(page.getByText(/Downstream:/)).toBeVisible();
  });

  await test.step("the server returns the same declared field chain and impact after truncation", async () => {
    const shallow = await read<{
      impact?: { upstream: number; downstream: number };
      nodes: { id: string }[];
      fieldNodes?: { id: string; certainty: string }[];
      fieldEdges?: { from: string; to: string; operation: string }[];
    }>(`/v1/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&depth=1&fields=on`);
    const deep = await read<{
      impact?: { upstream: number; downstream: number };
      nodes: { id: string }[];
      fieldNodes?: { id: string; certainty: string }[];
      fieldEdges?: { from: string; to: string; operation: string }[];
    }>(`/v1/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&depth=3&fields=on`);
    expect(shallow.impact?.upstream).toBe(deep.impact?.upstream);
    expect(shallow.impact?.downstream).toBe(deep.impact?.downstream);
    expect(deep.fieldNodes?.some(field => field.id === `object:build.${objectName}#note`)).toBeTruthy();
    expect(deep.fieldNodes?.find(field => field.id === `object:build.${objectName}#manual`)?.certainty).toBe("unverified");
    expect(deep.fieldEdges?.some(edge => edge.from === `dataset:${output}#title` && edge.to === `object:build.${objectName}#note` && edge.operation === "bind")).toBeTruthy();
    expect(outputRID).toMatch(/^ri\./);
  });
});
