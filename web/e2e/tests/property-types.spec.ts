import {expect,test} from "@playwright/test";
import {decide,fresh,open} from "./host";

const read=async(request:any,type:string,id:string)=>(await(await request.get(`/v1/records/${type}/${id}`,{headers:{Authorization:"Bearer manager"}})).json()).record;
const freeze=async(page:any)=>{
 await page.getByRole("button",{name:"Review release",exact:true}).click();
 await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();
 await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
 await expect(page.getByRole("button",{name:"Activate release",exact:true})).toBeEnabled();
};
const activate=async(page:any)=>{
 await page.getByRole("button",{name:"Activate release",exact:true}).click();
 await expect(page.getByRole("status").filter({hasText:"Release active for operators."})).toBeVisible();
};

test("a shared property is authored once, pinned by two objects and delivered through original records",async({page,request},testInfo)=>{
 test.setTimeout(90_000);
 const name=fresh("quantity").replace(/[^a-z0-9]/gi,"").toLowerCase(),objects=[fresh("OBJ"),fresh("OBJ")],names=[`${name}planned`,`${name}actual`];
 await open(page,"manager","/property-type?id=new");
 await page.getByLabel("Shared property name",{exact:true}).fill(name);
 await page.getByLabel("Shared property title",{exact:true}).fill("Shared quantity");
 await page.getByLabel("Shared property description",{exact:true}).fill("Quantity measured by the owning object.");
 await page.getByRole("combobox",{name:"Shared property type",exact:true}).selectOption("integer");
 await page.getByRole("button",{name:"Save shared property",exact:true}).click();
 let id="";await expect.poll(async()=>{const result=await(await request.get("/v1/records/build.propertytype?limit=100",{headers:{Authorization:"Bearer manager"}})).json();id=result.records.find((r:any)=>r.name===name)?.id??"";return id;}).not.toBe("");
 await expect(page.getByRole("button",{name:"Review release",exact:true})).toBeEnabled();
 await freeze(page);
 await decide(request,"manager","build","build.propertytype.edit",{type:"build.propertytype",id},{title:"Unpublished quantity"});
 await activate(page);expect(JSON.parse((await read(request,"build.propertytype",id)).published).title).toBe("Shared quantity");
 const binding={ref:{app:"build",kind:"property-type",name},sourceVersion:"1.property-1"},selection=`build/property-type/${name}@1.property-1`;
 for(let at=0;at<2;at++){
  await decide(request,"manager","build","build.object.create",{type:"build.object",id:objects[at]},{name:names[at],title:at?"Measured item":"Planned item",fields:[{name:"quantity",title:"Local quantity",type:"text"}],access:[{role:"user",read:"all",create:true,edit:true}]});
  await open(page,"manager",`/process?id=${objects[at]}&field=quantity`);
  const inspector=page.getByRole("region",{name:"The piece in hand",exact:true});
  await inspector.getByRole("combobox",{name:"Shared property version",exact:true}).selectOption(selection);
  await expect(inspector.getByRole("combobox",{name:"Type",exact:true})).toHaveValue("integer");await expect(inspector.getByRole("combobox",{name:"Type",exact:true})).toBeDisabled();
  await expect(inspector.getByLabel("What people call it",{exact:true})).toHaveValue("Shared quantity");await expect(inspector.getByLabel("What people call it",{exact:true})).toBeDisabled();
  await inspector.getByLabel("Name",{exact:true}).fill(at?"measured":"planned");await inspector.getByLabel("Required",{exact:true}).check();
  await page.getByRole("button",{name:"Save",exact:true}).click();await expect.poll(async()=>(await read(request,"build.object",objects[at])).fields[0].property).toEqual(binding);await expect(inspector.getByRole("combobox",{name:"Shared property version",exact:true})).toBeEnabled();
  if(process.env.PLATFORM_SCREENSHOTS&&at===0){await page.screenshot({path:testInfo.outputPath("shared-field-desktop.png"),fullPage:true});await page.setViewportSize({width:390,height:844});await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await inspector.getByRole("combobox",{name:"Shared property version",exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:testInfo.outputPath("shared-field-narrow.png"),fullPage:true});await page.setViewportSize({width:1280,height:720});}
  await freeze(page);
  if(at===0){await decide(request,"manager","build","build.propertytype.edit",{type:"build.propertytype",id},{title:"New quantity"});await decide(request,"manager","build","build.propertytype.publish",{type:"build.propertytype",id},{});}
  await activate(page);
 }
 await open(page,"manager",`/process?id=${objects[0]}&field=planned`);
 const inspector=page.getByRole("region",{name:"The piece in hand",exact:true});
 await expect(inspector.getByRole("combobox",{name:"Shared property version",exact:true})).toHaveValue(selection);
 await expect(inspector.getByRole("combobox",{name:"Shared property version",exact:true}).getByRole("option",{name:/New quantity.*1.property-2/})).toHaveCount(1);
 await expect(inspector.getByLabel("What people call it",{exact:true})).toHaveValue("Shared quantity");
 for(let at=0;at<2;at++){
  const type=`build.${names[at]}`,field=at?"measured":"planned",row=fresh("ROW");
  await decide(request,"desk","build",`${type}.create`,{type,id:row},{[field]:at+3});
  const result=await(await request.get(`/v1/records/${type}/${row}`,{headers:{Authorization:"Bearer desk"}})).json();expect(result.record[field]).toBe(at+3);
 }
 await open(page,"manager",`/model?object=build.${names[0]}&tab=properties`);
 const workspace=page.getByRole("region",{name:"Model workspace",exact:true}),semantic=page.getByRole("region",{name:"Semantic inspector",exact:true});
 await workspace.getByRole("row").filter({has:page.getByRole("cell",{name:"planned",exact:true})}).click();await expect(semantic).toContainText("1.property-1");await expect(semantic).toContainText("Quantity measured by the owning object.");
 await page.getByRole("button",{name:"Shared property catalog",exact:true}).click();await workspace.getByRole("row").filter({hasText:name}).filter({hasText:"1.property-1"}).click();
 await expect(semantic.getByRole("button",{name:"Planned item · planned",exact:true})).toBeVisible();await expect(semantic.getByRole("button",{name:"Measured item · measured",exact:true})).toBeVisible();
 if(process.env.PLATFORM_SCREENSHOTS){await page.screenshot({path:testInfo.outputPath("shared-property-catalog.png"),fullPage:true});}
 await open(page,"manager",`/property-type?id=${id}`);
 await expect(page.getByRole("combobox",{name:"Shared property type",exact:true})).toBeDisabled();await expect(page.getByLabel("Shared property name",{exact:true})).toBeDisabled();
 if(process.env.PLATFORM_SCREENSHOTS){await page.screenshot({path:testInfo.outputPath("shared-property-editor.png"),fullPage:true});await page.setViewportSize({width:390,height:844});await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await page.screenshot({path:testInfo.outputPath("shared-property-editor-narrow.png"),fullPage:true});}
 const operator=await page.context().newPage();await open(operator,"desk",`/property-type?id=${id}`);await expect(operator.getByRole("heading",{name:"Welcome, desk-1",exact:true})).toBeVisible();await expect(operator.getByRole("button",{name:"Shared properties",exact:true})).toHaveCount(0);await expect(operator.getByRole("button",{name:"Save shared property",exact:true})).toHaveCount(0);
 await expect(decide(request,"desk","build","build.propertytype.edit",{type:"build.propertytype",id},{title:"Unauthorized"})).rejects.toThrow(/POLICY_DENIED/);
});

