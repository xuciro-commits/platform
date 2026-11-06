import { readFile } from "node:fs/promises";
import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F22 a builder exports the visible lineage subgraph, impact list, fields, and readable metadata as CSV and JSON", async ({ page }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const project = unique("f22p"), projectTitle = `F22 ${project}`;
  const input = unique("f22in"), output = unique("f22out"), pipeline = unique("f22pipe"), object = unique("f22object");
  const inputTitle = "F22 input dataset", outputTitle = "F22 output dataset", pipelineTitle = "F22 rename pipeline";
  const objectName = `${object}record`, objectTitle = `F22 object ${object}`;
  const resource = (directory: { resources: { id: string; draft?: string; project?: string }[] }, draft: string) =>
    directory.resources.find(item => item.draft === draft);

  await test.step("seed a dataset, pipeline and object with a declared field rename", async () => {
    await submit("build.app", project, "create", { name: project, title: projectTitle }, 0);
    await submit("build.dataset", input, "create", { name: `${input}data`, title: inputTitle, key: "id", project }, 0);
    await submit("build.dataset", input, "load", { rows: [{ id: "A1", note: "from input" }] }, null);
    await submit("build.dataset", output, "create", { name: `${output}data`, title: outputTitle, marking: "restricted", key: "id", project }, 0);
    await submit("build.dataset", output, "load", { rows: [{ id: "A1", title: "from pipeline" }], marking: "restricted" }, null);
    await submit("build.pipeline", pipeline, "create", {
      name: `${pipeline}clean`, title: pipelineTitle, input, outputDataset: output, project,
      steps: [{ kind: "rename", from: "note", to: "title" }],
    }, 0);
    await submit("build.object", object, "create", {
      project,
      name: objectName, title: objectTitle, plural: "F22 objects", display: "note",
      fields: [{ name: "note", title: "Note", type: "text" }],
      datasources: [{ kind: "dataset", source: output, key: "id", columns: [{ column: "title", attribute: "note" }] }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => resource(await read<{ resources: { id: string; draft?: string }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toEqual(expect.stringMatching(/^ri\./));
  });

  const directory = await read<{ resources: { id: string; draft?: string; project: string }[] }>("/v1/resources");
  const objectRID = resource(directory, object)!.id;
  const graphPath = `/v1/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&depth=3`;
  const sourceGraph = await read<{
    nodes: { id: string; title: string; project?: string; guard?: string; markingStatus: string; datasetVersion?: number }[];
    edges: { from: string; to: string; role: string; consumedVersion?: number }[];
  }>(graphPath);
  const sourceOutput = sourceGraph.nodes.find(node => node.title === outputTitle);
  expect(sourceOutput).toBeTruthy();
  const projectRID = sourceOutput!.project;

  await signIn(page, "admin", `/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&depth=3`);
  await expect(page.getByText(outputTitle, { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Toggle structure panel" }).click();
  await expect(page.getByPlaceholder("Name or API name")).toBeVisible();

  await test.step("the current filter and isolation are reflected in a downloaded JSON subgraph", async () => {
    await page.getByPlaceholder("Name or API name").fill("F22 output");
    await expect(page.getByText(inputTitle, { exact: true })).toHaveCount(0);
    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export", exact: true }).click();
    await page.getByRole("menuitem", { name: "Visible subgraph · JSON", exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toMatch(/^lineage-subgraph-\d{14}\.json$/);
    const exported = JSON.parse(await readFile((await download.path())!, "utf8"));
    expect(exported.format).toBe("platform.lineage-subgraph");
    expect(exported.filters.search).toBe("F22 output");
    expect(exported.nodes).toHaveLength(1);
    expect(exported.edges).toHaveLength(0);
    expect(exported.nodes[0]).toMatchObject({
      id: sourceOutput!.id,
      title: outputTitle,
      kind: "dataset",
      project: projectRID,
      status: "current",
      guard: sourceOutput!.guard ?? undefined,
      markingStatus: sourceOutput!.markingStatus,
    });
    expect(exported.nodes[0].datasetVersion).toBe(sourceOutput!.datasetVersion ?? undefined);
  });

  await test.step("an isolated path exports node and edge rows as CSV without the hidden input", async () => {
    await page.goto(`/#/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&depth=3&selected=${encodeURIComponent(`pipeline:${pipeline}`)}`);
    await page.getByPlaceholder("Name or API name").fill("");
    await expect(page.getByText(inputTitle, { exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "Isolate", exact: true }).click();
    await expect(page.getByRole("button", { name: "Show all", exact: true })).toBeVisible();

    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export", exact: true }).click();
    await page.getByRole("menuitem", { name: "Visible subgraph · CSV", exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toMatch(/^lineage-subgraph-\d{14}\.csv$/);
    const exported = await readFile((await download.path())!, "utf8");
    expect(exported.charCodeAt(0)).toBe(0xfeff);
    const lines = exported.replace(/^\uFEFF/, "").trim().split(/\r?\n/);
    expect(lines[0]).toContain("recordType,id,resourceId,kind,name,title");
    expect(lines.filter(line => line.startsWith("node,"))).toHaveLength(3);
    expect(lines.filter(line => line.startsWith("edge,"))).toHaveLength(2);
    expect(exported).toContain(outputTitle);
    expect(exported).toContain(pipelineTitle);
    expect(exported).toContain("consumedVersion");
    expect(exported).not.toContain(inputTitle);
    await page.getByRole("button", { name: "Show all", exact: true }).click();
  });

  await test.step("the existing impact result downloads as readable JSON and CSV", async () => {
    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export", exact: true }).click();
    await page.getByRole("menuitem", { name: "Impact list · JSON", exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toMatch(/^lineage-impact-\d{14}\.json$/);
    const exported = JSON.parse(await readFile((await download.path())!, "utf8"));
    expect(exported.format).toBe("platform.lineage-impact");
    expect(exported.impact.upstream).toBeGreaterThanOrEqual(3);
    expect(exported.impact.items).toEqual(expect.arrayContaining([
      expect.objectContaining({ title: inputTitle, kind: "dataset", project: projectRID, status: "current" }),
      expect.objectContaining({ title: outputTitle, kind: "dataset", project: projectRID, status: "current" }),
      expect.objectContaining({ title: pipelineTitle, kind: "pipeline", project: projectRID, status: "never-run", distance: expect.any(Number) }),
    ]));
    expect(exported.impact.items.find((item: { title: string }) => item.title === outputTitle)).toMatchObject({
      guard: sourceOutput!.guard ?? undefined,
      markingStatus: sourceOutput!.markingStatus,
    });

    const downloadingCSV = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export", exact: true }).click();
    await page.getByRole("menuitem", { name: "Impact list · CSV", exact: true }).click();
    const csvDownload = await downloadingCSV;
    expect(csvDownload.suggestedFilename()).toMatch(/^lineage-impact-\d{14}\.csv$/);
    const csv = await readFile((await csvDownload.path())!, "utf8");
    expect(csv.replace(/^\uFEFF/, "")).toMatch(/^id,kind,title,project,status,distance,rid/);
    expect(csv).toContain(inputTitle);
    expect(csv).toContain(outputTitle);
  });

  await test.step("field mode exports the shown field nodes and edges", async () => {
    const fieldID = `dataset:${output}#title`;
    await page.goto(`/#/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}&fields=on&field=${encodeURIComponent(fieldID)}`);
    await expect(page.getByRole("button", { name: "Back to resources", exact: true })).toBeVisible();
    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export", exact: true }).click();
    await page.getByRole("menuitem", { name: "Visible subgraph · JSON", exact: true }).click();
    const download = await downloading;
    const exported = JSON.parse(await readFile((await download.path())!, "utf8"));
    expect(exported.filters.fieldMode).toBe(true);
    expect(exported.fields).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: fieldID, node: `dataset:${output}`, kind: "dataset", name: "title", project: projectRID }),
    ]));
    expect(exported.fieldEdges).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: fieldID, to: `object:build.${objectName}#note`, operation: "bind" }),
    ]));
    expect(exported.counts.fields).toBeGreaterThan(0);
    expect(exported.counts.fieldEdges).toBeGreaterThan(0);
  });
});
