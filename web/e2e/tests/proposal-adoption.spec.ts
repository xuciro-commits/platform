import { createServer } from "node:http";
import { once } from "node:events";
import { expect, fresh, pageUIProfile, shots, slug, test } from "./kit";

test("a builder assembles proposal review and a person adopts through the original approval and transaction", async ({ page, builder, operator }, info) => {
  test.setTimeout(150_000);
  let modelCalls = 0;
  let releaseModel!: () => void;
  const runtimeReply = new Promise<void>(resolve => { releaseModel = resolve; });
  const provider = createServer(async (request, response) => {
    for await (const _chunk of request) { /* Consume this local fixture request. */ }
    modelCalls++;
    if (modelCalls === 4) await runtimeReply;
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ verdict: "pass", confidence: 0.4 }) } }], usage: { prompt_tokens: 10, completion_tokens: 6, cost: 0.01 } }));
  });
  provider.listen(0, "127.0.0.1");
  await once(provider, "listening");
  try {
    const address = provider.address();
    if (!address || typeof address === "string") throw new Error("model fixture did not listen");
    const objectName = slug("inspection"), functionName = slug("classify"), functionID = fresh("FUNCTION");
    const object = await builder.object({ name: objectName, title: "Inspection review", fields: [
      { name: "temperature", title: "Temperature", type: "integer", sourceType: "record", aiInput: true },
      { name: "verdict", title: "Verdict", type: "choice", choices: "pass,hold", sourceType: "generated" },
    ], states: [{ name: "draft", title: "Draft" }], actions: [{ name: "adopt", title: "Confirm inspection", from: ["draft"],
      roles: ["user", "builder"], inputs: [{ name: "verdict", title: "Verdict", type: "choice", choices: "pass,hold", required: true }],
      transaction: { subject: `build.${objectName}`, steps: [{ name: "classify", kind: "update", target: { type: `build.${objectName}`, id: "$subject" }, changes: [{ field: "verdict", from: "verdict" }] }] },
    }] });
    const providerID = slug("proposalmodel"), model = `${providerID}/probe`;
    await builder.decide("ai.provider.add", { type: "ai.provider", id: providerID }, { kind: "local", baseUrl: `http://127.0.0.1:${address.port}`, wire: "openai" }, "ai");
    await builder.decide("ai.model.enable", { type: "ai.model", id: model }, { access: "users" }, "ai");
    await builder.decide("build.function.create", { type: "build.function", id: functionID }, {
      name: functionName, title: "Classify inspection", description: "A typed classification for review", object: object.type, fields: ["temperature"], instructions: "Classify the supplied temperature", model,
      output: [{ name: "verdict", type: "string", choices: ["pass", "hold"], required: true, description: "Proposed classification" }, { name: "confidence", type: "decimal", required: true, description: "Confidence" }],
      maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"],
    });
    await builder.open(page, `/function?id=${functionID}`);
    await page.getByRole("checkbox", { name: "Produce a business proposal", exact: true }).check();
    await page.getByRole("combobox", { name: "Handover role", exact: true }).selectOption("builder");
    await page.getByRole("combobox", { name: "Confidence output", exact: true }).selectOption("confidence");
    await page.getByRole("spinbutton", { name: "Minimum confidence", exact: true }).fill("0.7");
    await page.getByRole("button", { name: "Save function", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.function", functionID)).proposal?.minimumConfidence).toBe(0.7);
    await builder.decide("build.function.publish", { type: "build.function", id: functionID }, {});
    await builder.open(page, `/action-type?id=${object.id}&action=adopt`);
    await page.getByRole("region", { name: "Action structure", exact: true }).getByRole("button", { name: /^Transaction/ }).click();
    await page.getByRole("checkbox", { name: "Adopt an AI proposal", exact: true }).check();
    await page.getByRole("textbox", { name: "Proposal function name", exact: true }).fill(functionName);
    await page.getByRole("button", { name: "Add proposal mapping", exact: true }).click();
    await page.getByRole("textbox", { name: "Proposal output field", exact: true }).fill("verdict");
    await page.getByRole("combobox", { name: "Business action input", exact: true }).selectOption("verdict");
    await page.getByRole("button", { name: "Approval", exact: true }).click();
    await page.getByRole("checkbox", { name: "Wait for approval", exact: true }).check();
    await page.getByRole("combobox", { name: "Approver role", exact: true }).selectOption("builder");
    await page.getByRole("button", {name:"Save",exact:true}).click();
    await expect.poll(async () => (await builder.record("build.object", object.id)).actions[0].adoption?.function.name).toBe(functionName);
    await builder.decide("build.object.publish", { type: "build.object", id: object.id }, {});
    const advicePage = await builder.page({ title: "Inspection proposals", object: object.type, sections: [
      { id: "inspections", widget: "table", configVersion: 1, fields: ["temperature"] },
      { id: "classification", widget: "function", configVersion: 1, title: "Inspection advice", function: { name: functionName, version: 1 } },
    ], document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: {
      root: { kind: "rows", children: ["inspections", "classification"] },
      inspections: { kind: "widget", section: "inspections" }, classification: { kind: "widget", section: "classification" },
    } } });
    await builder.decide("build.page.publish", { type: "build.page", id: advicePage.id }, {});
    const application = await builder.application({ title: "Quality proposals", pages: [advicePage.name], resources: [{ app: "build", kind: "object", name: object.type }, { app: "build", kind: "function", name: functionName }] });
    await builder.decide("build.app.publish", { type: "build.app", id: application.id }, {});
    const candidate=await builder.candidate([{kind:"app",id:application.id}]),plan=fresh("PLAN");
    await builder.decide("build.testplan.create",{type:"build.testplan",id:plan},{name:slug("proposalquality"),title:"Proposal quality",function:functionID,model,at:new Date().toISOString(),steps:[{type:object.type,id:"SYNTHETIC",action:`${object.type}.create`,payload:JSON.stringify({temperature:24}),expect:"accepted"}],evaluation:[{minQuality:1,maxCostUsd:0.1,maxLatencyMillis:300000,cases:[{name:"classification",input:{temperature:24},expected:{verdict:"pass",confidence:0.4}}]}]});
    const evaluation=await builder.request.post("/v1/releases/evaluations",{headers:builder.headers,data:{candidateId:candidate,planId:plan,key:fresh("MEASURE")}});
    expect(evaluation.ok(),await evaluation.text()).toBe(true);
    const evaluationID=(await evaluation.json()).id;
    await expect.poll(async()=>(await builder.record("build.evaluation",evaluationID)).state,{timeout:20_000}).toBe("passed");
    await builder.activate(candidate);
    const sourceID = fresh("SOURCE"), otherSourceID = fresh("SOURCE");
    await operator.decide(`${object.type}.create`, {type:object.type,id:sourceID}, {temperature:24});
    await operator.decide(`${object.type}.create`, {type:object.type,id:otherSourceID}, {temperature:57});
    const before = await operator.record(object.type, sourceID!);
    await operator.open(page, `/page?app=build&kind=page&name=${advicePage.name}&application=build:${application.name}`);
    await expect(page.getByRole("heading", { name: "Inspection proposals", exact: true })).toBeVisible();
    const sourceRow = page.getByRole("row").filter({ has: page.getByRole("cell", { name: "24", exact: true }) });
    await sourceRow.click();
    await expect(page.getByRole("button", { name: "Request advice", exact: true })).toHaveCount(0);
    const requested = page.waitForRequest(request => request.method() === "POST" && request.url().endsWith("/v1/submissions") && request.postDataJSON().schema?.name === "build.function-call.start");
    await page.getByRole("button", { name: "Request original analysis", exact: true }).click();
    const submitted = (await requested).postDataJSON();
    const callID = submitted.target.id;
    expect(JSON.parse(Buffer.from(submitted.payload, "base64").toString())).toMatchObject({ app: "build", name: functionName, version: 1, source: sourceID });
    await expect(page.getByRole("status").filter({ hasText: "Waiting for the original model result." })).toBeVisible();
    releaseModel();
    await expect.poll(async () => (await operator.record("build.function-call", callID)).state, { timeout: 20_000 }).toBe("ready");
    expect(await operator.record(object.type, sourceID!)).toEqual(before);
    await expect(page.getByText("Original AI result", { exact: true })).toBeVisible();
    await expect(page.getByText("verdict: pass", { exact: true })).toBeVisible();
    await expect(page.getByText("confidence: 0.4", { exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "Proposal review", exact: true })).toBeVisible();
    const history = page.getByRole("region", { name: "Function call history", exact: true });
    await expect(history.getByRole("row").filter({ hasText: `${object.type}/${sourceID}` })).toBeVisible();
    await page.getByRole("row").filter({ has: page.getByRole("cell", { name: "57", exact: true }) }).click();
    await expect(page.getByText("Original AI result", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("region", { name: "Proposal review", exact: true })).toHaveCount(0);
    await expect(history.getByRole("row").filter({ hasText: `${object.type}/${sourceID}` })).toHaveCount(0);
    await sourceRow.click();
    await history.getByRole("row").filter({ hasText: `${object.type}/${sourceID}` }).click();
    await expect(page).toHaveURL(new RegExp(`[?&]id=${callID}(?:&|$)`));
    await expect(page.getByRole("heading", { name: functionName, exact: true })).toBeVisible();
    const measured = page.locator("section").filter({ has: page.getByRole("heading", { name: "Measured model call", exact: true }) }).last();
    await expect(measured.locator("dl > div").filter({ has: page.getByText("Input tokens", { exact: true }) }).locator("dd")).toHaveText("10");
    await expect(measured.locator("dl > div").filter({ has: page.getByText("Output tokens", { exact: true }) }).locator("dd")).toHaveText("6");
    await expect(measured).toContainText("$0.010000");
    const review = page.getByRole("region", { name: "Proposal review", exact: true });
    await expect(review).toBeVisible();
    await review.getByRole("combobox", { name: "Verdict *", exact: true }).selectOption("hold");
    await review.getByRole("button", { name: "Request adoption approval", exact: true }).click();
    await expect(review.getByRole("status")).toHaveText("The adoption is waiting in the original approval inbox.");
    expect(await operator.record(object.type, sourceID!)).toEqual(before);
    const approvals = await builder.read<any>("records/work.approval");
    const approval = approvals.records.find((record: any) => record.target === `${object.type}/${sourceID}` && record.state === "pending");
    expect(approval).toBeTruthy();
    await builder.decide("work.approval.approve", { type: "work.approval", id: approval.id }, {}, "work");
    const decidedApproval=await builder.record("work.approval",approval.id);
    expect(decidedApproval.state,decidedApproval.outcome).toBe("approved");
    await expect.poll(async () => (await operator.record("build.function-call", callID)).decision).toBe("adopted");
    const adopted = await operator.record("build.function-call", callID);
    expect(adopted.amendments).toEqual({ verdict: "hold" });
    expect((await operator.record(object.type, sourceID!)).verdict).toBe("hold");
    expect(modelCalls).toBe(4);
    await operator.open(page, `/record?type=build.function-call&id=${callID}`);
    await expect(page.getByRole("region", { name: "Proposal review", exact: true }).getByText("adopted", { exact: true })).toBeVisible();
    await expect(page.getByRole("region",{name:"Proposal review",exact:true}).getByRole("status")).toHaveCount(0);
    await shots(page, page.getByRole("region", { name: "Proposal review", exact: true }), "adopted-proposal", info);
    await builder.open(page, `/project?id=${application.id}&area=verify`);
    await page.getByText("AI placement and evaluations", { exact: true }).click();
    await expect(page.getByText("Business proposal", { exact: false }).first()).toBeVisible();
    await expect(page.getByText(/1 calls · 10 input tokens · 6 output tokens/)).toBeVisible();
  } finally {
    releaseModel();
    provider.closeAllConnections();
    await new Promise<void>((resolve, reject) => provider.close(error => error ? reject(error) : resolve()));
  }
});