test("a shared property draft keeps its original revision on conflict and requires a choice before discarding",async({page,request})=>{
 const name=fresh("meaning").replace(/[^a-z0-9]/gi,"").toLowerCase(),id=fresh("PROP");
 await decide(request,"manager","build","build.propertytype.create",{type:"build.propertytype",id},{name,title:"Saved meaning",description:"A shared meaning",type:"text"});
 await open(page,"manager",`/property-type?id=${id}`);await page.getByLabel("Shared property title",{exact:true}).fill("Local meaning");
 await decide(request,"manager","build","build.propertytype.edit",{type:"build.propertytype",id},{title:"Remote meaning"});
 for(let attempt=0;attempt<2;attempt++){await page.getByRole("button",{name:"Save shared property",exact:true}).click();await expect(page.getByRole("alert").filter({hasText:/CONFLICT|changed|revision/i})).toBeVisible();await expect(page.getByLabel("Shared property title",{exact:true})).toHaveValue("Local meaning");}
 expect((await read(request,"build.propertytype",id)).title).toBe("Remote meaning");
 await page.getByRole("button",{name:"Reload saved shared property",exact:true}).click();
 await expect(page.getByRole("dialog")).toBeVisible();await page.getByRole("button",{name:"Keep editing",exact:true}).click();await expect(page.getByLabel("Shared property title",{exact:true})).toHaveValue("Local meaning");
 await page.getByRole("button",{name:"Reload saved shared property",exact:true}).click();await page.getByRole("button",{name:"Discard changes",exact:true}).click();await expect(page.getByLabel("Shared property title",{exact:true})).toHaveValue("Remote meaning");
});
