import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F3 an integrator updates keyed rows without changing old versions and schedules a real pipeline build", async ({ page }) => {
  test.setTimeout(180_000);
  const { read, submit, publish } = acceptedClient(page);
  const name = unique("scheduled");
  const input = unique("INPUT"), output = unique("OUTPUT"), pipeline = unique("PIPELINE");
  let schedule = "";
  const version = async (id: string, number: number) => (await read<{ record: { data: { id: string; note: string }[] } }>(`/v1/records/build.datasetversion/${encodeURIComponent(`${id}@${number}`)}`)).record.data;
  const original = [{ id: "A", note: "original" }, { id: "B", note: "retained" }];

  await signIn(page, "admin", "/projects");
  await submit("build.dataset", input, "create", { name: `${name}input`, title: "Scheduled input", key: "id", keep: 5 });
  await submit("build.dataset", output, "create", { name: `${name}output`, title: "Scheduled output", keep: 5 });
  await submit("build.dataset", input, "load", { type: "snapshot", rows: original });

  await test.step("update and delete use the dataset key, while the earlier version stays readable", async () => {
    await page.goto(`/#/dataset?id=${input}`);
    await page.getByRole("combobox", { name: "Transaction type", exact: true }).selectOption("update");
    await page.locator('input[type="file"]').setInputFiles({ name: "updates.csv", mimeType: "text/csv", buffer: Buffer.from("id,note\nA,replaced\nC,inserted\n") });
    await expect.poll(async () => (await read<{ record: { version: number } }>(`/v1/records/build.dataset/${input}`)).record.version).toBe(2);
    expect(await version(input, 2)).toEqual([{ id: "A", note: "replaced" }, { id: "B", note: "retained" }, { id: "C", note: "inserted" }]);
    expect(await version(input, 1)).toEqual(original);

    await page.getByRole("combobox", { name: "Transaction type", exact: true }).selectOption("delete");
    await page.locator('input[type="file"]').setInputFiles({ name: "delete.csv", mimeType: "text/csv", buffer: Buffer.from("id\nB\n") });
    await expect.poll(async () => (await read<{ record: { version: number } }>(`/v1/records/build.dataset/${input}`)).record.version).toBe(3);
    expect(await version(input, 3)).toEqual([{ id: "A", note: "replaced" }, { id: "C", note: "inserted" }]);
    expect(await version(input, 1)).toEqual(original);
  });

  await submit("build.pipeline", pipeline, "create", { name: `${name}pipeline`, title: `Pipeline ${name}`, input, outputDataset: output });
  await submit("build.pipeline", pipeline, "publish", {});
  await submit("build.pipeline", pipeline, "run", {});
  await expect.poll(async () => (await read<{ record: { version?: number } }>(`/v1/records/build.dataset/${output}`)).record.version ?? 0, { timeout: 45_000 }).toBe(1);

  await test.step("a schedule is independently created and really builds its published pipeline", async () => {
    await page.goto("/#/schedule?id=new");
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(`${name}timer`);
    await page.getByRole("textbox", { name: "Title", exact: true }).fill(`Schedule ${name}`);
    await page.getByRole("combobox", { name: "Pipeline", exact: true }).selectOption(pipeline);
    await page.getByRole("combobox", { name: "Every", exact: true }).selectOption("15m");
    await page.getByRole("checkbox", { name: "Enabled", exact: true }).check();
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect.poll(async () => {
      const list = await read<{ records: { id: string; name: string }[] }>("/v1/records/build.schedule?limit=200");
      schedule = list.records.find(row => row.name === `${name}timer`)?.id ?? "";
      return schedule;
    }).not.toBe("");
    await expect.poll(async () => (await read<{ record: { version?: number } }>(`/v1/records/build.dataset/${output}`)).record.version ?? 0, { timeout: 45_000 }).toBe(2);
    expect(await version(output, 2)).toEqual(await version(input, 3));
    expect((await read<{ record: { lastRun?: string } }>(`/v1/records/build.schedule/${schedule}`)).record.lastRun).toEqual(expect.any(String));
    const built = await read<{ record: { last?: { written: number; error?: string } } }>(`/v1/records/build.pipeline/${pipeline}`);
    expect(built.record.last?.error ?? "").toBe("");
    expect(built.record.last?.written).toBe(2);
    await page.goto(`/#/schedule?id=${schedule}`);
    await page.getByRole("checkbox", { name: "Enabled", exact: true }).uncheck();
    await expect.poll(async () => (await read<{ record: { enabled: boolean } }>(`/v1/records/build.schedule/${schedule}`)).record.enabled).toBe(false);
    await publish([{ kind: "schedule", id: schedule }]);
    await expect(page.getByRole("combobox", { name: "Pipeline", exact: true })).toHaveValue(pipeline);
  });

  await submit("build.schedule", schedule, "archive", {});
  await submit("build.pipeline", pipeline, "pause", {});
  await submit("build.pipeline", pipeline, "archive", {});
  await submit("build.dataset", input, "archive", {});
  await submit("build.dataset", output, "archive", {});
});