test("an application agent needs a passing candidate evaluation before activation", async ({page,builder},info) => {
  let calls=0;
  const provider=createServer(async(request,response)=>{
    for await(const _chunk of request) { /* Consume this local fixture request. */ }
    calls++;
    response.setHeader("Content-Type","application/json");
    response.end(JSON.stringify({choices:[{message:{content:"Ready"}}],usage:{prompt_tokens:3,completion_tokens:2,cost:0.01}}));
  });
  provider.listen(0,"127.0.0.1");
  await once(provider,"listening");
  try {
    const address=provider.address();
    if(!address||typeof address==="string")throw Error("model fixture did not listen");
    const object=await builder.object({title:"Agent source",fields:[{name:"note",title:"Note",type:"text"}]}),name=slug("assistant"),id=fresh("AGENT"),providerID=slug("agentmodel"),model=`${providerID}/probe`;
    await builder.decide("ai.provider.add",{type:"ai.provider",id:providerID},{kind:"local",baseUrl:`http://127.0.0.1:${address.port}`,wire:"openai"},"ai");
    await builder.decide("ai.model.enable",{type:"ai.model",id:model},{access:"users"},"ai");
    await builder.decide("platform.setting.set",{type:"platform.setting",id:"agent/model"},{value:model},"platform");
    await builder.decide("build.agent.create",{type:"build.agent",id},{name,title:"Candidate assistant",description:"Review cases",instructions:"Report Ready",tools:[`${object.type}.edit`],cases:[{name:"result",goal:"Give a result",expect:"Ready"}]});
    const application=await builder.application({title:"Measured agents",resources:[{app:"build",kind:"agent",name}]}),candidate=await builder.candidate([{kind:"agent",id},{kind:"app",id:application.id}]);
    await builder.open(page,"/release-review");
    await page.getByRole("combobox",{name:"Saved candidate",exact:true}).selectOption(candidate);
    await expect(page.getByRole("button",{name:"Activate release",exact:true})).toBeDisabled();
    await page.getByRole("button",{name:"Run declared cases",exact:true}).click();
    await expect.poll(()=>calls,{timeout:20_000}).toBe(3);
    await expect.poll(async()=>{
      const response=await builder.request.get(`/v1/releases/candidates/${candidate}`,{headers:builder.headers});
      const review=await response.json();
      return review.ai?.find((item:any)=>item.resource.ref.name===name)?.evaluations.some((report:any)=>report.state==="passed");
    },{timeout:20_000}).toBe(true);
    await page.getByRole("button",{name:"Refresh evaluation reports",exact:true}).click();
    await expect(page.getByRole("button",{name:"Activate release",exact:true})).toBeEnabled();
    await page.getByRole("button",{name:"Activate release",exact:true}).click();
    await expect(page.getByText("Release active for operators.",{exact:false})).toBeVisible();
    await shots(page,page.getByRole("region",{name:"Agent candidate evaluations",exact:true}),"agent-candidate-evaluation",info);
  } finally {
    provider.closeAllConnections();
    await new Promise<void>((resolve,reject)=>provider.close(error=>error?reject(error):resolve()));
  }
});
