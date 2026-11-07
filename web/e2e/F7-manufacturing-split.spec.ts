import { readFileSync } from "node:fs";
import { acceptedClient, expect, signIn, test } from "./kit";
import type { PackageDescriptor, PackageOwnership, PackagePreview, PackageView, ResourceDirectory } from "../packages/kernel/src/gen/host";

test.use({ viewport: { width: 1680, height: 1050 } });

test("F7 a manufacturing administrator migrates legacy ownership into independently removable ERP and MES products", async ({ page }) => {
  test.setTimeout(300_000);
  const api = acceptedClient(page);
  const load = (name: string) => JSON.parse(readFileSync(new URL(`../../deploy/packages/${name}.json`, import.meta.url), "utf8")) as PackageDescriptor;
  const legacy = load("manufacturing"), products = [load("manufacturing-core"), load("manufacturing-erp"), load("manufacturing-mes")];
  const offer = async (descriptor: PackageDescriptor) => {
    const response = await page.request.post("/v1/packages/inspect", { headers: api.headers, data: { content: descriptor } });
    expect(response.ok(), await response.text()).toBeTruthy();
    const preview = await response.json() as PackagePreview;
    await api.submit("platform.package", descriptor.id, "import", { id: descriptor.id, digest: preview.digest }, null);
  };
  await offer(legacy);
  await api.submit("platform.package", legacy.id, "install", { id: legacy.id, version: legacy.version }, null);
  await api.submit("build.erpaccount", "SPLITACCOUNT", "create", { code: "SPLIT", name: "Preserved account", kind: "asset", posting: true, active: true });
  const before = await api.read<ResourceDirectory>("/v1/resources");
  for (const product of products) await offer(product);
  await signIn(page, "admin", "/apps");
  for (const product of products) {
    await page.getByRole("region", { name: "Package library" }).getByRole("button", { name: new RegExp(`^${product.title}`) }).click();
    await page.getByRole("button", { name: "Transfer existing resources…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "Transfer resource ownership" });
    await expect(dialog.getByRole("list", { name: "Resources to transfer" }).getByRole("listitem").first()).toBeVisible();
    await dialog.getByRole("button", { name: "Transfer ownership", exact: true }).click();
    await expect(dialog).toContainText("Ownership is ready.");
    await dialog.getByRole("button", { name: "Close", exact: true }).last().click();
    expect((await api.read<PackageOwnership>(`/v1/packages/${product.id}/ownership?version=${product.version}`)).entries).toEqual([]);
    await api.submit("platform.package", product.id, "install", { id: product.id, version: product.version }, null);
  }
  await api.submit("platform.package", legacy.id, "retire", { id: legacy.id }, null);
  const after = await api.read<ResourceDirectory>("/v1/resources");
  for (const resource of before.resources.filter(resource => resource.managedBy === legacy.id && resource.draft !== `package:${legacy.id}`)) {
    const current = after.resources.find(value => value.id === resource.id)!;
    expect(current.draft).toBe(resource.draft);
    expect(current.state).not.toBe("archived");
    expect(current.managedBy).toMatch(/^manufacturing\.(core|erp|mes)$/);
  }
  expect((await api.read<{ record: { name: string } }>("/v1/records/build.erpaccount/SPLITACCOUNT")).record.name).toBe("Preserved account");
  await api.submit("platform.package", "manufacturing.mes", "retire", { id: "manufacturing.mes" }, null);
  expect((await api.read<PackageView[]>("/v1/packages")).find(view => view.descriptor.id === "manufacturing.erp")?.installed?.state).toBe("active");
  expect((await api.read<{ record: { name: string } }>("/v1/records/build.erpaccount/SPLITACCOUNT")).record.name).toBe("Preserved account");
});
