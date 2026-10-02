import {expect,test} from "@playwright/test";
import {decide,fresh,open} from "./host";
import {addBlock,chooseBlock} from "./workflow-helpers";

test("a Logic workflow authors a retained query and keeps its frozen read after a later publication",async({page,request},testInfo)=>{
 test.setTimeout(90_000);
 const name=fresh("logicquery").replace(/[^a-z0-9]/gi,"").toLowerCase(),type=`build.${name}`,object=fresh("OBJ"),query=fresh("QUERY");
 const read=async(typ:string,id:string)=>(await(await request.get(`/v1/records/${typ}/${id}`,{headers:{Authorization:"Bearer manager"}})).json()).record;
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Logic query notes",fields:[{name:"label",title:"Label",type:"text"},{name:"bucket",title:"Bucket",type:"text"}]});
 await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 for(const [label,bucket] of [["QUERY-FIRST","A"],["QUERY-SECOND","B"]])await decide(request,"desk","build",`${type}.create`,{type,id:fresh("ROW")},{label,bucket});
 await decide(request,"manager","build","build.query.create",{type:"build.query",id:query},{name,title:"Original Logic query",description:"One reusable query for a workflow.",object:type,domain:[["bucket","=","A"]],limit:50});
 await decide(request,"manager","build","build.query.publish",{type:"build.query",id:query},{});
 await open(page,"manager","/workflow?id=new");
 const properties=page.getByRole("region",{name:"Workflow properties",exact:true});
 await properties.getByRole("textbox",{name:"Workflow name",exact:true}).fill(name);
 await properties.getByRole("textbox",{name:"Workflow title",exact:true}).fill("Pinned query workflow");
 await addBlock(page,`build/query/${name}`);
 await expect(properties.getByRole("combobox",{name:"Retained query version",exact:true})).toHaveValue("1");
 await addBlock(page,"flow/control/end");
 await properties.getByRole("combobox",{name:"Binding source",exact:true}).selectOption("step");
 await properties.getByRole("combobox",{name:"Upstream step",exact:true}).selectOption(name);
 await chooseBlock(page,name,true);await properties.getByText("Control paths",{exact:true}).click();await properties.getByRole("combobox",{name:"Next path",exact:true}).selectOption("end");
 await page.getByRole("button",{name:"Save workflow",exact:true}).click();await expect(page.getByRole("button",{name:"Save workflow",exact:true})).toBeDisabled();
 let id="";await expect.poll(async()=>{const result=await(await request.get("/v1/records/build.process?limit=500",{headers:{Authorization:"Bearer manager"}})).json();id=result.records.find((r:any)=>r.name===name)?.id??"";return id;}).not.toBe("");
 expect((await read("build.process",id)).steps[0]).toMatchObject({kind:"query",queryVersion:1,query:name});
 await page.getByRole("toolbar",{name:"Workflow actions",exact:true}).getByRole("button",{name:"Release",exact:true}).click();await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
 await decide(request,"manager","build","build.query.edit",{type:"build.query",id:query},{title:"Later Logic query",domain:[["bucket","=","B"]]});await decide(request,"manager","build","build.query.publish",{type:"build.query",id:query},{});
 await page.getByRole("button",{name:"Activate release",exact:true}).click();await expect(page.getByRole("status").filter({hasText:"Release active for operators."})).toBeVisible();
 await decide(request,"manager","build","build.query.edit",{type:"build.query",id:query},{title:"Latest Logic query",domain:[["bucket","=","B"]]});await decide(request,"manager","build","build.query.publish",{type:"build.query",id:query},{});
 await open(page,"manager",`/workflow?id=${id}`);await chooseBlock(page,name);await expect(properties.getByRole("combobox",{name:"Retained query version",exact:true})).toHaveValue("1");
 await expect(properties.getByRole("combobox",{name:"Retained query version",exact:true}).getByRole("option",{name:"1.query-3",exact:true})).toHaveCount(1);
 // A later descriptor must not be shown as the old step's source.
 await properties.getByRole("tab",{name:"Settings",exact:true}).click();await expect(properties).toContainText(`build/query/${name}@1.query-1`);
 if(process.env.PLATFORM_SCREENSHOTS){await page.screenshot({path:testInfo.outputPath("logic-query-source.png"),fullPage:true});await properties.getByRole("tab",{name:"Input",exact:true}).click();await page.setViewportSize({width:390,height:844});await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await properties.getByRole("combobox",{name:"Retained query version",exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:testInfo.outputPath("logic-query-source-narrow.png"),fullPage:true});await page.setViewportSize({width:1280,height:720});}
 await page.getByRole("toolbar",{name:"Workflow actions",exact:true}).getByRole("button",{name:"Run",exact:true}).click();await page.getByRole("button",{name:"Run published version",exact:true}).click();
 let instance:any;await expect.poll(async()=>{const result=await(await request.get("/v1/records/flow.instance?limit=500",{headers:{Authorization:"Bearer manager"}})).json();instance=result.records.find((r:any)=>r.flow===`build.${name}`);return instance?.state;}).toBe("done");
 expect(instance.outputs[name].records.map((r:any)=>r.label)).toEqual(["QUERY-FIRST"]);expect(instance.dependencies).toMatch(/^sha256-v1:/);expect(instance.release).toMatch(/^sha256-v1:/);
 await page.reload();await chooseBlock(page,name);await expect(properties.getByRole("combobox",{name:"Retained query version",exact:true})).toHaveValue("1");
 await expect(decide(request,"desk","build","build.process.edit",{type:"build.process",id},{title:"Unauthorized"})).rejects.toThrow(/POLICY_DENIED/);
});
