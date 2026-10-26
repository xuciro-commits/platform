import { expect, fresh, shots, slug, test } from "./kit";

test("application release finds draft agent queries and an incomplete selection offers recovery", async ({ page, builder }, info) => {
  const review = page.getByRole("region", { name: "Release review", exact: true });
  const object = await builder.object({ title: "Opportunity", fields: [{ name: "title", title: "Title", type: "text" }] });
  const queryName = slug("openopportunities"), queryID = fresh("QUERY"), agentName = slug("salesassistant"), agentID = fresh("AGENT");
  await builder.decide("build.query.create", { type: "build.query", id: queryID }, { name: queryName, title: "Open opportunities", description: "Read opportunities", object: object.type, limit: 20 });
  await builder.decide("build.agent.create", { type: "build.agent", id: agentID }, { name: agentName, title: "Sales assistant", description: "Review opportunities", instructions: "Read the opportunities query", tools: [`query:${queryName}`] });
  const application = await builder.application({ title: "CRM release", resources: [{ app: "build", kind: "agent", name: agentName }] });

  await builder.open(page, `/release-review?kind=app&id=${application.id}&application=${application.id}`);
  await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(review.getByText("Joint candidate ready for review", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Joint selection", { exact: true }).getByText(queryID, { exact: true })).toBeVisible();
  expect((await builder.record("build.query", queryID)).state).toBe("draft");
  expect((await builder.record("build.agent", agentID)).state).toBe("draft");

  // A manually selected agent alone still fails the real owner check. Its
  // diagnostic resolves logical names to actual record IDs, then lets the
  // builder inspect the query and add it without publishing it separately.
  const route = `/release-review?kind=agent&id=${agentID}&application=${application.id}`;
  await builder.open(page, route);
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: `Agent ${agentName} needs query ${queryName}` })).toBeVisible();
  await shots(page, page.getByRole("group", { name: "Release problem", exact: true }), "agent-query-release-recovery", info);
  await page.getByRole("button", { name: "Open query", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Query name", exact: true })).toHaveValue(queryName);
  await builder.open(page, route);
  await page.getByRole("button", { name: "Include query in candidate", exact: true }).click();
  await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(review.getByText("Joint candidate ready for review", { exact: true })).toBeVisible();
  expect((await builder.record("build.query", queryID)).state).toBe("draft");
  expect((await builder.record("build.agent", agentID)).state).toBe("draft");
});
