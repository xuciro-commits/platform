import {expect,test} from "@playwright/test";
import {readFileSync} from "node:fs";
import {decide,fresh,open,pageUIProfile,stableReadRevision} from "./host";

test("four embedded sources freeze original pages and render authorized records, real analysis and an isolated external document",async({page,request},info)=>{
 test.setTimeout(120_000);page.setDefaultTimeout(10_000);
 const name=fresh("embedimport").replace(/[^a-z0-9]/gi,"").toLowerCase(),type=`build.${name}`,object=fresh("OBJ"),childID=fresh("PAGE"),analysisID=fresh("PAGE"),parentID=fresh("PAGE"),headers={Authorization:"Bearer manager"};
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Embedding import records",fields:[{name:"name",title:"Name",type:"text"}]});
 await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 for(const id of ["A","B"])await decide(request,"manager","build",`${type}.create`,{type,id},{name:`Authorized original ${id}`});
 const ref={app:"build",kind:"object",name:type},nodes=(sections:any[])=>({root:{kind:"rows",children:sections.map(s=>s.id)},...Object.fromEntries(sections.map(s=>[s.id,{kind:"widget",section:s.id}]))});
 const childSections=[{id:"original",widget:"text",configVersion:1,text:"FROZEN ORIGINAL CHILD"},{id:"detail",widget:"detail",configVersion:1,title:"Confirmed child record",fields:["name"],recordVariable:"record"}];
 await decide(request,"manager","build","build.page.create",{type:"build.page",id:childID},{name:`${name}child`,title:"Actual original child",object:type,sections:childSections,document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:nodes(childSections),variables:{record:{scope:"page",type:"record",mode:"input"}},interface:{version:1,inputs:{asset:{variable:"record",type:"record",object:ref,required:true}}}}});
 await decide(request,"manager","build","build.page.publish",{type:"build.page",id:childID},{});
 const analysisSections=[{id:"count",widget:"metric",configVersion:1,title:"Actual original count",measure:"count",collectionVariable:"set"}];
 await decide(request,"manager","build","build.page.create",{type:"build.page",id:analysisID},{name:`${name}analysis`,title:"Actual original analysis",object:type,sections:analysisSections,document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:nodes(analysisSections),variables:{set:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}}},queries:{read:{object:ref,limit:20,sort:["id"]}}}});
 await decide(request,"manager","build","build.page.publish",{type:"build.page",id:analysisID},{});
 const m={id:"source",name:"Imported originals",pages:[{id:"page",name:"Imported originals",rootSectionId:"root"}],sections:{root:{id:"root",name:"Root",layout:"rows",children:["table","module","custom","dashboard","frame"].map(id=>({kind:"widget",id}))}},overlays:[],unusedWidgetIds:[],variables:[{id:"assets",name:"Source assets",type:"objectSet",definitionKind:"objectSetDefinition",objectSet:{objectType:"Asset",steps:[]}},{id:"selected",name:"Selected asset",type:"object",definitionKind:"widgetOutput",widgetId:"table",widgetOutputKey:"activeObject"}],widgets:{
  table:{id:"table",name:"Original record producer",type:"ObjectTable",config:{objectSetVarId:"assets",activeVarId:"selected",columns:[{key:"name"}]}},
  module:{id:"module",name:"Imported module",type:"EmbeddedModule",config:{moduleId:"source.child",bindings:[{parentVar:"selected",childInterface:"selectedAsset"}]}},
  custom:{id:"custom",name:"Imported registered page",type:"CustomWidget",config:{widgetSet:"com.vendor.ops",version:"1.0.0",params:[{name:"inputAsset",varId:"selected"}],permissions:["read.objects"]}},
  dashboard:{id:"dashboard",name:"Imported analysis",type:"QuiverDashboard",config:{title:"Imported analysis"}},
  frame:{id:"frame",name:"Imported external document",type:"Iframe",config:{url:"https://docs.example.com/report",height:240}}
 }},source=JSON.stringify(m);
 await decide(request,"manager","build","build.page.create",{type:"build.page",id:parentID},{name:`${name}parent`,title:"Embedding source import",object:type,sections:[{id:"initial",widget:"text",configVersion:1,text:"Initial"}],document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:nodes([{id:"initial"}])}});
 await open(page,"manager",`/compose?id=${parentID}`);await page.getByRole("button",{name:"Import Workshop module",exact:true}).click();
 const dialog=page.getByRole("dialog",{name:"Import Workshop module",exact:true});await dialog.getByRole("textbox",{name:"Source module JSON",exact:true}).fill(source);await dialog.getByRole("combobox",{name:"Map object Asset",exact:true}).selectOption(type);await dialog.getByRole("combobox",{name:"Map field Asset.name",exact:true}).selectOption("name");
 const apply=dialog.getByRole("button",{name:"Apply imported page draft",exact:true});await expect(apply).toBeDisabled();
 for(const [title,migration,childName,port]of [["Imported module","original-page",`${name}child`,"selectedAsset"],["Imported registered page","registered-page",`${name}child`,"inputAsset"],["Imported analysis","actual-analysis-page",`${name}analysis`,""]]){
  const fields=dialog.getByRole("group",{name:`Map embedded source ${title}`,exact:true});await fields.getByRole("combobox",{name:"Embedded source interpretation",exact:true}).selectOption(migration);await fields.getByRole("combobox",{name:"Fixed original embedded page",exact:true}).selectOption(JSON.stringify({app:"build",kind:"page",name:childName}));if(port)await fields.getByRole("combobox",{name:`Map embedded input ${port}`,exact:true}).selectOption("asset");
 }
 const frameFields=dialog.getByRole("group",{name:"Map external document Imported external document",exact:true});await frameFields.getByRole("combobox",{name:"External document interpretation",exact:true}).selectOption("sandboxed-document");
 await dialog.getByRole("checkbox").check();await expect(apply).toBeEnabled();
 const downloading=page.waitForEvent("download");await dialog.getByRole("button",{name:"Download mapping report",exact:true}).click();const report=JSON.parse(readFileSync((await(await downloading).path())!,"utf8"));expect(report.source).toBe(source);expect(report.diagnostics.some((d:any)=>d.code==="native-original-embedding"&&!d.blocking)).toBe(true);
 await apply.click();await page.getByRole("button",{name:"Save",exact:true}).click();await expect.poll(async()=>{const body=await(await request.get(`/v1/records/build.page/${parentID}`,{headers})).json();return body.record.sections.filter((s:any)=>s.embedding).length;}).toBe(3);
 await page.getByRole("button",{name:"Review release",exact:true}).click();await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
 childSections[0].text="LATER CHILD";await decide(request,"manager","build","build.page.edit",{type:"build.page",id:childID},{sections:childSections});await decide(request,"manager","build","build.page.publish",{type:"build.page",id:childID},{});await page.getByRole("button",{name:"Activate release",exact:true}).click();
 const runtime=await page.context().newPage();await stableReadRevision(runtime);let frameHeaders:Record<string,string>|undefined;
 await runtime.route("https://docs.example.com/report",async route=>{frameHeaders=route.request().headers();await route.fulfill({contentType:"text/html",body:'<!doctype html><p>Original external document bytes</p><script>document.body.insertAdjacentHTML("beforeend","<p id=script-ran>External script executed</p>")</script><form><input name=external></form>'});});
 await open(runtime,"desk",`/page?app=build&kind=page&name=${name}parent`);
 const section=(title:string)=>runtime.getByRole("heading",{name:title,exact:true,level:3}).locator(".."),table=section("Original record producer"),module=section("Imported module"),custom=section("Imported registered page"),analysis=section("Imported analysis");
 await table.getByRole("cell",{name:"Authorized original A",exact:true}).click();
 for(const view of [module,custom]){await expect(view).toContainText("Authorized original A");await expect(view).toContainText("FROZEN ORIGINAL CHILD");await expect(view).not.toContainText("LATER CHILD");}
 await table.getByRole("cell",{name:"Authorized original B",exact:true}).click();for(const view of [module,custom]){await expect(view).toContainText("Authorized original B");await expect(view).not.toContainText("Authorized original A");}
 await expect(analysis.getByText("2",{exact:true})).toBeVisible();await expect(analysis).not.toContainText("MTBF");
 const external=runtime.frameLocator('iframe[title="Imported external document"]');await expect(external.getByText("Original external document bytes",{exact:true})).toBeVisible();await expect(external.locator("#script-ran")).toHaveCount(0);expect(frameHeaders?.authorization).toBeUndefined();expect(frameHeaders?.referer).toBeUndefined();
 if(process.env.PLATFORM_SCREENSHOTS){await module.screenshot({path:info.outputPath("imported-original-module.png")});await runtime.setViewportSize({width:390,height:844});await expect(analysis.getByText("2",{exact:true})).toBeVisible();await analysis.screenshot({path:info.outputPath("imported-analysis-narrow.png")});}
});
