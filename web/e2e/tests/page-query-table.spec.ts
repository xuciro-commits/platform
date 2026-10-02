import { expect,test } from "@playwright/test";
import { decide,fresh,open,pageUIProfile,stableReadRevision } from "./host";

test("a table and loop share one bounded plan while view and parameter changes clear old selections",async({page,request},testInfo)=>{
 test.setTimeout(60_000);
 const name=fresh("window").replace(/[^a-z0-9]/gi,"").toLowerCase(),type=`build.${name}`,object=fresh("OBJ"),id=fresh("PAGE");
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Window notes",fields:[{name:"note",title:"Note",type:"text"},{name:"bucket",title:"Bucket",type:"text"}]});
 await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 for(const [suffix,bucket] of [["A1","A"],["A2","A"],["A3","A"],["B1","B"]])await decide(request,"desk","build",`${type}.create`,{type,id:`${name}${suffix}`},{note:`WINDOW-${suffix}`,bucket});
 await decide(request,"manager","build","build.page.create",{type:"build.page",id},{name,title:"Shared query desk",object:type,sections:[{id:"input",widget:"input",configVersion:1,title:"Bucket parameter"},{id:"table",widget:"table",configVersion:1,title:"Shared window",fields:["note","bucket"]},{id:"selected",widget:"detail",configVersion:1,title:"Selected window record",fields:["note"]},{id:"card",widget:"detail",configVersion:1,title:"Window card",fields:["note"],recordVariable:"item"}],document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["input","table","selected","cards"]},input:{kind:"widget",section:"input",valueVariable:"bucket"},table:{kind:"widget",section:"table"},selected:{kind:"widget",section:"selected"},cards:{kind:"loop",title:"Shared cards",children:["card"],loop:{collection:"tableWindow",itemVariable:"item",limit:2}},card:{kind:"widget",section:"card"}},variables:{bucket:{title:"Bucket",scope:"page",type:"string",mode:"state",initial:"A"},window:{title:"Shared dataset",scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}},tableWindow:{scope:"page",type:"object-set",mode:"resource",source:{kind:"query",section:"table"}},item:{scope:"loop-item",owner:"cards",type:"record",mode:"resource",source:{kind:"item",node:"cards"}}},queries:{read:{title:"Shared dataset",object:{app:"build",kind:"object",name:type},limit:2,sort:["id"],conditions:[{field:"bucket",op:"=",value:{variable:"bucket"}}]}}}});
 await open(page,"manager",`/compose?id=${id}`);
 const tree=page.getByRole("region",{name:"Widgets and layout",exact:true}),inspector=page.getByRole("region",{name:"The widget in hand",exact:true});
 await tree.getByRole("button",{name:"Shared window",exact:true}).click();
 await inspector.getByRole("combobox",{name:"Table query window",exact:true}).selectOption("window");
 if(process.env.PLATFORM_SCREENSHOTS)await inspector.screenshot({path:testInfo.outputPath("table-inspector.png")});
 await page.getByRole("button",{name:"Save",exact:true}).click();await expect(page.getByRole("button",{name:"Save",exact:true})).toBeDisabled();
 await page.getByRole("button",{name:"Review release",exact:true}).click();await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();await page.getByRole("button",{name:"Activate release",exact:true}).click();
 const operation=await page.context().newPage();await stableReadRevision(operation);let reads=0;operation.on("request",request=>{const url=new URL(request.url());if(url.pathname===`/v1/records/${type}`)reads++;});
 await open(operation,"desk",`/page?app=build&kind=page&name=${name}`);
 const table=operation.getByRole("table"),cards=operation.getByRole("list",{name:"Shared cards",exact:true});
 const selected=operation.getByRole("heading",{name:"Selected window record",exact:true}).locator("..");
 await expect(table.getByRole("row").filter({hasText:"WINDOW-A1"})).toBeVisible();await expect(cards.getByText("WINDOW-A1",{exact:true})).toBeVisible();expect(reads).toBe(1);
 await table.getByRole("row").filter({hasText:"WINDOW-A1"}).click();await expect(selected.getByText("WINDOW-A1",{exact:true})).toBeVisible();
 await operation.getByRole("button",{name:"Next page",exact:true}).click();
 await expect(table.getByRole("row").filter({hasText:"WINDOW-A3"})).toBeVisible();await expect(cards.getByText("WINDOW-A3",{exact:true})).toBeVisible();
 await expect(selected.getByText("Select a record to see it here.",{exact:true})).toBeVisible();expect(reads).toBe(2);
 await table.getByRole("row").filter({hasText:"WINDOW-A3"}).click();await expect(selected.getByText("WINDOW-A3",{exact:true})).toBeVisible();
 await operation.getByRole("textbox",{name:"Search",exact:true}).fill("A1");
 await expect(table.getByRole("row").filter({hasText:"WINDOW-A1"})).toBeVisible();await expect(cards.getByText("WINDOW-A1",{exact:true})).toBeVisible();
 await expect(selected.getByText("Select a record to see it here.",{exact:true})).toBeVisible();
 await operation.getByRole("textbox",{name:"Bucket parameter",exact:true}).fill("B");
 await expect(operation.getByRole("textbox",{name:"Search",exact:true})).toHaveValue("");
 await expect(table.getByRole("row").filter({hasText:"WINDOW-B1"})).toBeVisible();await expect(cards.getByText("WINDOW-B1",{exact:true})).toBeVisible();
 await operation.getByRole("combobox",{name:"Sort",exact:true}).selectOption("-note");await expect(table.getByRole("row").filter({hasText:"WINDOW-B1"})).toBeVisible();
 if(process.env.PLATFORM_SCREENSHOTS){await operation.screenshot({path:testInfo.outputPath("shared-query-table.png"),fullPage:true});await operation.setViewportSize({width:390,height:844});await operation.evaluate(()=>new Promise<void>((resolve)=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await operation.screenshot({path:testInfo.outputPath("shared-query-table-narrow.png"),fullPage:true});}
 await operation.reload();await expect(operation.getByRole("textbox",{name:"Bucket parameter",exact:true})).toHaveValue("A");await expect(operation.getByRole("textbox",{name:"Search",exact:true})).toHaveValue("");
 await expect(table.getByRole("row").filter({hasText:"WINDOW-A1"})).toBeVisible();
});
