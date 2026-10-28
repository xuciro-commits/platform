import { createServer } from "node:http";
import { once } from "node:events";
import { test, expect, slug, shots } from "./kit";

test("configure a service, select purpose defaults, send an explicit trial and recover a disabled model", async ({ page, builder }, info) => {
  test.setTimeout(120_000);
  const name = slug("models");
  const calls: Record<string, any>[] = [];
  const service = createServer(async (request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.url?.startsWith("/v1/models")) { response.end(JSON.stringify({ data: [{ id: "text", context_window: 32768, max_output_tokens: 2048 }, { id: "other" }] })); return; }
    let text = ""; for await (const chunk of request) text += chunk;
    const body = JSON.parse(text); calls.push(body);
    response.setHeader("Content-Type", "text/event-stream");
    const usage = calls.length === 1 ? 'data: {"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":2}}\n\n' : "";
    response.end('data: {"choices":[{"delta":{"content":"Trial completed"},"finish_reason":"stop"}]}\n\n' + usage + 'data: [DONE]\n\n');
  });
  service.listen(0, "127.0.0.1"); await once(service, "listening");
  try {
    const address = service.address(); if (!address || typeof address === "string") throw Error("Model fixture did not listen");
    await builder.open(page, "/ai-configuration");
    await page.getByRole("button", { name: "Connect services", exact: true }).click();
    await page.getByRole("button", { name: "Add provider", exact: true }).click();
    const provider = page.getByRole("dialog", { name: "Add AI provider", exact: true });
    await provider.getByRole("combobox", { name: "Kind", exact: true }).selectOption("local");
    await provider.getByRole("textbox", { name: "ID", exact: true }).fill(name);
    await provider.getByRole("textbox", { name: "Base URL", exact: true }).fill(`http://127.0.0.1:${address.port}/v1`);
    await provider.getByRole("button", { name: "Add", exact: true }).click();
    await expect(provider).not.toBeVisible();
    const card = page.locator('[data-ui="card"]').filter({ has: page.locator("span.font-semibold").filter({ hasText: name }) });
    await card.getByRole("button", { name: "Check connection", exact: true }).click();
    await expect(card.getByRole("status")).toHaveText("Connection verified");
    await card.getByRole("button", { name: "Models", exact: true }).click();
    await expect(page.getByRole("row").filter({ hasText: "other" })).toBeVisible();
    await page.getByRole("row").filter({ hasText: "text" }).getByRole("button", { name: "AI members only", exact: true }).click();
    await expect.poll(async () => (await builder.read<any[]>("ai-models")).some(model => model.provider === name && model.model === "text")).toBe(true);
    // Adopt a declared synthetic DeepSeek capability sample through the same
    // administrator action. The local fixture exercises the actual wire;
    // this is not represented as live vendor discovery or paid validation.
    await builder.decide("ai.model.enable", { type: "ai.model", id: `${name}/text` }, {
      access: "users", capabilities: { source: "synthetic DeepSeek protocol fixture", observedAt: new Date().toISOString(), purposes: ["generation", "agent"], contextTokens: 32768, outputTokens: 2048, adapter: "deepseek", thinkingModes: ["enabled", "disabled"], effortLevels: ["high", "max"] },
    }, "ai");
    await page.getByRole("button", { name: "Available models", exact: true }).click();
    await page.getByRole("button", { name: "Details and defaults", exact: true }).click();
    const details = page.getByRole("dialog", { name: "text", exact: true });
    await details.getByText("Advanced model options", { exact: true }).click();
    await details.getByLabel("Input budget (tokens)", { exact: true }).fill("4000");
    await details.getByLabel("Generation budget (tokens)", { exact: true }).fill("512");
    await details.getByRole("combobox",{name:"Thinking mode",exact:true}).selectOption("enabled");
    await details.getByRole("combobox",{name:"Thinking level",exact:true}).selectOption("high");
    await details.getByRole("button", { name: "Save configuration", exact: true }).click();
    await expect(details).not.toBeVisible();
    await page.getByRole("button", { name: "Default purposes", exact: true }).click();
    const generation = page.locator("section").filter({ has: page.getByRole("heading", { name: "Application generation", exact: true }) });
    await generation.getByRole("combobox", { name: "Application generation", exact: true }).selectOption(`${name}/text`);
    await generation.getByRole("button", { name: "Save default", exact: true }).click();
    await expect(generation.getByRole("status")).toHaveText("Configuration saved");
    const agents = page.locator("section").filter({ has: page.getByRole("heading", { name: "Agent tasks", exact: true }) });
    await agents.getByRole("combobox", { name: "Agent tasks", exact: true }).selectOption("@generation");
    await agents.getByRole("button", { name: "Save default", exact: true }).click();
    expect(calls).toHaveLength(0);
    await shots(page, page.locator("body"), "ai-default-purposes", info);
    const packageID=slug("modelpackage"),app=slug("modelapp"),agent=slug("reviewer");
    const descriptor={id:packageID,version:"1.0.0",namespace:packageID,title:"Model package task",blueprint:{roots:[{app:"build",kind:"app",name:app}],assets:[
      {ref:{app:"build",kind:"agent",name:agent},contractVersion:1,sourceVersion:"1.agent-1",body:{name:agent,title:"Package reviewer",description:"Review supplied input",instructions:"Review the supplied input",tools:[],budget:{Steps:1,Tokens:2000,Actions:1},cases:[]}},
      {ref:{app:"build",kind:"app",name:app},contractVersion:1,sourceVersion:"1",body:{name:app,title:"Model package application",pages:[],resources:[{app:"build",kind:"agent",name:agent}]}}
    ]}};
    await builder.open(page,"/apps");
    await page.getByRole("button",{name:"Add package",exact:true}).click();
    await page.getByLabel("Package source",{exact:true}).selectOption("file");
    await page.getByLabel("Package file",{exact:true}).setInputFiles({name:"model-package.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify(descriptor))});
    await page.getByRole("button",{name:"Review package",exact:true}).click();
    await page.getByRole("button",{name:"Add reviewed version to library",exact:true}).click();
    await page.getByRole("region",{name:"Package library",exact:true}).getByRole("button").filter({hasText:packageID}).click();
    await page.getByRole("combobox",{name:"Exact package version",exact:true}).selectOption("1.0.0");
    await page.getByRole("button",{name:"Prepare applications",exact:true}).click();
    await expect(page.getByText("Model available",{exact:true})).toBeVisible();
    await expect(page.getByText(`${agent} · ${name}/text`,{exact:true})).toBeVisible();
    await page.getByRole("button",{name:"Manage AI configuration",exact:true}).click();
    await page.getByRole("button",{name:"Return to package preparation",exact:true}).click();
    await expect(page.locator("section").filter({has:page.getByRole("heading",{name:"Install and create drafts",exact:true})}).getByText("Model package application",{exact:true})).toBeVisible();
    expect(calls).toHaveLength(0);
    await builder.open(page, `/ai-playground?model=${encodeURIComponent(`${name}/text`)}`);
    await page.getByRole("textbox", { name: "Prompt", exact: true }).fill("A synthetic trial without business data");
    await page.getByRole("button", { name: "Send trial · may incur charges", exact: true }).click();
    await expect(page.getByText("Trial completed", { exact: true })).toBeVisible();
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ model: "text", max_tokens: 512, thinking: { type: "enabled" }, reasoning_effort: "high" });
    await page.getByRole("button", { name: "Send trial · may incur charges", exact: true }).click();
    await expect.poll(() => calls.length).toBe(2);
    await builder.open(page, "/ai-usage");
    const unsettled = page.getByRole("region", { name: "Unsettled model calls", exact: true });
    await expect(unsettled).toBeVisible();
    await expect(unsettled).toContainText("Usage not reported");
    await expect(unsettled).toContainText(`${name}/text`);
    await shots(page, unsettled, "ai-unsettled-usage", info);
    await builder.open(page, "/ai-configuration?section=models");
    await page.getByRole("button", { name: "Disable", exact: true }).click();
    const removal = page.getByRole("dialog", { name: "Disable model", exact: true });
    await expect(removal).toContainText("Application generation");
    await expect(removal).toContainText("Agent tasks");
    await removal.getByRole("button", { name: "Disable model", exact: true }).click();
    await expect(removal).not.toBeVisible();
    await page.getByRole("button", { name: "Default purposes", exact: true }).click();
    await expect(generation.getByRole("combobox", { name: "Application generation", exact: true })).toHaveValue(`${name}/text`);
    await expect(generation.getByRole("alert")).toContainText("selected model is unavailable");
    await builder.decide("ai.model.enable", { type: "ai.model", id: `${name}/text` }, { access: "users" }, "ai");
    await page.getByRole("button", { name: "Available models", exact: true }).click();
    await page.getByRole("button", { name: "Default purposes", exact: true }).click();
    await expect(generation.getByRole("alert")).not.toBeVisible();
    const me = await builder.read<any>("me");
    await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "zh-CN" }, "platform");
    await page.evaluate(() => { localStorage.setItem("platform.language", "zh-CN"); localStorage.setItem("platform.appearance", JSON.stringify({ scheme: "dark" })); });
    await page.reload();
    await expect(page.getByRole("heading", { name: "AI 配置", exact: true })).toBeVisible();
    await shots(page, page.locator("body"), "ai-configuration-zh-dark", info);
    await builder.open(page, "/ai-usage");
    await expect(page.getByRole("region", { name: "未结算模型调用", exact: true })).toContainText("提供方未报告用量");
  } finally {
    service.closeAllConnections(); service.close();
    const me = await builder.read<any>("me").catch(()=>undefined);
    if(me) await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "en" }, "platform").catch(()=>undefined);
  }
});
