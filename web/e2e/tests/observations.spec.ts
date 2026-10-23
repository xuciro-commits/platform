import { once } from "node:events";
import { createServer } from "node:http";
import { expect, fresh, shots, slug, test } from "./kit";

test("a declared external observation reads original points and submits only its summary decision", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const requests: { device: string; signal: string; mode: string; since: string; until: string; limit: number }[] = [];
  let uncertain=false;
  const adapter = createServer(async (request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.method !== "POST") { response.end(JSON.stringify({ ok: true })); return; }
    let raw = "";
    for await (const part of request) raw += part;
    const read = JSON.parse(raw); requests.push(read);
    if(read.mode==="intervals"){
      const since=Date.parse(read.since),step=(Date.parse(read.until)-since)/4;
      response.end(JSON.stringify({intervals:{since:read.since,until:read.until,revision:uncertain?"collector-2":"collector-1",coverage:{availableSince:read.since,completeUntil:read.until,good:uncertain?3:4,uncertain:uncertain?1:0},points:["20","40","40","20"].map((value,index)=>({at:new Date(since+index*step).toISOString(),receivedAt:new Date(since+index*step).toISOString(),quality:uncertain&&index===1?"uncertain":"good",value}))}}));return;
    }
    const firstAt = new Date(Date.parse(read.since) + 1000).toISOString(), lastAt = new Date(Date.parse(read.until) - 1000).toISOString();
    response.end(JSON.stringify({ window: { since: read.since, until: read.until, unit: "C", count: 3, firstAt, lastAt, first: "20", last: "40", min: "20", max: "40", sum: "90", mean: "30" }, ...(read.mode === "points" ? { points: [{ at: firstAt, value: "20" }, { at: new Date((Date.parse(firstAt) + Date.parse(lastAt)) / 2).toISOString(), value: "30" }, { at: lastAt, value: "40" }] } : {}) }));
  });
  adapter.listen(0, "127.0.0.1"); await once(adapter, "listening");
  try {
    const address = adapter.address();
    if (!address || typeof address === "string") throw Error("Observation adapter did not start");
    const name = slug("observations"), connection = fresh("CONNECTION"), sourceID = fresh("OBSERVATION");
    const asset = await builder.object({
      title: "Observed equipment", fields: [{ name: "device", title: "External device", type: "text", required: true }, { name: "mean", title: "Window mean", type: "decimal" }],
      states: [{ name: "normal", title: "Normal" }, { name: "warning", title: "Warning" }],
      actions: [{ name: "warn", title: "Record observation warning", from: ["normal"], to: "warning", roles: ["user", "builder"], inputs: [{ name: "mean", title: "Window mean", type: "decimal", required: true }], sets: [{ field: "mean", from: "mean" }] }],
      access: [{ role: "user", read: "all", create: true, edit: true }],
    });
    const [record] = await builder.records(asset.type, [{ device: "device-A" }]);
    await builder.decide("build.connection.create", { type: "build.connection", id: connection }, { name: `${name}connection`, title: "External observation store", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
    await builder.decide("build.connection.check", { type: "build.connection", id: connection }, {});
    await expect.poll(async () => (await builder.record("build.connection", connection)).state,{timeout:20_000}).toBe("ready");
    const latest = { op: "", kind: "decimal", ref: "observation.latest" }, literal = { op: "", kind: "decimal", lit: { value: "30" } };
    await builder.decide("build.observation.create", { type: "build.observation", id: sourceID }, {
      name, title: "Equipment temperature", connection, object: asset.type, deviceField: "device", signal: "temperature", unit: "C", path: "/observations", windowSeconds: 300, expectedRetentionSeconds: 86400, expectedGranularitySeconds: 1, roles: ["user", "builder"],
      decision: { action: { app: "build", kind: "action", name: `${asset.type}.warn` }, when: { op: "and", kind: "boolean", args: [{ op: ">", kind: "boolean", args: [latest, literal] }, { op: ">", kind: "boolean", args: [{ op: "delta", kind: "decimal", window: { source: "observation" } }, { op: "", kind: "decimal", lit: { value: "10" } }] }] }, inputs: { mean: { op: "over-window", kind: "decimal", window: { source: "observation", aggregate: "mean" } } } },
    });
    await builder.open(page, `/observation-source?id=${sourceID}`);
    await page.getByRole("group",{name:"Editing mode",exact:true}).getByRole("button",{name:"Visual",exact:true}).click();
    await expect(page.getByRole("textbox", { name: "Observation title", exact: true })).toHaveValue("Equipment temperature");
    await page.getByRole("textbox", { name: "Observation title", exact: true }).fill("Published equipment temperature");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.observation", sourceID)).title).toBe("Published equipment temperature");
    await shots(page, page.locator("body"), "observation-source-editor", info);
    const candidate = await builder.candidate([{ kind: "observation", id: sourceID }]);
    await builder.activate(candidate);
    const declaration = (await builder.definitions<any>()).find(item => item.ref.kind === "observation" && item.ref.name === name);
    expect(declaration.requires).toEqual(expect.arrayContaining([{ app: "build", kind: "object", name: asset.type }, { app: "build", kind: "connection", name: `${name}connection` }, { app: "build", kind: "action", name: `${asset.type}.warn` }]));
    expect(JSON.stringify(declaration.observation)).not.toContain(`127.0.0.1:${address.port}`);

    await operator.open(page, `/observations?app=build&name=${name}&record=${record}`);
    await expect(page.getByRole("combobox", { name: "Published observation source", exact: true })).toContainText("Published equipment temperature");
    await page.getByRole("combobox", { name: "Observation read mode", exact: true }).selectOption("points");
    await page.getByRole("button", { name: "Read observation window", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Original observation points", exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: "40", exact: true })).toBeVisible();
    expect(requests.at(-1)).toMatchObject({ device: "device-A", signal: "temperature", mode: "points", limit: 500 });
    expect((await operator.record(asset.type, record!)).state).toBe("normal");
    const pointPanel = page.getByRole("region", { name: "Original observation points", exact: true });
    await expect(pointPanel.getByRole("img", { name: "temperature · C", exact: true }).locator("canvas")).toBeVisible();
    await shots(page, pointPanel, "observation-external-points", info);
    await page.getByRole("button", { name: "Evaluate declared decision", exact: true }).click();
    await expect(page.getByText("The declared condition matched. The original business action answered the request.", { exact: true })).toBeVisible();
    const changed = await operator.record(asset.type, record!);
    expect(changed.state).toBe("warning"); expect(String(changed.mean)).toBe("30");
    expect(requests.at(-1)).toMatchObject({ mode: "summary", device: "device-A" });
    expect(changed.points).toBeUndefined(); expect(changed.window).toBeUndefined();
    await shots(page, page.getByRole("region", { name: "External window summary", exact: true }), "observation-business-result", info);

    // The same source editor publishes a typed state condition. These reads
    // expose unknown time and boundaries, without submitting a business action.
    await builder.open(page,`/observation-source?id=${sourceID}`);
    await page.getByRole("group",{name:"Editing mode",exact:true}).first().getByRole("button",{name:"Visual",exact:true}).click();
    await page.getByRole("checkbox",{name:"Analyze a state condition over the source window",exact:true}).check();
    const intervalEditor=page.getByRole("region",{name:"State interval analysis",exact:true});
    await intervalEditor.getByRole("combobox",{name:"Sample value type",exact:true}).selectOption("decimal");
    await intervalEditor.getByLabel("Maximum sample gap (seconds)",{exact:true}).fill("90");
    await intervalEditor.getByRole("button",{name:"Expression source",exact:true}).click();
    const stateCondition={op:">",kind:"boolean",args:[{op:"",kind:"decimal",ref:"sample.value"},{op:"",kind:"decimal",lit:{value:"35"}}]};
    await intervalEditor.getByRole("textbox",{name:"Expression source",exact:true}).fill(JSON.stringify(stateCondition,null,2));
    await intervalEditor.getByRole("button",{name:"Apply checked expression",exact:true}).click();
    await page.getByRole("button",{name:"Save",exact:true}).click();
    await expect.poll(async()=>(await builder.record("build.observation",sourceID)).intervals?.when).toEqual(stateCondition);
    await builder.activate(await builder.candidate([{kind:"observation",id:sourceID}]));
    await operator.open(page,`/observations?app=build&name=${name}&record=${record}`);
    await expect(page.getByRole("combobox",{name:"Observation read mode",exact:true})).toHaveValue("intervals");
    await page.getByRole("button",{name:"Read observation window",exact:true}).click();
    const intervals=page.getByRole("region",{name:"State intervals",exact:true});
    await expect(intervals.getByText("This window has complete state evidence. Reading it does not accept a business result.",{exact:true})).toBeVisible();
    await expect(intervals.getByRole("cell",{name:"Observed transition",exact:true})).toHaveCount(2);
    uncertain=true;await page.getByRole("button",{name:"Read observation window",exact:true}).click();
    await expect(intervals.getByText("Unknown time remains. These intervals cannot prove a complete business result or a normal ending.",{exact:true})).toBeVisible();
    await expect(intervals.getByText(/Uncertain quality/)).toBeVisible();
    await expect(intervals.getByRole("cell",{name:"Boundary remains open",exact:true})).toBeVisible();
    expect((await operator.record(asset.type,record!)).revision).toBe(changed.revision);
    await shots(page,intervals,"observation-state-intervals",info);
  } finally { adapter.close(); await once(adapter, "close"); }
});
