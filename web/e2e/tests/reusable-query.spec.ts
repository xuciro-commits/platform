import { expect, test } from "@playwright/test";
import { decide, fresh, open, pageUIProfile } from "./host";

test("a builder publishes one reusable query and two pages keep its exact version after a newer publication", async ({page,request},testInfo)=>{
 test.setTimeout(60_000);
 const name=fresh("query").replace(/[^a-z0-9]/gi,"").toLowerCase(),type=`build.${name}`,object=fresh("OBJ"),first=fresh("PAGE"),second=fresh("PAGE"),firstName=`${name}first`,secondName=`${name}second`;
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Reusable query notes",fields:[{name:"note",title:"Note",type:"text"},{name:"bucket",title:"Bucket",type:"text"}]});
 await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 for(const [note,bucket] of [["REUSABLE-A","A"],["REUSABLE-B","B"]])await decide(request,"desk","build",`${type}.create`,{type,id:fresh("ROW")},{note,bucket});
 await open(page,"manager","/query?id=new");
 await page.getByLabel("Query name",{exact:true}).fill(name);
 await page.getByLabel("Query title",{exact:true}).fill("Shared bucket query");
 await page.getByLabel("Query description",{exact:true}).fill("Reuse one published query in two pages.");
 await page.getByRole("combobox",{name:"Source object",exact:true}).selectOption(type);
 await page.getByRole("button",{name:"Add query condition",exact:true}).click();
 await page.getByRole("combobox",{name:"Query condition field",exact:true}).selectOption("bucket");
 await page.getByRole("textbox",{name:"Query literal value",exact:true}).fill("A");
 await page.getByRole("button",{name:"Save query",exact:true}).click();
 await expect(page.getByRole("button",{name:"Save query",exact:true})).toBeDisabled();
 const queryID=(await(await request.get("/v1/records/build.query?limit=100",{headers:{Authorization:"Bearer manager"}})).json()).records.find((q:any)=>q.name===name).id;
 await page.getByRole("button",{name:"Review release",exact:true}).click();
 await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();
 await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
 // A mutable edit after freeze cannot contaminate the accepted candidate.
 await decide(request,"manager","build","build.query.edit",{type:"build.query",id:queryID},{domain:[["bucket","=","B"]]});
 await page.getByRole("button",{name:"Activate release",exact:true}).click();
 await expect.poll(async()=>(await(await request.get(`/v1/records/build.query/${queryID}`,{headers:{Authorization:"Bearer manager"}})).json()).record.version).toBe(1);
 const query=(await(await request.get(`/v1/records/build.query/${queryID}`,{headers:{Authorization:"Bearer manager"}})).json()).record;
 expect(JSON.parse(query.published).domain).toEqual([["bucket","=","A"]]);
 expect(query.domain).toEqual([["bucket","=","B"]]);
 const binding=`build/${name}@1.query-1`;
 for(const [id,pageName] of [[first,firstName],[second,secondName]]){
  await decide(request,"manager","build","build.page.create",{type:"build.page",id},{name:pageName,title:pageName,object:type,sections:[{id:"table",widget:"table",configVersion:1,title:"Shared notes",fields:["note","bucket"]}],document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["table"]},table:{kind:"widget",section:"table"}},variables:{}}});
  await open(page,"manager",`/compose?id=${id}`);
  const tree=page.getByRole("region",{name:"Widgets and layout",exact:true}),inspector=page.getByRole("region",{name:"The widget in hand",exact:true});
  await tree.getByRole("button",{name:"Query plans",exact:true}).click();await inspector.getByRole("button",{name:"Add query plan",exact:true}).click();
  await inspector.getByRole("combobox",{name:"Named query binding",exact:true}).selectOption(binding);
  await tree.getByRole("button",{name:/^Shared notes/}).click();
  await inspector.getByRole("combobox",{name:"Table query window",exact:true}).selectOption({index:1});
  await page.getByRole("button",{name:"Save",exact:true}).click();await expect(page.getByRole("button",{name:"Save",exact:true})).toBeDisabled();
  await decide(request,"manager","build","build.page.publish",{type:"build.page",id},{});
 }
 await decide(request,"manager","build","build.query.publish",{type:"build.query",id:queryID},{});
 await open(page,"manager",`/compose?id=${first}`);
 const tree=page.getByRole("region",{name:"Widgets and layout",exact:true}),inspector=page.getByRole("region",{name:"The widget in hand",exact:true});
 await tree.getByRole("button",{name:"Query plans",exact:true}).click();
 await expect(inspector.getByRole("combobox",{name:"Named query binding",exact:true})).toHaveValue(binding);
 await expect(inspector.getByRole("combobox",{name:"Named query binding",exact:true}).getByRole("option",{name:/1.query-2/})).toHaveCount(1);
 for(const pageName of [firstName,secondName]){
  const runtime=await page.context().newPage();await open(runtime,"desk",`/page?app=build&kind=page&name=${pageName}`);
  await expect(runtime.getByText("REUSABLE-A",{exact:true})).toBeVisible();await expect(runtime.getByText("REUSABLE-B",{exact:true})).toHaveCount(0);
  await runtime.reload();await expect(runtime.getByText("REUSABLE-A",{exact:true})).toBeVisible();await runtime.close();
 }
 await open(page,"manager",`/query?id=${queryID}`);
 await expect(page.getByRole("region",{name:"Published query versions",exact:true})).toContainText("1.query-1");
 if(process.env.PLATFORM_SCREENSHOTS){await page.screenshot({path:testInfo.outputPath("reusable-query-editor.png"),fullPage:true});await page.setViewportSize({width:390,height:844});await page.evaluate(()=>new Promise<void>((resolve)=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await page.screenshot({path:testInfo.outputPath("reusable-query-editor-narrow.png"),fullPage:true});}
});
