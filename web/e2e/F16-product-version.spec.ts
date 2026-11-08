import type { PackageDescriptor, PackagePreview, PackageView, ResourceDirectory } from "../packages/api/src/gen/host";
import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

test("C15 a product chooses contents, freezes a saved candidate as a version, and Marketplace consumes only that version", async ({ page }) => {
  test.setTimeout(180_000);
  const source = acceptedClient(page, "publisher", "publisher"), customer = acceptedClient(page);
  const tag = unique("chain"), object = unique("CHAINOBJ"), app = unique("CHAINAPP"), project = unique("CHAINPROJECT");
  const type = `build.${tag}`, product = `${tag}product`, title = `Chain product ${tag}`;
  const ref = (kind: string, name: string) => ({ app: "build", kind, name });

  await source.submit("build.app", project, "create", { name: project, title: "Product project" });
  await source.submit("build.object", object, "create", {
    name: tag, title: "Product record", project, fields: [{ name: "name", title: "Name", type: "text" }],
  });
  await source.submit("build.app", app, "create", { name: `${tag}app`, title: "Product application", resources: [ref("object", type)] });
  await source.submit("build.product", product, "create", {
    name: product, title, description: "Created through the product chain.", project: app,
    roots: [ref("app", `${tag}app`)], requires: [],
  });

  await signIn(page, "publisher", "/products");
  await page.getByRole("button", { name: new RegExp(title) }).click();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await expect(page.getByText(`build/app/${tag}app`, { exact: true })).toBeVisible();
  const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Create release candidate", exact: true }).click();
  const releasePreview = await previewResponse;
  expect(releasePreview.ok(), await releasePreview.text()).toBeTruthy();
  const candidate = (await releasePreview.json()).candidateId as string;
  const savedResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/candidates") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  const saved = await savedResponse;
  expect(saved.ok(), await saved.text()).toBeTruthy();
  expect((await saved.json()).id).toBe(candidate);

  await page.goto("/#/products");
  await page.getByRole("button", { name: new RegExp(title) }).click();
  await page.getByLabel("Semantic version").fill("1.2.0");
  await page.getByLabel("Saved candidate").selectOption(candidate);
  await page.getByRole("button", { name: "Publish product version", exact: true }).click();
  await expect(page.getByText("Immutable", { exact: true })).toBeVisible();

  const versionRecord = await source.read<{ record: { descriptor: PackageDescriptor; candidate: string } }>(`/v1/records/build.productversion/${encodeURIComponent(`${product}@1.2.0`)}`);
  expect(versionRecord.record.candidate).toBe(candidate);
  expect(versionRecord.record.descriptor.blueprint, "the frozen version carries the candidate closure").toBeTruthy();

  const catalog = await source.read<PackageView[]>("/v1/packages");
  const offered = catalog.find(item => item.descriptor.id === product && item.descriptor.version === "1.2.0");
  expect(offered?.source).toBe("product");
  expect(offered?.candidate).toBe(candidate);

  const generic = { id: "generic", version: "1.0.0", namespace: "generic", title: "Generic package", contributions: [] };
  const refused = await page.request.post("/v1/packages/inspect", { headers: customer.headers, data: { content: generic } });
  expect(refused.status()).toBe(400);
  expect(await refused.text()).toContain("product versions only");

  const inspected = await page.request.post("/v1/packages/inspect", { headers: customer.headers, data: { content: versionRecord.record.descriptor } });
  expect(inspected.ok(), await inspected.text()).toBeTruthy();
  const packagePreview = await inspected.json() as PackagePreview;
  await customer.submit("platform.package", product, "import", { id: product, digest: packagePreview.digest }, null);
  await customer.submit("platform.package", product, "install", { id: product, version: "1.2.0" }, null);

  const installed = (await customer.read<PackageView[]>("/v1/packages")).find(item => item.descriptor.id === product);
  expect(installed?.installed?.version).toBe("1.2.0");
  const directory = await customer.read<ResourceDirectory>("/v1/resources");
  const managed = directory.resources.find(resource => resource.ref.name === type && resource.ref.kind === "object");
  expect(managed?.managedBy).toBe(product);
  expect(managed?.project).toBe(`package:${product}`);
});
