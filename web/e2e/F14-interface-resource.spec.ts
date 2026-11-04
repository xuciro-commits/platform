import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F14 a builder creates and publishes a tenant interface, then an object type implements it", async ({ page }) => {
  test.setTimeout(180_000);
  const client = acceptedClient(page, "builder");
  const project = unique("interfaceproject");
  const interfaceName = `build.${unique("coded")}`;
  const interfaceTitle = "Coded interface";
  const objectName = unique("codedobject");
  const objectID = unique("OBJ");

  await client.submit("build.app", project, "create", { name: project, title: "Interface project" }, 0);
  await signIn(page, "builder", `/interfaces?id=new&application=${project}`);
  await expect(page.getByRole("heading", { name: "New interface", level: 1 })).toBeVisible();

  await page.getByLabel(/^Interface name/).fill(interfaceName);
  await page.getByLabel("Interface title", { exact: true }).fill(interfaceTitle);
  await page.getByLabel("Interface description", { exact: true }).fill("A shared shape created from the Interfaces workbench.");
  await page.getByRole("button", { name: /^Fields/ }).click();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  await page.getByLabel("Field name", { exact: true }).fill("code");
  await page.getByLabel("Field title", { exact: true }).fill("Code");
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  await page.getByLabel("Field name", { exact: true }).nth(1).fill("name");
  await page.getByLabel("Field title", { exact: true }).nth(1).fill("Name");
  await page.getByRole("button", { name: "Create", exact: true }).click();

  const readDirectory = () => client.read<{ resources: { type: string; draft?: string; ref: { kind: string; name: string } }[] }>("/v1/resources");
  await expect.poll(async () => (await readDirectory()).resources.find(resource => resource.ref.kind === "interface" && resource.ref.name === interfaceName)?.draft).toEqual(expect.any(String));
  const interfaceID = (await readDirectory()).resources.find(resource => resource.ref.kind === "interface" && resource.ref.name === interfaceName)!.draft!;
  const readInterface = () => client.read<{ record: { name: string; state: string; version?: number; published?: string } }>(`/v1/records/build.interface/${interfaceID}`);
  await expect.poll(async () => (await readInterface()).record.name).toBe(interfaceName);

  const publish = page.getByRole("button", { name: "Publish interface", exact: true });
  await expect(publish).toBeEnabled();
  await publish.click();
  await expect.poll(async () => (await readInterface()).record.state).toBe("published");
  expect((await readInterface()).record.published).toBeTruthy();

  await client.submit("build.object", objectID, "create", {
    name: objectName, title: "UI object", project,
    fields: [{ name: "code", title: "Code", type: "text" }, { name: "name", title: "Name", type: "text" }],
    implements: [interfaceName],
  }, 0);
  await client.submit("build.object", objectID, "publish", {}, null);

  await page.goto(`/#/interfaces?application=${project}`);
  await expect(page.getByText(interfaceTitle, { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "UI object", exact: true })).toBeVisible();
});
