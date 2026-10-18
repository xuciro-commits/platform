import { readFileSync } from "node:fs";
import type * as Api from "../../packages/kernel/src/gen/host";
import { expect, protocolVersion, shots, test } from "./kit";

test("a builder creates heating drafts from an installed package and an operator handles a real alarm", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(20_000);
  const gateway = "http://127.0.0.1:18510";
  const services = "http://127.0.0.1:18511";
  const driverVersion = protocolVersion();
  const configuration = JSON.parse(readFileSync(new URL("../../../deploy/examples/heating/harbin.json", import.meta.url), "utf8")) as Api.DriverConfiguration;
  await builder.open(page, "/apps?surface=tenant");
  for (const [id, version] of [["protocol.simulator", driverVersion], ["industry.heating", "0.1.0"]]) {
    const card = page.getByRole("region", { name: `Package ${id}`, exact: true });
    await card.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption(version!);
    await card.getByRole("button", { name: "Install", exact: true }).click();
    await expect(card.getByText("active", { exact: true })).toBeVisible();
  }
  const industry = page.getByRole("region", { name: "Package industry.heating", exact: true });
  const creation = page.waitForResponse(response => new URL(response.url()).pathname === "/v1/packages/industry.heating/drafts" && response.request().method() === "POST");
  await industry.getByRole("button", { name: "Create application drafts", exact: true }).click();
  const created = await creation;
  expect(created.ok()).toBe(true);
  const application = await created.json() as Api.PackageDraftResult;
  expect(application.application).toBe("heatingops");
  await expect.poll(() => page.url().includes(`id=${application.applicationID}`)).toBe(true);
  const connection = application.drafts.find(draft => draft.ref.kind === "connection" && draft.ref.name === "heatingfield")!;
  const propertyFeed = application.drafts.find(draft => draft.ref.kind === "connection" && draft.ref.name === "propertyfeed")!;
  const sample = application.drafts.find(draft => draft.ref.kind === "sample-set" && draft.ref.name === "heatingdemo")!;
  const kinds = await builder.read<{ items: Api.ConnectionKind[] }>("connection-kinds");
  const simulator = kinds.items.find(kind => kind.package === "protocol.simulator" && kind.profile?.id === "simulator" && kind.version === driverVersion)!;
  expect(simulator.digest).toMatch(/^(sha256:)?[0-9a-f]{64}$/);

  await builder.open(page, `/connection?id=${connection.id}`);
  await page.getByRole("combobox", { name: "Kind", exact: true }).selectOption(simulator.id);
  await page.getByRole("textbox", { name: "Gateway address", exact: true }).fill(gateway);
  await page.getByRole("textbox", { name: "Secret name", exact: true }).fill("heating-gateway");
  await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
  await page.getByRole("combobox", { name: "Simulation scenario", exact: true }).selectOption(configuration.scenario!);
  await page.getByRole("spinbutton", { name: "Simulation seed", exact: true }).fill(String(configuration.seed));
  await page.getByRole("textbox", { name: "Seeded numeric variation (default 0)", exact: true }).fill(String(configuration.connection?.variation));
  for (const device of configuration.devices) {
    await page.getByRole("button", { name: "Add device", exact: true }).click();
    await page.getByRole("textbox", { name: "Device ID", exact: true }).last().fill(device.id);
    await page.getByRole("textbox", { name: "Device title", exact: true }).last().fill(device.title);
  }
  await page.getByRole("button", { name: "Import a point table", exact: true }).click();
  await page.getByLabel("Point table file", { exact: true }).setInputFiles({ name: "heating-points.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(configuration.points)) });
  await page.getByRole("button", { name: "Replace points with this table", exact: true }).click();
  for (const command of configuration.commands ?? []) {
    await page.getByRole("button", { name: "Add command", exact: true }).click();
    await page.getByRole("textbox", { name: "Command ID", exact: true }).last().fill(command.id);
    await page.getByRole("combobox", { name: "Command point", exact: true }).last().selectOption(command.point);
    await page.getByRole("textbox", { name: "Minimum command value", exact: true }).last().fill(command.min ?? "");
    await page.getByRole("textbox", { name: "Maximum command value", exact: true }).last().fill(command.max ?? "");
  }
  await page.getByRole("button", { name: "Save connection", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save connection", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check", exact: true }).click();
  await expect.poll(async () => (await builder.record<Api.Connection>("build.connection", connection.id)).state, { timeout: 20_000 }).toBe("ready");

  await builder.open(page, `/connection?id=${propertyFeed.id}`);
  await page.getByRole("textbox", { name: "Address", exact: true }).fill(services);
  await page.getByRole("textbox", { name: "Secret name", exact: true }).fill("heating-property");
  await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
  await page.getByRole("button", { name: "Save connection", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save connection", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check", exact: true }).click();
  await expect.poll(async () => (await builder.record<Api.Connection>("build.connection", propertyFeed.id)).state, { timeout: 20_000 }).toBe("ready");
  await builder.decide("ai.provider.add", { type: "ai.provider", id: "heating" }, { kind: "local", baseUrl: `${services}/v1`, wire: "openai" }, "ai");
  await builder.decide("ai.model.enable", { type: "ai.model", id: "heating/health" }, { access: "users" }, "ai");

  await builder.open(page, `/project?id=${application.applicationID}&area=verify`);
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  const dependencyRead = page.waitForResponse(response => new URL(response.url()).pathname === "/v1/releases/drafts/referenced" && response.request().method() === "POST");
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  const dependencies = await dependencyRead;
  expect(dependencies.ok(), await dependencies.text()).toBe(true);
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Save immutable candidate", exact: true })).toBeVisible({ timeout: 10_000 });
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("combobox", { name: "Evaluation plan", exact: true }).selectOption("heatingadvicequality");
  await verify.getByRole("button", { name: "Run measured evaluation", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled({ timeout: 20_000 });
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // Synthetic demonstration records are explicit original create decisions.
  // The package and its isolated trial never populate the live business store.
  const samples = await builder.record<{ samples: Api.SampleRecords[] }>("build.sample-set", sample.id);
  const device = samples.samples.find(group => group.type === "build.device")!.records.find(row => row.id === "devicedemo")!;
  for (const group of samples.samples) for (const row of group.records) {
    const { id, state: _state, ...payload } = row;
    await builder.decide(`${group.type}.create`, { type: group.type, id: String(id) }, payload);
  }
  await operator.open(page, "/page?app=build&kind=page&name=heatingoverview&application=build:heatingops");
  await expect(page.getByRole("heading", { name: "Building overview", exact: true })).toBeVisible();
  const building = page.getByRole("row").filter({ hasText: "Example heating site" });
  await building.click();
  await expect(page.getByRole("row").filter({ hasText: String(device.code) })).toBeVisible();
  await shots(page, page.locator("body"), "heating-building-overview", info);

  await operator.decide("build.device.warn", { type: "build.device", id: "devicedemo" }, { reading: 70, summary: "High supply temperature" });
  let alarm: { id: string; summary: string; state: string } | undefined;
  await expect.poll(async () => {
    const result = await operator.read<{ records: { id: string; summary: string; state: string }[] }>("records/build.alarm?limit=100");
    alarm = result.records.find(record => record.summary === "High supply temperature");
    return alarm?.state;
  }).toBe("open");
  await operator.open(page, "/page?app=build&kind=page&name=heatingalarms&application=build:heatingops");
  await expect(page.getByRole("heading", { name: "Alarm handling", exact: true }).first()).toBeVisible();
  await page.getByRole("row").filter({ hasText: "High supply temperature" }).click();
  await page.getByRole("button", { name: "Acknowledge alarm", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: "Acknowledgement note *", exact: true }).fill("Property team is checking the heating circuit.");
  await form.getByRole("button", { name: "Acknowledge alarm", exact: true }).click();
  await expect(form).not.toBeVisible();
  await expect.poll(async () => (await operator.record("build.alarm", alarm!.id)).state).toBe("acknowledged");
  await shots(page, page.locator("body"), "heating-alarm-handling", info);
  await operator.open(page, "/page?app=build&kind=page&name=heatinghealth&application=build:heatingops");
  await page.getByRole("row").filter({ hasText: String(device.name) }).click();
  await expect(page.getByRole("heading", { name: "Health advice", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Request original analysis", exact: true }).click();
  await expect(page.getByText("summary: The accepted device evidence needs inspection.", { exact: true })).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("nextsteps: Check the latest observation and inspect the controller locally.", { exact: true })).toBeVisible();
  await shots(page, page.locator("body"), "heating-health-advice", info);
});
