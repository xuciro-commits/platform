import { type Page, expect, test } from "@playwright/test";
import { signIn } from "./support";

async function submit(page: Page, schema: string, targetType: string, targetId: string, payload: Record<string, unknown>, revision?: number) {
  return page.evaluate(async ({ schema, targetType, targetId, payload, revision }) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const headers = { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant, "Content-Type": "application/json" };
    const [catalog, identity] = await Promise.all([
      fetch("/v1/actions", { headers }).then((r) => r.json()),
      fetch("/v1/me", { headers }).then((r) => r.json()),
    ]);
    const action = catalog.find((entry: { schema: string }) => entry.schema === schema);
    if (!action) throw new Error(`missing action ${schema}`);
    const bytes = new TextEncoder().encode(JSON.stringify(payload));
    const encoded = btoa(String.fromCharCode(...bytes));
    const body: Record<string, unknown> = {
      tenantId: identity.tenantId,
      principalId: identity.principalId,
      authority: action.authority || "build",
      target: { type: targetType, id: targetId },
      schema: { name: schema, version: action.schemaVersion.family },
      idempotencyKey: crypto.randomUUID(),
      payload: encoded,
    };
    if (revision !== undefined) body.expectedRevision = revision;
    const response = await fetch("/v1/submissions", {
      method: "POST",
      headers: { ...headers, "Platform-Schema-Version": action.schemaVersion.version },
      body: JSON.stringify(body),
    });
    const answer = await response.json();
    if (!response.ok || answer.error) throw new Error(JSON.stringify(answer));
    return answer;
  }, { schema, targetType, targetId, payload, revision });
}

test.describe.serial("gantt layout", () => {
  const now = new Date();
  const ym = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
  const day = (n: number) => `${ym}-${String(n).padStart(2, "0")}`;

  test("gantt: renders bars on dates, drags with persistence, unscheduled sidebar, scales and read-only guard", async ({ page, browser }) => {
    await signIn(page);
    await submit(page, "build.object.create", "build.object", "GANTT-TASK", {
      project: "sales",
      name: "gantttask",
      title: "Gantt task",
      plural: "Gantt tasks",
      display: "title",
      fields: [
        { name: "title", title: "Title", type: "text", required: true },
        { name: "start", title: "Start date", type: "date" },
        { name: "end", title: "End date", type: "date" },
        { name: "status", title: "Status", type: "choice", choices: "todo:Todo,doing:Doing,done:Done" },
      ],
      access: [{ role: "user", read: "all" }],
      view: { version: 1, layout: "gantt", dates: "start", until: "end", group: "status", lanes: "status" },
    });
    await submit(page, "build.object.publish", "build.object", "GANTT-TASK", {});
    await submit(page, "sales.gantttask.create", "sales.gantttask", "GT-1", {
      title: "Design review",
      start: day(5),
      end: day(9),
      status: "doing",
    });
    await submit(page, "sales.gantttask.create", "sales.gantttask", "GT-2", {
      title: "API integration",
      start: day(10),
      end: day(14),
      status: "todo",
    });
    await submit(page, "sales.gantttask.create", "sales.gantttask", "GT-3", {
      title: "Backlog item",
      status: "todo",
    });

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/ontology/objects/sales.gantttask");
    await expect(page.getByRole("heading", { level: 1, name: "Gantt tasks", exact: true })).toBeVisible();
    await expect(page.getByRole("radio", { name: "Gantt", exact: true })).toBeChecked();

    const barA = page.getByRole("button", { name: `Design review (${day(5)} – ${day(9)})`, exact: true });
    const barB = page.getByRole("button", { name: `API integration (${day(10)} – ${day(14)})`, exact: true });
    await expect(barA).toBeVisible();
    await expect(barB).toBeVisible();
    const boxA = (await barA.boundingBox())!;
    const boxB = (await barB.boundingBox())!;
    expect(boxB.x).toBeGreaterThan(boxA.x);

    const unscheduled = page.getByRole("region", { name: "Unscheduled", exact: true });
    await expect(unscheduled).toBeVisible();
    await expect(unscheduled.getByRole("button", { name: "Backlog item" })).toBeVisible();

    const moved = page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.status() === 200);
    await page.mouse.move(boxA.x + boxA.width / 2, boxA.y + boxA.height / 2);
    await page.mouse.down();
    await page.mouse.move(boxA.x + boxA.width / 2 + 72, boxA.y + boxA.height / 2, { steps: 6 });
    await page.mouse.up();
    await moved;

    const movedBarA = page.getByRole("button", { name: `Design review (${day(7)} – ${day(11)})`, exact: true });
    await expect(movedBarA).toBeVisible();
    await page.reload();
    await expect(movedBarA).toBeVisible();

    await movedBarA.click();
    const details = page.getByRole("dialog", { name: "Details" });
    await expect(details.getByRole("heading", { level: 2, name: "Design review" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(details).toBeHidden();

    const dayWidthBox = (await movedBarA.boundingBox())!;
    await page.getByRole("radio", { name: "Week", exact: true }).click();
    await expect(page).toHaveURL(/scale.*week/);
    await expect(movedBarA).toBeVisible();
    const weekWidthBox = (await movedBarA.boundingBox())!;
    expect(weekWidthBox.width).toBeLessThan(dayWidthBox.width);

    await page.getByRole("radio", { name: "Month", exact: true }).click();
    await expect(page).toHaveURL(/scale.*month/);
    await expect(movedBarA).toBeVisible();

    await page.getByRole("radio", { name: "Day", exact: true }).click();
    await expect(page).toHaveURL(/scale.*day/);

    const readerContext = await browser.newContext();
    const reader = await readerContext.newPage();
    await reader.setViewportSize({ width: 1440, height: 900 });
    await reader.goto("/");
    await reader.getByRole("button", { name: "user-1 · acceptance" }).click();
    await expect(reader.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await reader.goto("/ontology/objects/sales.gantttask");
    const readerBarA = reader.getByRole("button", { name: `Design review (${day(7)} – ${day(11)})`, exact: true });
    await expect(readerBarA).toBeVisible();
    await expect(reader.getByLabel("Resize end date")).toHaveCount(0);

    let submittedByReader = false;
    reader.on("request", (request) => {
      if (request.url().endsWith("/v1/submissions")) submittedByReader = true;
    });
    const rBox = (await readerBarA.boundingBox())!;
    await reader.mouse.move(rBox.x + rBox.width / 2, rBox.y + rBox.height / 2);
    await reader.mouse.down();
    await reader.mouse.move(rBox.x + rBox.width / 2 + 72, rBox.y + rBox.height / 2, { steps: 6 });
    await reader.mouse.up();
    await expect(readerBarA).toBeVisible();
    expect(submittedByReader).toBe(false);
    await readerContext.close();
  });
});
