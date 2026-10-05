import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("a workflow upgrade preserves existing data and waiting runs and refuses a storage migration", async ({ page, request }, testInfo) => {
  const name = fresh("upgrade").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const type = `build.${name}`, flowName = `${name}flow`;
  const objectID = fresh("OBJ"), flowID = fresh("FLOW"), oldRecord = fresh("OLD"), newRecord = fresh("NEW");
  const builder = { Authorization: "Bearer manager" }, user = { Authorization: "Bearer desk" };
  const record = async (kind: string, id: string, headers = builder) => (await (await request.get(`/v1/records/${kind}/${id}`, { headers })).json()).record;
  const active = async () => (await (await request.get("/v1/releases/active", { headers: user })).json()).id;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name, title: "Upgrade sample", fields: [{ name: "note", title: "Note", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" }],
    actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }, { name: "reject", title: "Reject", from: ["open"], to: "rejected" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.process.create", { type: "build.process", id: flowID }, {
    name: flowName, title: "Versioned review", object: type, when: "open",
    steps: [{ name: "review", title: "Original review", kind: "ask", ask: "user", answers: ["approve"], cases: { approve: "close" } },
      { name: "close", title: "Original close", kind: "action", act: "close" }],
  });
  await open(page, "manager", `/release-review?kind=flow&id=${flowID}`);

  const activate = async () => {
    await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
    await expect(page.getByText("Candidate ready for review", { exact: true })).toBeVisible();
    const id = await page.getByText("Draft candidate:").locator("code").innerText();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
    await expect.poll(active).toBe(id);
    return id;
  };
  const first = await activate();
  await decide(request, "desk", "build", `${type}.create`, { type, id: oldRecord }, { note: "Existing operator data" });
  const oldRunID = `build.${flowName}:${oldRecord}`;
  await expect.poll(async () => (await record("flow.instance", oldRunID, user))?.state).toBe("waiting");
  const waiting = await record("flow.instance", oldRunID, user);
  expect(waiting).toMatchObject({ version: 1, release: first, dependencies: first });
  const operator = await page.context().newPage();
  await open(operator, "desk", `/flow?id=${encodeURIComponent(oldRunID)}`);
  const releaseButton = operator.getByRole("button", { name: "Last activated release", exact: true });
  await expect(releaseButton).toHaveAttribute("title", first);
  await releaseButton.click();
  const releaseDialog = operator.getByRole("dialog", { name: "Last activated release", exact: true });
  await expect(releaseDialog.locator("code")).toHaveText(first);
  await releaseDialog.getByRole("button", { name: "Close", exact: true }).click();
  expect((await request.get("/v1/releases/candidates?limit=1", { headers: user })).status()).toBe(403);

  // A changed workflow version is supported; its object/action dependencies stay intact.
  await decide(request, "manager", "build", "build.process.edit", { type: "build.process", id: flowID }, {
    steps: [{ name: "reject", title: "New rejection path", kind: "action", act: "reject" }],
  });
  await page.goto(`/#/release-review?kind=flow&id=${flowID}`);
  const second = await activate();
  expect(second).not.toBe(first);
  await expect(releaseButton).toHaveAttribute("title", second, { timeout: 10_000 });
  expect(await record(type, oldRecord, user)).toMatchObject({ note: "Existing operator data", state: "open" });
  expect(await record("flow.instance", oldRunID, user)).toMatchObject({ state: "waiting", version: 1, release: first });
  await decide(request, "desk", "build", `${type}.create`, { type, id: newRecord }, { note: "New operator data" });
  await expect.poll(async () => (await record(type, newRecord, user))?.state).toBe("rejected");
  expect(await record("flow.instance", `build.${flowName}:${newRecord}`, user)).toMatchObject({ version: 2, release: second, dependencies: second });

  // The operator sees the old run's startup release, not today's tenant pointer.
  await operator.getByText("Release binding", { exact: true }).click();
  await expect(operator.getByText("This run stays on its recorded version.", { exact: true })).toBeVisible();
  await expect(operator.getByText("Started under release", { exact: true }).locator("xpath=following-sibling::dd[1]")).toHaveText(first);
  await releaseButton.click();
  await expect(releaseDialog.locator("code")).toHaveText(second);
  await operator.route("**/v1/releases/active", (route) => route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"unavailable"}' }));
  await releaseDialog.getByRole("button", { name: "Refresh release", exact: true }).click();
  await expect(releaseDialog.getByRole("alert")).toHaveText("The active release could not be read. Retry to check the current identifier.");
  await expect(releaseDialog.locator("code")).toHaveCount(0);
  await releaseDialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(releaseButton).toHaveText("Release unavailable");
  await expect(releaseButton).not.toHaveAttribute("title", second);
  await releaseButton.click();
  await operator.unroute("**/v1/releases/active");
  await releaseDialog.getByRole("button", { name: "Refresh release", exact: true }).click();
  await expect(releaseDialog.locator("code")).toHaveText(second);
  await releaseDialog.getByRole("button", { name: "Close", exact: true }).click();
  if (process.env.PLATFORM_SCREENSHOTS) await operator.screenshot({ path: testInfo.outputPath("retained-run-release.png") });
  // The shared menu keeps release information reachable when header status is hidden.
  const viewport = operator.viewportSize();
  if (process.env.PLATFORM_SCREENSHOTS) await operator.setViewportSize({ width: 390, height: 844 });
  await operator.getByRole("button", { name: "Search and commands", exact: true }).click();
  await operator.getByRole("dialog", { name: "Command palette", exact: true }).getByRole("option", { name: "Last activated release", exact: true }).click();
  await expect(releaseDialog.locator("code")).toHaveText(second);
  if (process.env.PLATFORM_SCREENSHOTS) await operator.screenshot({ path: testInfo.outputPath("active-release-menu.png") });
  await releaseDialog.getByRole("button", { name: "Close", exact: true }).click();
  if (process.env.PLATFORM_SCREENSHOTS && viewport) await operator.setViewportSize(viewport);
  await operator.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Inbox", exact: true }).click();
  const task = operator.getByRole("listitem").filter({ hasText: oldRecord });
  await expect(task).toBeVisible();
  await task.getByRole("button", { name: /^approve$/i }).click();
  await expect.poll(async () => (await record(type, oldRecord, user))?.state).toBe("done");
  expect(await record(type, oldRecord, user)).toMatchObject({ note: "Existing operator data", state: "done" });
  expect(await record(type, newRecord, user)).toMatchObject({ note: "New operator data", state: "rejected" });

  // Adding a stored field still requires migration, even after waiting work finishes.
  await decide(request, "manager", "build", "build.object.edit", { type: "build.object", id: objectID }, {
    fields: [{ name: "note", title: "Note", type: "text" }, { name: "priority", title: "Priority", type: "integer" }],
  });
  await page.goto(`/#/release-review?kind=object&id=${objectID}`);
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await expect(page.getByText("Candidate ready for review", { exact: true })).toBeVisible();
  const unsupported = await page.getByText("Draft candidate:").locator("code").innerText();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: /storage shape.*upgrade plan/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeDisabled();
  const refused = await request.post("/v1/releases/active", { headers: builder, data: { candidateId: unsupported, key: fresh("UPGRADE") } });
  expect(refused.ok()).toBe(false);
  expect(await active()).toBe(second);
  const entities = await (await request.get("/v1/entities", { headers: user })).json();
  expect(entities.find((entity: { type: string }) => entity.type === type).fields.map((field: { name: string }) => field.name)).not.toContain("priority");
  expect(await record(type, oldRecord, user)).toMatchObject({ note: "Existing operator data", state: "done" });
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("unsupported-record-upgrade.png") });
});
