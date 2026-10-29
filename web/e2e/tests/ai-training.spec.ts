import { test, expect, slug, fresh, shots } from "./kit";

test("a confirmed agent proposal becomes a training candidate that stays unqualified until a person reviews it", async ({ page, builder }, info) => {
  test.setTimeout(90_000);
  const object = await builder.object({ name: slug("trainable"), title: "Trainable record", fields: [{ name: "temperature", title: "Temperature", type: "integer", sourceType: "record" }] });
  const policy = slug("policy"), run = fresh("PROPOSAL"), target = fresh("RECORD");
  const action = `${object.type}.create`;

  await builder.open(page, "/ai-configuration?section=training");
  await expect(page.getByText("No training candidates yet", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "Policy ID", exact: true }).fill(policy);
  await page.getByRole("textbox", { name: "Allowed fields", exact: true }).fill("temperature");
  await page.getByRole("checkbox", { name: "Enable this policy" }).check();
  await page.getByRole("checkbox", { name: "Data is permitted for training" }).check();
  await page.getByRole("checkbox", { name: "Provider terms permit training use" }).check();
  await page.getByRole("button", { name: "Save training policy", exact: true }).click();
  await expect.poll(async () => (await builder.read<{ policies: { id: string }[] }>("ai-training")).policies.some(p => p.id === policy)).toBe(true);

  // Saving the proposal changes nothing in the business; the person's confirmation does.
  await builder.decide("agent.tool.propose", { type: "agent.run", id: run }, { owner: "build", action, target, payload: { temperature: 21 } }, "agent");
  expect((await builder.read<{ records: { id: string }[] }>(`records/${object.type}`)).records.some(r => r.id === target)).toBe(false);
  await builder.decide("agent.run.confirm", { type: "agent.run", id: run }, {}, "agent");
  await expect.poll(async () => (await builder.read<{ records: { id: string }[] }>(`records/${object.type}`)).records.some(r => r.id === target)).toBe(true);

  // The accepted decision is candidate evidence only: no quality proof, so nothing is eligible to download.
  await expect.poll(async () => (await builder.read<{ candidates: { outcome: string }[] }>("ai-training")).candidates.some(c => c.outcome === "accepted"), { timeout: 30_000 }).toBe(true);
  await builder.open(page, "/ai-configuration?section=training");
  const row = page.getByRole("row").filter({ hasText: action }).filter({ has: page.getByRole("cell", { name: "accepted", exact: true }) });
  await expect(row).toBeVisible();
  await expect(row.getByText("Qualified training sample", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Eligible samples: 0", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Download training dataset", exact: true })).toBeDisabled();
  await shots(page, page.locator("body"), "ai-training-unqualified", info);

  // An independent review with recorded evidence is what qualifies it.
  await row.getByRole("button", { name: "Review", exact: true }).click();
  const review = page.locator("section").filter({ has: page.getByRole("heading", { name: "Independent quality review", exact: true }) });
  await review.getByRole("textbox", { name: "Quality evidence" }).fill("Checked against the inspection procedure");
  await review.getByRole("button", { name: "Approve training qualification", exact: true }).click();
  await expect(row.getByText("Qualified training sample", { exact: true })).toBeVisible();
  await expect(page.getByText("Eligible samples: 1", { exact: true })).toBeVisible();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download training dataset", exact: true }).click();
  expect((await download).suggestedFilename()).toMatch(/\.jsonl$/);
});
