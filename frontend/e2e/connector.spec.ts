import { packageInput } from "../../scripts/package-fixtures.mjs";
import { expect, test, type Page } from "@playwright/test";
import { signIn, submit } from "./support";

const contract = "connectortrial/1";
const impl = "build:kit.trial";
const capability = "/admin/capabilities/" + encodeURIComponent(contract);
const origin = process.env.PLATFORM_TRUNK_CONNECTOR_ORIGIN!;

async function api(page: Page, path: string, body?: unknown) {
  return page.evaluate(async ({ path, body }) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const response = await fetch(path, { method: body ? "POST" : "GET", headers: { Authorization: "Bearer " + session.token, "Platform-Tenant": session.tenant, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
    const answer = await response.json();
    if (!response.ok || answer.error) throw new Error(JSON.stringify(answer));
    return answer;
  }, { path, body });
}

async function credential(page: Page, id: string, kind: string, reference: string) {
  await submit(page, "platform.credential.save", "platform.credential", id, { id, title: id, kind, source: "deployment", reference, bindings: [{ consumer: "implementation/" + impl, origin }], expectedVersion: 0 });
}

async function callRecord(page: Page, destination: string) {
  const result = await api(page, "/v1/records/build.capabilitycall?limit=100");
  return (result.records as Array<{ id: string; protocol: string; payload?: { destination?: string }; state: string; answer?: string; effect?: string }>).find((row) => row.protocol === contract && row.payload?.destination === destination);
}

test("HTTPS connector: conformance, signed delivery and human settlement", async ({ page, request }) => {
  test.setTimeout(180_000);
  expect(origin).toMatch(/^https:\/\/127\.0\.0\.1:/);
  await signIn(page);
  for (const name of ["connectortrial-contract", "connectortrial"]) {
    const fixture = JSON.parse(packageInput(name).bytes.toString("utf8"));
    const inspected = await api(page, "/v1/packages/inspect", { content: fixture });
    const imported = await submit(page, "platform.package.import", "platform.package", fixture.id, { id: fixture.id, digest: inspected.digest }, 0);
    await submit(page, "platform.package.install", "platform.package", fixture.id, { id: fixture.id, version: fixture.version }, imported.record.revision);
  }
  await submit(page, "build.app.create", "build.app", "kitconnector", { name: "kitconnector", title: "Kit connector acceptance" }, 0);
  await submit(page, "build.implementation.create", "build.implementation", "KIT-TRIAL", { name: "kit.trial", title: "Kit HTTPS trial", project: "kitconnector", contract, kind: "connector", origins: [origin], requests: [{ action: "dispatch", method: "POST", path: "/dispatch" }] }, 0);
  await submit(page, "build.implementation.publish", "build.implementation", "KIT-TRIAL", {}, 1);
  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "connectortrial", role: "user" });
  await credential(page, "kit-wrong", "password", "KIT_TRUNK_WRONG");
  await credential(page, "kit-basic", "password", "KIT_TRUNK_BASIC");
  await credential(page, "kit-hmac", "api-key", "KIT_TRUNK_HMAC");
  await page.goto(capability);
  await expect(page.getByText("Kit HTTPS trial", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Enable", exact: true })).toBeDisabled();
  await page.getByText("Authentication", { exact: true }).click();
  await page.getByRole("option", { name: "Basic authentication", exact: true }).click();
  await page.getByText("Credential", { exact: true }).click();
  await page.getByRole("option", { name: "kit-wrong (kit-wrong)", exact: true }).click();
  await page.getByRole("textbox", { name: "Request templates" }).fill(JSON.stringify({ dispatch: { to: "{destination}", channel: "fixture" } }));
  await page.getByRole("button", { name: "Save and run tests" }).click();
  await expect(page.getByText("Conformance tests requested. Refresh in a moment to view the latest results.", { exact: true })).toBeVisible();
  await expect.poll(async () => (await api(page, "/v1/records/build.connectorsetup?limit=100")).records.find((row: { implementation: string }) => row.implementation === impl)?.state, { timeout: 30_000 }).toBe("failed");
  await page.reload();
  await expect(page.getByText("Last conformance test: 0/2 passed")).toBeVisible();
  await expect(page.getByRole("button", { name: "Enable", exact: true })).toBeDisabled();
  await page.getByText("Credential", { exact: true }).click();
  await page.getByRole("option", { name: "kit-basic (kit-basic)", exact: true }).click();
  await page.getByRole("button", { name: "Save and run tests" }).click();
  await expect(page.getByText("Conformance tests requested. Refresh in a moment to view the latest results.", { exact: true })).toBeVisible();
  await expect.poll(async () => (await api(page, "/v1/records/build.connectorsetup?limit=100")).records.find((row: { implementation: string }) => row.implementation === impl)?.state, { timeout: 30_000 }).toBe("passed");
  await page.reload();
  await expect(page.getByText("Last conformance test: 2/2 passed")).toBeVisible();
  if (await page.getByRole("button", { name: "Configure", exact: true }).count()) await page.getByRole("button", { name: "Configure", exact: true }).click();
  await page.getByText("Authentication", { exact: true }).click();
  await page.getByRole("option", { name: "HMAC-SHA256", exact: true }).click();
  await page.getByText("Credential", { exact: true }).click();
  await page.getByRole("option", { name: "kit-hmac (kit-hmac)", exact: true }).click();
  await page.getByRole("button", { name: "Save and run tests" }).click();
  await expect(page.getByText("Conformance tests requested. Refresh in a moment to view the latest results.", { exact: true })).toBeVisible();
  await expect.poll(async () => (await api(page, "/v1/records/build.connectorsetup?limit=100")).records.find((row: { implementation: string }) => row.implementation === impl)?.state, { timeout: 30_000 }).toBe("passed");
  await page.reload();
  await expect(page.getByRole("button", { name: "Enable", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Enable", exact: true }).click();
  await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();
  const dispatch = async (destination: string) => {
    await submit(page, "connectortrial.dispatch.create", "connectortrial.dispatch", "kit-" + destination, { destination }, 0);
    const id = "kit-" + destination;
    await page.goto("/connectortrial/connectortrial.dispatch/" + encodeURIComponent(id));
    await page.getByRole("button", { name: "More actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "Send dispatch", exact: true }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Send dispatch", exact: true }).click();
    await expect.poll(async () => (await callRecord(page, destination))?.state, { timeout: 30_000 }).toBe(destination === "success" ? "answered" : "unknown");
  };
  await dispatch("success");
  await dispatch("retry");
  expect((await callRecord(page, "success"))?.answer).toContain("received-success");
  const counts = async () => (await request.get(origin + "/counts", { ignoreHTTPSErrors: true })).json();
  expect((await counts()).retry).toBe(1);
  await page.goto(capability);
  const retry = await callRecord(page, "retry");
  expect(retry?.effect).toBeTruthy();
  await page.getByRole("link", { name: "dispatch · " + retry!.id, exact: true }).locator("xpath=ancestor::*[.//button[normalize-space()='Retry call']][1]").getByRole("button", { name: "Retry call", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Retry call", exact: true }).click();
  await expect.poll(async () => (await callRecord(page, "retry"))?.state, { timeout: 30_000 }).toBe("answered");
  expect((await counts()).retry).toBe(2);
  await dispatch("timeout");
  expect((await counts()).timeout).toBe(1);
  await page.goto(capability);
  const timeout = await callRecord(page, "timeout");
  expect(timeout?.effect).toBeTruthy();
  await page.getByRole("link", { name: "dispatch · " + timeout!.id, exact: true }).locator("xpath=ancestor::*[.//button[normalize-space()='Mark failed']][1]").getByRole("button", { name: "Mark failed", exact: true }).click();
  await page.getByRole("dialog").getByRole("textbox", { name: /reason/i }).fill("Provider confirmed no dispatch");
  await page.getByRole("dialog").getByRole("button", { name: "Mark failed", exact: true }).click();
  await expect.poll(async () => (await callRecord(page, "timeout"))?.state).toBe("failed");
  expect((await counts()).timeout).toBe(1);
  const raw = JSON.stringify(await api(page, "/v1/records/build.capabilitycall?limit=100"));
  expect(raw).not.toContain("kit-trunk-signing-key");
  expect(raw).not.toContain("kit-trunk-password");
});
