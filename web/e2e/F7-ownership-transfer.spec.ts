import { readFileSync } from "node:fs";
import { acceptedClient, expect, signIn, test } from "./kit";
import type { PackageDescriptor, PackageOwnership, PackagePreview, ResourceDirectory } from "../packages/api/src/gen/host";

test.use({ viewport: { width: 1680, height: 1050 } });

test("F7 an administrator transfers a product's resources to its successor and retires the predecessor", async ({ page }) => {
  test.setTimeout(300_000);
  const api = acceptedClient(page);
  const successor = JSON.parse(readFileSync(new URL("../../deploy/packages/manufacturing-core.json", import.meta.url), "utf8")) as PackageDescriptor;
  const predecessor = { ...successor, id: "acceptance.predecessor", namespace: "acceptance.predecessor", title: "Predecessor product" };
  const offer = async (descriptor: PackageDescriptor) => {
    const response = await page.request.post("/v1/packages/inspect", { headers: api.headers, data: { content: descriptor } });
    expect(response.ok(), await response.text()).toBeTruthy();
    const preview = await response.json() as PackagePreview;
    await api.submit("platform.package", descriptor.id, "import", { id: descriptor.id, digest: preview.digest }, null);
  };
  await offer(predecessor);
  await api.submit("platform.package", predecessor.id, "install", { id: predecessor.id, version: predecessor.version }, null);
  const before = (await api.read<ResourceDirectory>("/v1/resources")).resources.filter(resource => resource.managedBy === predecessor.id && resource.draft !== `package:${predecessor.id}`);
  expect(before.length).toBeGreaterThan(0);
  await offer(successor);
  await signIn(page, "admin", "/apps");
  await page.getByRole("region", { name: "Package library" }).getByRole("button", { name: new RegExp(`^${successor.title}`) }).click();
  await page.getByRole("button", { name: "Transfer existing resources…", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Transfer resource ownership" });
  await expect(dialog.getByRole("list", { name: "Resources to transfer" }).getByRole("listitem").first()).toBeVisible();
  await dialog.getByRole("button", { name: "Transfer ownership", exact: true }).click();
  await expect(dialog).toContainText("Ownership is ready.");
  await dialog.getByRole("button", { name: "Close", exact: true }).last().click();
  expect((await api.read<PackageOwnership>(`/v1/packages/${successor.id}/ownership?version=${successor.version}`)).entries).toEqual([]);
  await api.submit("platform.package", successor.id, "install", { id: successor.id, version: successor.version }, null);
  await api.submit("platform.package", predecessor.id, "retire", { id: predecessor.id }, null);
  const after = await api.read<ResourceDirectory>("/v1/resources");
  for (const resource of before) {
    const current = after.resources.find(value => value.id === resource.id)!;
    expect(current.draft).toBe(resource.draft);
    expect(current.state).not.toBe("archived");
    expect(current.managedBy).toBe(successor.id);
  }
});
