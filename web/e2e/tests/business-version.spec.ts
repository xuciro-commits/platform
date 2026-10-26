import { expect, runtime, slug, test } from "./kit";

test("builder configures business versions and old orders retain their selected version", async ({page,builder,operator,editor}) => {
  const access=[{role:"user",read:"all",create:true,edit:true,archive:true}];
  const routing=await builder.object({name:slug("routing"),title:"Business route",fields:[
    {name:"code",title:"Code",type:"text",required:true},
    {name:"version",title:"Business version",type:"integer",required:true},
    {name:"from",title:"Valid from",type:"date",required:true},
    {name:"until",title:"Valid until",type:"date"},
    {name:"name",title:"Name",type:"text",required:true,search:true},
  ],access});
  await builder.open(page,`/object-type?id=${routing.id}&tab=versions`);
  await page.getByRole("checkbox",{name:"Retain business versions as ordinary records",exact:true}).check();
  await page.getByRole("combobox",{name:"Business key",exact:true}).selectOption("code");
  await page.getByRole("combobox",{name:"Business version",exact:true}).selectOption("version");
  await page.getByRole("combobox",{name:"Valid from field",exact:true}).selectOption("from");
  await page.getByRole("combobox",{name:"Valid until field",exact:true}).selectOption("until");
  await page.getByRole("combobox",{name:"Pin policy",exact:true}).selectOption("on_create");
  await editor.release();
  await expect.poll(async()=>JSON.parse((await builder.record("build.object",routing.id)).published).versioning?.pin).toBe("on_create");

  const master=await runtime(page,operator,routing.name);
  for(const version of [1,2]) {
    await master.getByRole("button",{name:"Create business route",exact:true}).click();
    const form=master.getByRole("dialog",{name:"Create business route",exact:true});
    await form.getByRole("textbox",{name:/^Code/}).fill("R");
    await form.getByLabel(/^Business version/).fill(String(version));
    await form.getByLabel(/^Valid from/).fill("2020-01-01");
    await form.getByRole("textbox",{name:/^Name/}).fill(`Route ${version}`);
    await form.getByRole("button",{name:"Create",exact:true}).click();
    await expect(form).toBeHidden();
    await expect(master.getByRole("cell",{name:"R",exact:true})).toHaveCount(version);
  }
  const rows=await (await builder.request.get(`/v1/records/${routing.type}`,{headers:builder.headers})).json();
  const first=rows.records.find((row:any)=>row.version===1);
  const order=await builder.object({name:slug("order"),title:"Versioned order",fields:[
    {name:"name",title:"Name",type:"text",required:true,search:true},
    {name:"routing",title:"Routing",type:"reference",ref:routing.type,required:true},
    {name:"routingversion",title:"Pinned routing version",type:"integer"},
  ],access});
  await builder.open(page,`/object-type?id=${order.id}&tab=versions`);
  await page.getByRole("button",{name:"Add a version pin",exact:true}).click();
  await page.getByRole("combobox",{name:"Versioned reference",exact:true}).selectOption("routing");
  await page.getByRole("combobox",{name:"Pinned version field",exact:true}).selectOption("routingversion");
  await editor.release();
  const [oldID]=await builder.records(order.type,[{name:"Old order",routing:first.id}]);
  const [newRoute]=await builder.records(routing.type,[{code:"R",version:3,from:"2020-01-01",name:"Route 3"}]);
  const [newID]=await builder.records(order.type,[{name:"New order",routing:newRoute}]);
  expect((await builder.record(order.type,oldID!)).routingversion).toBe(1);
  expect((await builder.record(order.type,newID!)).routingversion).toBe(3);
  const tab=await runtime(page,operator,order.name);
  await tab.getByText("Old order",{exact:true}).first().click();
  await expect(tab.getByText("Pinned version: 1",{exact:true})).toBeVisible();
  await builder.decide(`${routing.type}.edit`,{type:routing.type,id:first.id},{until:"2025-01-01"});
  expect((await builder.record(order.type,oldID!)).routingversion).toBe(1);
  await operator.open(master,`/record?type=${routing.type}&id=${first.id}`);
  await expect(master.getByText("Expired",{exact:true})).toBeVisible();
  await expect(master.getByText("Valid until: 2025-01-01",{exact:true})).toBeVisible();
});
