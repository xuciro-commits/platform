import { readFileSync } from "node:fs";
import type * as Api from "../../packages/kernel/src/gen/host";
import { expect, shots, slug, test } from "./kit";

test("an installed protocol binary configures devices, previews points and publishes a connection", async ({ page, builder }, info) => {
  // The host's package directory contains binaries built by make packages.
  // Installation and every read below use the real package owner and gateway.
  const packageID = "protocol.simulator", version = "0.1.3";
  const gateway = process.env.PROTOCOL_GATEWAY_URL ?? "http://127.0.0.1:18510";
  await builder.open(page, "/apps?surface=tenant");
  const card = page.getByRole("region", { name: `Package ${packageID}`, exact: true });
  await card.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption(version);
  await card.getByRole("button", { name: "Install", exact: true }).click();
  await expect(card.getByText("active", { exact: true })).toBeVisible();

  const kinds = await builder.read<{ items: Api.ConnectionKind[] }>("connection-kinds");
  const simulator = kinds.items.find(kind => kind.package === packageID && kind.version === version && kind.profile?.id === "simulator");
  expect(simulator?.profile?.simulation).toBe(true);
  expect(simulator?.digest).toMatch(/^(sha256:)?[0-9a-f]{64}$/);
  const name = slug("heating");
  await builder.open(page, "/connection?id=new");
  await page.getByRole("textbox", { name: "Connection name", exact: true }).fill(name);
  await page.getByRole("textbox", { name: "Connection title", exact: true }).fill("Simulated heating station");
  await page.getByRole("combobox", { name: "Kind", exact: true }).selectOption(simulator!.id);
  await page.getByRole("textbox", { name: "Gateway address", exact: true }).fill(gateway);
  await page.getByRole("textbox", { name: "Secret name", exact: true }).fill("protocol-gateway");
  await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
  await page.getByRole("combobox", { name: "Simulation scenario", exact: true }).selectOption("normal");
  await page.getByRole("button", { name: "Add device", exact: true }).click();
  await page.getByRole("textbox", { name: "Device ID", exact: true }).fill("station-01");
  await page.getByRole("textbox", { name: "Device title", exact: true }).fill("Station 01");

  await page.getByRole("button", { name: "Import a point table", exact: true }).click();
  const pointFile = page.getByLabel("Point table file", { exact: true });
  await pointFile.setInputFiles({ name: "invalid-points.csv", mimeType: "text/csv", buffer: Buffer.from("id,device,address,type\nsupply-temperature,station-01,,decimal\n") });
  await expect(page.getByText("address is required.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Replace points with this table", exact: true })).toBeDisabled();
  const points = [
    { id: "supply-temperature", device: "station-01", address: "58.4", type: "decimal", unit: "°C", intervalMS: 1000 },
    { id: "valve-position", device: "station-01", address: "40", type: "decimal", unit: "%", intervalMS: 1000 },
  ];
  await pointFile.setInputFiles({ name: "heating-points.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(points)) });
  await page.getByRole("button", { name: "Replace points with this table", exact: true }).click();
  await page.getByRole("button", { name: "Add command", exact: true }).click();
  await page.getByRole("textbox", { name: "Command ID", exact: true }).fill("set-valve");
  await page.getByRole("combobox", { name: "Command point", exact: true }).selectOption("valve-position");
  await page.getByRole("textbox", { name: "Minimum command value", exact: true }).fill("0");
  await page.getByRole("textbox", { name: "Maximum command value", exact: true }).fill("100");
  await page.getByRole("button", { name: "Read-only preview", exact: true }).click();
  const preview = page.getByRole("region", { name: "Read-only preview", exact: true });
  await expect(preview.getByText("Read confirmed", { exact: true })).toBeVisible();
  await expect(preview.getByRole("cell", { name: "supply-temperature", exact: true })).toBeVisible();
  await expect(preview.getByRole("cell", { name: "58.4", exact: true })).toBeVisible();
  await expect(preview.getByRole("columnheader", { name: "Sample time", exact: true })).toBeVisible();
  await expect(preview.getByRole("cell", { name: "good", exact: true }).first()).toBeVisible();
  await expect(preview.getByText("Simulation", { exact: true })).toBeVisible();
  await shots(page, preview, "protocol-connection-preview", info);

  await page.getByRole("button", { name: "Save connection", exact: true }).click();
  let saved: Api.Connection | undefined;
  await expect.poll(async () => {
    const result = await builder.read<{ records: Api.Connection[] }>("records/build.connection?limit=100");
    saved = result.records.find(connection => connection.name === name);
    return saved?.protocol?.configuration.points.length;
  }).toBe(2);
  expect(saved!.protocol).toMatchObject({ package: packageID, version, digest: simulator!.digest, profile: "simulator", configuration: { commands: [{ id: "set-valve", point: "valve-position", min: "0", max: "100" }] } });
  await expect(page.getByRole("button", { name: "Save connection", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check", exact: true }).click();
  await expect.poll(async () => (await builder.record<Api.Connection>("build.connection", saved!.id)).state, { timeout: 20_000 }).toBe("ready");
  await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();
});

test("a third protocol imports documented HVAC points and exposes current readings to a business member", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const packageID = "protocol.modbus-tcp", version = "0.1.3";
  const configuration: Api.DriverConfiguration = JSON.parse(readFileSync(new URL("../../../deploy/examples/hvac/modbus.json", import.meta.url), "utf8"));
  await builder.open(page, "/apps?surface=tenant");
  const card = page.getByRole("region", { name: `Package ${packageID}`, exact: true });
  await card.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption(version);
  await card.getByRole("button", { name: "Install", exact: true }).click();
  await expect(card.getByText("active", { exact: true })).toBeVisible();
  const kinds = await builder.read<{ items: Api.ConnectionKind[] }>("connection-kinds");
  const kind = kinds.items.find(item => item.package === packageID && item.version === version);
  expect(kind?.profile?.operations).toEqual(["read", "sample"]);

  const name = slug("hvac");
  await builder.open(page, "/connection?id=new");
  await page.getByRole("textbox", { name: "Connection name", exact: true }).fill(name);
  await page.getByRole("textbox", { name: "Connection title", exact: true }).fill("HVAC protocol service · simulated values");
  await page.getByRole("combobox", { name: "Kind", exact: true }).selectOption(kind!.id);
  await page.getByRole("textbox", { name: "Gateway address", exact: true }).fill("http://127.0.0.1:18510");
  await page.getByRole("textbox", { name: "Secret name", exact: true }).fill("protocol-gateway");
  await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
  await page.getByRole("textbox", { name: "Modbus TCP address, with optional port (default 502) *", exact: true }).fill(String(configuration.connection!.address));
  for (const [index, device] of configuration.devices.entries()) {
    await page.getByRole("button", { name: "Add device", exact: true }).click();
    await page.getByRole("textbox", { name: "Device ID", exact: true }).nth(index).fill(device.id);
    await page.getByRole("textbox", { name: "Device title", exact: true }).nth(index).fill(device.title);
    await page.getByLabel("Modbus unit ID (0–255; default 1)", { exact: true }).nth(index).fill(String(device.settings!.unitID));
  }
  await page.getByRole("button", { name: "Import a point table", exact: true }).click();
  await page.getByLabel("Point table file", { exact: true }).setInputFiles({
    name: "hvac-points.csv", mimeType: "text/csv",
    buffer: readFileSync(new URL("../../../deploy/examples/hvac/modbus-points.csv", import.meta.url)),
  });
  await page.getByRole("button", { name: "Replace points with this table", exact: true }).click();
  await expect(page.getByRole("button", { name: "Add command", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Read-only preview", exact: true }).click();
  const preview = page.getByRole("region", { name: "Read-only preview", exact: true });
  await expect(preview.getByText("Read confirmed", { exact: true })).toBeVisible();
  for (const value of ["23.5", "123.4", "21.5", "35", "7.5", "12.5"]) {
    await expect(preview.getByRole("cell", { name: value, exact: true })).toBeVisible();
  }
  await expect(preview.getByRole("cell", { name: "good", exact: true })).toHaveCount(10);
  await shots(page, preview, "modbus-hvac-preview", info);
  await page.getByRole("button", { name: "Save connection", exact: true }).click();
  let saved: Api.Connection | undefined;
  await expect.poll(async () => {
    const records = await builder.read<{ records: Api.Connection[] }>("records/build.connection?limit=100");
    saved = records.records.find(record => record.name === name);
    return saved?.protocol?.configuration.points.length;
  }).toBe(10);
  await expect(page.getByRole("button", { name: "Save connection", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check", exact: true }).click();
  await expect.poll(async () => (await builder.record<Api.Connection>("build.connection", saved!.id)).state).toBe("ready");
  await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // A source uses the configured point instead of repeating its unit and URL.
  const equipment = await builder.object({
    title: "HVAC equipment", fields: [{ name: "device", title: "Device identity", type: "text", required: true }],
    access: [{ role: "user", read: "all", create: true }],
  });
  const [record] = await builder.records(equipment.type, [{ device: "mau-01" }]);
  const observation = { id: `${saved!.id}-CURRENT`, name: slug("hvaccurrent") };
  await builder.decide("build.observation.create", { type: "build.observation", id: observation.id }, {
    name: observation.name, title: "HVAC supply temperature", connection: saved!.id,
    object: equipment.type, deviceField: "device", signal: "pending", unit: "unit", path: "/observations",
    windowSeconds: 60, expectedRetentionSeconds: 3600, expectedGranularitySeconds: 1, roles: ["user", "builder"],
  });
  await builder.open(page, `/observation-source?id=${observation.id}`);
  await page.getByRole("combobox", { name: "Protocol point", exact: true }).selectOption("mau-supply-temperature");
  await expect(page.getByRole("textbox", { name: "Unit", exact: true })).toHaveValue("℃");
  await expect(page.getByRole("textbox", { name: "Relative adapter path", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => {
    const saved = await builder.record("build.observation", observation.id);
    return { signal: saved.signal, unit: saved.unit, path: saved.path };
  }).toEqual({ signal: "mau-supply-temperature", unit: "℃", path: "/observation" });
  await builder.activate(await builder.candidate([{ kind: "observation", id: observation.id }]));

  // The member reads one current point, while historical windows stay distinct.
  await operator.open(page, `/record?type=${equipment.type}&id=${record}&inspect=true`);
  await page.getByRole("button", { name: "Observations", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Observation read mode", exact: true })).toHaveValue("latest");
  await expect(page.getByRole("button", { name: "Evaluate declared decision", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Read current value", exact: true }).click();
  const current = page.getByRole("region", { name: "Current device reading", exact: true });
  await expect(current.getByText("23.5", { exact: false })).toBeVisible();
  await expect(current.getByText("Good quality", { exact: true })).toBeVisible();
  await expect(page.getByRole("region", { name: "External window summary", exact: true })).toHaveCount(0);
  await shots(page, page.locator("body"), "modbus-business-current", info);
  await page.getByRole("combobox", { name: "Observation read mode", exact: true }).selectOption("summary");
  await expect(current).toHaveCount(0);
  await page.getByRole("button", { name: "Read observation window", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Observation read is unavailable" })).toBeVisible();
  await expect(page.getByRole("region", { name: "External window summary", exact: true })).toHaveCount(0);
});
