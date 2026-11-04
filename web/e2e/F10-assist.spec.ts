import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F10 an assistant reads only authorized context and writes only after the member confirms its action", async ({ page }) => {
  test.setTimeout(300_000);
  const tag = unique("assist");
  const typeName = tag.toLowerCase();
  const project = unique("assistproject");
  const agentName = `${typeName}agent`;
  const agentID = `build.${agentName}`;
  const action = `build.${typeName}.approve`;
  const actionTool = action.replaceAll(".", "_").replaceAll("#", "__").replaceAll("/", "_").replaceAll(":", "_").replaceAll("-", "_");
  const record = unique("RECORD");
  const secretTypeName = `${typeName}secret`;
  const secret = `PROTECTED-${unique("VALUE")}`;
  const requests: string[] = [];
  const provider = "f10-mock";
  const aiPort = Number(process.env.PLATFORM_E2E_AI_PORT ?? Number(process.env.PLATFORM_E2E_PORT ?? 18496) + 1);

  const server = createServer((request, response) => {
    let body = "";
    request.setEncoding("utf8");
    request.on("data", chunk => { body += chunk; });
    request.on("end", () => {
      if (request.url === "/v1/models") {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(JSON.stringify({ data: [{ id: "review" }] }));
        return;
      }
      if (request.url !== "/v1/chat/completions") {
        response.writeHead(404);
        response.end();
        return;
      }
      requests.push(body);
      const input = JSON.parse(body) as { messages?: { tool_calls?: { function?: { name?: string } }[] }[] };
      const called = (input.messages ?? []).flatMap(message => message.tool_calls ?? []).map(call => call.function?.name ?? "");
      const call = !called.includes("search")
        ? { name: "search", arguments: JSON.stringify({ query: "protected", rationale: "Check the protected source before acting" }) }
        : !called.includes(actionTool)
          ? { name: actionTool, arguments: JSON.stringify({ target: record, rationale: "The visible record can be approved", payload: {} }) }
          : { name: "finish", arguments: JSON.stringify({ result: "The record was approved after confirmation.", rationale: "The confirmed action completed" }) };
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({
        id: unique("chat"), object: "chat.completion", created: Math.floor(Date.now() / 1000), model: "review",
        choices: [{ index: 0, finish_reason: "tool_calls", message: {
          role: "assistant", content: null,
          tool_calls: [{ id: unique("call"), type: "function", function: { name: call.name, arguments: call.arguments } }],
        } }],
        usage: { prompt_tokens: 20, completion_tokens: 8, total_tokens: 28 },
      }));
    });
  });
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(aiPort, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;

  try {
    const admin = acceptedClient(page);
    await admin.submit("build.app", project, "create", { name: project, title: "Assist project" });
    const objectID = unique("OBJECT"), secretObjectID = unique("SECRET");
    await admin.submit("build.object", objectID, "create", {
      name: typeName, title: `Assist ${tag}`,
      project,
      fields: [{ name: "title", title: "Title", type: "text", required: true }],
      access: [{ role: "user", read: "all" }],
      states: [{ name: "open", title: "Open" }, { name: "approved", title: "Approved" }],
    });
    await admin.submit("build.actiontype", action, "create", {
      name: action, object: `build.${typeName}`, project,
      definition: { name: "approve", title: "Approve", from: ["open"], to: "approved" },
    });
    await admin.publish([{ kind: "object", id: objectID }]);
    await admin.submit(`build.${typeName}`, record, "create", { title: `Visible ${tag}` });

    await admin.submit("build.object", secretObjectID, "create", {
      name: secretTypeName, title: `Secret ${tag}`,
      project,
      fields: [{ name: "value", title: "Value", type: "text", required: true }],
      access: [], states: [{ name: "open", title: "Open" }],
    });
    await admin.publish([{ kind: "object", id: secretObjectID }]);
    await admin.submit(`build.${secretTypeName}`, unique("SECRET"), "create", { value: secret });

    await admin.submit("ai.provider", provider, "add", { kind: "local", baseUrl: `http://127.0.0.1:${address.port}/v1` }, 0);
    await admin.submit("ai.model", `${provider}/review`, "enable", { access: "everyone" }, 0);
    await admin.submit("build.agent", agentID, "create", {
      name: agentName, title: `Assistant ${tag}`, description: "Review one record and propose the declared approval.",
      instructions: "Search only when needed. Propose the approval only for the visible record. Never claim access to protected data.",
      model: `${provider}/review`, tools: [action], steps: 6, actions: 1, handoffRole: "user", project,
    });
    await admin.submit("build.agent", agentID, "publish", {});

    await signIn(page, "user", `/objects/${encodeURIComponent(`build.${typeName}`)}/${record}`);
    await expect.poll(() => new URL(page.url()).hash).toContain(`build.${typeName}`);
    await page.getByRole("button", { name: "AIP Assist", exact: true }).click();
    await expect(page.getByText("Current page context", { exact: true })).toBeVisible();
    await expect(page.getByText(`build.${typeName}/${record}`, { exact: true })).toBeVisible();
    await page.getByRole("combobox", { name: "Agent", exact: true }).selectOption(agentID);
    await page.getByRole("textbox", { name: "Goal", exact: true }).fill(`Approve ${record} after checking its context.`);
    const started = page.waitForResponse(response =>
      response.url().endsWith("/v1/submissions")
      && response.request().method() === "POST"
      && response.request().postDataJSON().schema.name === "agent.run.start",
    );
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    const startResponse = await started;
    expect(startResponse.ok(), await startResponse.text()).toBeTruthy();
    const runID = startResponse.request().postDataJSON().target.id as string;
    await expect(page.getByRole("button", { name: "Confirm", exact: true })).toBeVisible({ timeout: 30_000 });
    expect((await admin.read<{ record: { state: string } }>(`/v1/records/build.${typeName}/${record}`)).record.state).toBe("open");
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect.poll(
      async () => (await admin.read<{ record: { state: string } }>(`/v1/records/build.${typeName}/${record}`)).record.state,
      { timeout: 30_000 },
    ).toBe("approved");
    await expect.poll(async () => {
      const view = await acceptedClient(page, "user").read<{ values: { state: string; result: string } }>(`/v1/records/agent.run/${runID}`);
      return `${view.values.state}:${view.values.result}`;
    }, { timeout: 30_000 }).toBe("done:The record was approved after confirmation.");
    expect(requests.join("\n")).not.toContain(secret);
  } finally {
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
