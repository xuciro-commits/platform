import {expect,fresh,shots,slug,test} from "./kit";

test("business refusals preserve inputs, repeat one intent and expose flagged and routed corrections",async({page,builder,operator},info)=>{
  test.setTimeout(120_000);
  const name=slug("disposition"),type=`build.${name}`;
  const object=await builder.object({name,title:"Business disposition",fields:[
    {name:"note",title:"Note",type:"text"},
    {name:"done",title:"Done",type:"boolean"},
    {name:"number",title:"Number",type:"text"},
  ],states:[{name:"open",title:"Open"}],access:[{role:"user",read:"all",create:true,edit:true}]});
  const disposition=await builder.object({title:"Disposition evidence",fields:[
    {name:"subject",title:"Original record",type:"reference",ref:type,required:true},
    {name:"reason",title:"Reason",type:"text",required:true},
  ],access:[{role:"user",read:"all",create:true}]});
  const inputs=[{name:"label",title:"Decision label",type:"text",required:true}];
  const when=(label:string)=>({op:"=",kind:"boolean",args:[{op:"",kind:"text",ref:"input.label"},{op:"",kind:"text",lit:{value:label}}]});
  const reason={message:"The selected resource needs correction",translations:{"zh-CN":"所选资源需要纠正"}};
  const actions=[{
    name:"apply",title:"Apply disposition",from:["open"],roles:["user","builder"],inputs,
    transaction:{subject:type,refusals:[
      {when:when("block"),level:"block",code:"blocked",...reason},
      {when:when("flag"),level:"flag",code:"concession",...reason,record:{name:"concession",kind:"create",target:{type:disposition.type},changes:[{field:"subject",from:"$subject"},{field:"reason",from:"=Concession approved"}]}},
      {when:when("route"),level:"route",code:"rework",...reason,route:{type,id:"$subject",action:"rework"}},
    ],steps:[
      {name:"number",kind:"sequence",sequence:{name:slug("numbers"),pattern:"R-{n:3}"}},
      {name:"finish",kind:"update",target:{type,id:"$subject"},changes:[{field:"note",from:"label"},{field:"done",from:"=true"},{field:"number",from:"step.number"}]},
    ]},
  },{
    name:"rework",title:"Rework disposition",from:["open"],roles:["user","builder"],inputs,
    transaction:{subject:type,steps:[
      {name:"hold",kind:"update",target:{type,id:"$subject"},changes:[{field:"note",from:"=Rework required"},{field:"done",from:"=false"}]},
      {name:"rework",kind:"create",target:{type:disposition.type},changes:[{field:"subject",from:"$subject"},{field:"reason",from:"=Rework route"}]},
    ]},
  }];
  await builder.decide("build.object.edit",{type:"build.object",id:object.id},{actions});
  await builder.open(page,`/action-type?id=${object.id}&action=apply`);
  await page.getByRole("button",{name:/^Transaction(?: \d+)?$/}).first().click();
  await expect(page.getByText("Business refusal exits",{exact:true})).toBeVisible();
  await page.getByRole("textbox",{name:"Stable refusal code",exact:true}).first().fill("capacityblocked");
  await page.getByRole("button",{name:"Save",exact:true}).click();
  await expect.poll(async()=>(await builder.record("build.object",object.id)).actions[0].transaction.refusals[0].code).toBe("capacityblocked");
  await builder.decide("build.object.publish",{type:"build.object",id:object.id},{});
  const application=await builder.application({title:"Disposition operations",resources:[{app:"build",kind:"object",name:type},{app:"build",kind:"object",name:disposition.type}]});
  await builder.decide("build.app.publish",{type:"build.app",id:application.id},{});
  const ids=[fresh("BLOCK"),fresh("FLAG"),fresh("ROUTE")];
  for(const id of ids)await operator.decide(`${type}.create`,{type,id},{note:"Original request"});

  const intents:{body:any;correction?:string}[]=[];
  page.on("request",request=>{
    if(request.method()!=="POST"||!request.url().endsWith("/v1/submissions"))return;
    const body=request.postDataJSON();
    if(body.schema?.name===`${type}.apply`)intents.push({body,correction:request.headers()["platform-correction-of"]});
  });
  await operator.open(page,`/record?type=${type}&id=${ids[0]}`);
  await page.getByRole("button",{name:"Apply disposition",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:`Apply disposition ${ids[0]}`,exact:true});
  const label=dialog.getByRole("textbox",{name:/^Decision label/});
  await label.fill("block");
  await dialog.getByRole("button",{name:"Apply disposition",exact:true}).click();
  await expect(dialog.getByText("Business refused",{exact:true})).toBeVisible();
  await expect(label).toHaveValue("block");
  await expect(dialog.getByText("capacityblocked",{exact:true})).toBeVisible();
  const original=await operator.record(type,ids[0]!);
  expect(original.note).toBe("Original request");
  expect(original.number??"").toBe("");
  await dialog.getByRole("button",{name:"Retry original inputs",exact:true}).click();
  await expect.poll(()=>intents.length).toBe(2);
  expect(intents[1]!.body).toEqual(intents[0]!.body);
  let evidence=await operator.read<any>(`business-refusals?type=${type}&id=${ids[0]}`);
  expect(evidence.total).toBe(1);
  await label.fill("accepted");
  await dialog.getByText("Original refused inputs",{exact:true}).click();
  await expect(dialog.locator("pre")).toContainText('"block"');
  await shots(page,dialog,"business-refusal-correction",info);
  await dialog.getByRole("button",{name:"Submit corrected inputs",exact:true}).click();
  await expect(dialog).toHaveCount(0);
  expect(intents[2]!.body.idempotencyKey).not.toBe(intents[0]!.body.idempotencyKey);
  expect(intents[2]!.correction).toBe(intents[0]!.body.idempotencyKey);
  evidence=await operator.read<any>(`business-refusals?type=${type}&id=${ids[0]}`);
  expect(evidence.total).toBe(2);
  expect(evidence.records[0].correctionOf).toBe(intents[0]!.body.idempotencyKey);
  expect(evidence.records[0].changes).toEqual([{field:"label",before:"block",after:"accepted"}]);
  expect((await operator.record(type,ids[0]!)).number).toBe("R-001");
  await expect(page.getByText("Correction accepted",{exact:true})).toBeVisible();

  await operator.decide(`${type}.apply`,{type,id:ids[1]!},{label:"flag"});
  await operator.open(page,`/record?type=${type}&id=${ids[1]}`);
  await expect(page.getByText("Accepted with a flag",{exact:true})).toBeVisible();
  await expect(page.getByRole("button",{name:"Open refusal record",exact:true})).toBeVisible();
  expect((await operator.record(type,ids[1]!)).done).toBe(true);
  await shots(page,page.locator("body"),"business-refusal-flag",info);

  await operator.decide(`${type}.apply`,{type,id:ids[2]!},{label:"route"});
  await operator.open(page,`/record?type=${type}&id=${ids[2]}`);
  await expect(page.getByText("Routed for correction",{exact:true})).toBeVisible();
  const routed=await operator.record(type,ids[2]!);
  expect(routed.note).toBe("Rework required");
  expect(routed.number??"").toBe("");
  expect(routed.done).toBe(false);
  await shots(page,page.locator("body"),"business-refusal-route",info);

  await builder.open(page,`/application-runs?app=build&name=${application.name}&application=build:${application.name}`);
  await page.getByText("Refusal codes by object type",{exact:true}).click();
  await expect(page.getByRole("row").filter({hasText:"capacityblocked"})).toContainText("Business refused");
  await shots(page,page.locator("body"),"business-refusal-statistics",info);
});
