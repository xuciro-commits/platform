import { createServer } from "node:http";
import { once } from "node:events";
import { test, expect, slug, shots } from "./kit";

test("an administrator manages a credential and edits its AI provider without losing enabled models", async ({ page, builder }, info) => {
  test.setTimeout(90_000);
  const name = slug("provider"), credential = `${name}-key`;
  let secret = "browser-credential-first";
  let calls = 0;
  const service = createServer((request, response) => {
    calls++;
    response.setHeader("Content-Type", "application/json");
    if (request.headers.authorization !== `Bearer ${secret}`) { response.statusCode = 401; response.end('{"error":"invalid credential"}'); return; }
    response.end('{"data":[{"id":"example","name":"Example model"}]}');
  });
  service.listen(0, "127.0.0.1"); await once(service, "listening");
  try {
    const address = service.address(); if (!address || typeof address === "string") throw Error("Model service did not listen");
    const origin = `http://127.0.0.1:${address.port}`;
    await builder.open(page, "/ai-providers");
    await page.getByRole("button", { name: "Add provider", exact: true }).click();
    const providerDialog = page.getByRole("dialog", { name: "Add AI provider", exact: true });
    await providerDialog.getByRole("combobox", { name: "Kind", exact: true }).selectOption("local");
    await providerDialog.getByRole("textbox", { name: "ID", exact: true }).fill(name);
    await providerDialog.getByRole("textbox", { name: "Base URL", exact: true }).fill(origin + "/v1");
    await providerDialog.getByRole("button", { name: "Create credential", exact: true }).click();
    const credentialDialog = page.getByRole("dialog", { name: "Add credential", exact: true });
    await expect(credentialDialog.getByRole("textbox", { name: "Credential name", exact: true })).toHaveValue(credential);
    await credentialDialog.getByLabel("Secret value", { exact: true }).fill(secret);
    await credentialDialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(credentialDialog).not.toBeVisible();
    await expect(providerDialog.getByRole("combobox", { name: "Credential", exact: true })).toHaveValue(credential);
    await providerDialog.getByRole("button", { name: "Add", exact: true }).click();
    await expect(providerDialog).not.toBeVisible();
    const providerCard = page.locator("div").filter({ has: page.locator("span.font-semibold").filter({ hasText: name }) }).filter({ has: page.getByRole("button", { name: "Check connection", exact: true }) }).last();
    await providerCard.getByRole("button", { name: "Check connection", exact: true }).click();
    await expect(providerCard.getByRole("status")).toHaveText("Connection verified");
    await providerCard.getByRole("button", { name: "Models", exact: true }).click();
    await page.getByRole("row").filter({ hasText: "example" }).getByRole("button", { name: "AI members only", exact: true }).click();
    await providerCard.getByRole("button", { name: "Edit", exact: true }).click();
    const editProvider = page.getByRole("dialog", { name: "Edit AI provider", exact: true });
    await editProvider.getByRole("textbox", { name: "Base URL", exact: true }).fill(origin + "/v2");
    await editProvider.getByRole("button", { name: "Save", exact: true }).click();
    await expect(editProvider).not.toBeVisible();
    await expect.poll(async () => (await builder.read<any[]>("ai-models")).some(model => model.provider === name && model.model === "example")).toBe(true);
    await builder.open(page, "/credentials");
    const credentialCard = page.locator("div").filter({ has: page.locator("span.font-mono").filter({ hasText: credential }) }).filter({ has: page.getByRole("button", { name: "Disable", exact: true }) }).last();
    await credentialCard.getByRole("button", { name: "Edit", exact: true }).click();
    const editCredential = page.getByRole("dialog", { name: "Edit credential", exact: true });
    await expect(editCredential.getByLabel("Secret value", { exact: true })).toHaveValue("");
    secret = "browser-credential-second";
    await editCredential.getByLabel("Secret value", { exact: true }).fill(secret);
    await editCredential.getByLabel("Allowed origin", { exact: true }).fill("");
    await expect(editCredential.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await editCredential.getByRole("combobox", { name: "Consumer ID", exact: true }).selectOption(name);
    await expect(editCredential.getByLabel("Allowed origin", { exact: true })).toHaveValue(origin);
    await editCredential.getByRole("button", { name: "Save", exact: true }).click();
    await expect(editCredential).not.toBeVisible();
    await expect.poll(async () => (await builder.read<any>("credentials")).credentials.find((item: any) => item.id === credential)?.version).toBe(2);
    const stored = await page.evaluate(async () => {
      const values = [JSON.stringify(localStorage), JSON.stringify(sessionStorage)];
      for (const item of await indexedDB.databases()) {
        if (!item.name) continue;
        const database = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open(item.name!); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
        try { for (const store of Array.from(database.objectStoreNames)) values.push(JSON.stringify(await new Promise((resolve, reject) => { const request = database.transaction(store).objectStore(store).getAll(); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); }))); }
        finally { database.close(); }
      }
      return values.join("\n");
    });
    expect(stored).not.toContain("browser-credential-first"); expect(stored).not.toContain(secret);
    await shots(page, page.locator("body"), "credential-management", info);
    await builder.open(page, "/ai-providers");
    await providerCard.getByRole("button", { name: "Check connection", exact: true }).click();
    await expect(providerCard.getByRole("status")).toHaveText("Connection verified");
    await builder.open(page, "/credentials");
    await credentialCard.getByRole("button", { name: "Disable", exact: true }).click();
    await expect.poll(async () => (await builder.read<any>("credentials")).credentials.find((item: any) => item.id === credential)?.state).toBe("disabled");
    const before = calls;
    await builder.open(page, "/ai-providers");
    await providerCard.getByRole("button", { name: "Check connection", exact: true }).click();
    await expect(providerCard.getByRole("status")).not.toHaveText("Checking…");
    await expect(providerCard.getByRole("status")).not.toHaveText("Connection verified");
    await expect(providerCard.getByRole("status")).toHaveText("Credential is disabled. Enable or replace it in Credentials.");
    expect(calls).toBe(before);
  } finally { service.close(); await once(service, "close"); }
});
