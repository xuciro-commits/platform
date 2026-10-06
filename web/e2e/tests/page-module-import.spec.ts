import {expect,test} from "@playwright/test";
import {readFile} from "node:fs/promises";
import {readFileSync} from "node:fs";
import {decide,fresh,open,pageUIProfile} from "./host";
const sample=JSON.parse(readFileSync(new URL("../../packages/build/src/workshop/module-import/sample.workshop.json",import.meta.url),"utf8"));

test("Workshop JSON becomes an undoable native draft and frozen original record work while retaining source and refusal diagnostics",async({page,request},info)=>{
 test.setTimeout(90_000);
 const name=fresh("import").replace(/[^a-z0-9]/gi,"").toLowerCase(),type=`build.${name}`,object=fresh("OBJ"),id=fresh("PAGE"),source=JSON.stringify(sample,null,2);
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Imported assets",fields:[{name:"note",title:"Note",type:"text"}],states:[{name:"open",title:"Open"},{name:"done",title:"Done"}],actions:[{name:"close",title:"Complete asset",from:["open"],to:"done",inputs:[{name:"reason",title:"Reason",type:"text",required:true}]}]});
 await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 await decide(request,"desk","build",`${type}.create`,{type,id:`${name}A`},{note:"IMPORT-A"});
 await decide(request,"manager","build","build.page.create",{type:"build.page",id},{name,title:"Before import",object:type,sections:[{id:"before",widget:"text",configVersion:1,title:"Before import",text:"Keep original draft"}],document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["before"]},before:{kind:"widget",section:"before"}}}});
 await open(page,"manager",`/compose?id=${id}`);await page.getByRole("button",{name:"Import Workshop module",exact:true}).click();
 const dialog=page.getByRole("dialog",{name:"Import Workshop module",exact:true}),apply=dialog.getByRole("button",{name:"Apply imported page draft",exact:true});
 const bad=structuredClone(sample);bad.widgets.markdown.type="Scene3D";
 await dialog.getByRole("textbox",{name:"Source module JSON",exact:true}).fill(JSON.stringify(bad));await expect(dialog.getByRole("region",{name:"Migration diagnostics",exact:true})).toContainText("/widgets/markdown/type");await expect(apply).toBeDisabled();
 await dialog.getByRole("textbox",{name:"Source module JSON",exact:true}).fill(source);
 await dialog.getByRole("combobox",{name:"Map object Asset",exact:true}).selectOption(type);
 await dialog.getByRole("combobox",{name:"Map field Asset.name",exact:true}).selectOption("note");
 await dialog.getByRole("combobox",{name:"Map action finishAsset",exact:true}).selectOption(`${type}.close`);
 await expect(dialog.getByRole("status")).toContainText("Mapped 5 widgets");
 await dialog.getByRole("checkbox").check();await expect(apply).toBeEnabled();
 const originalDownload=page.waitForEvent("download");await dialog.getByRole("button",{name:"Download original JSON",exact:true}).click();const original=await originalDownload;await original.saveAs(info.outputPath("original.json"));expect(await readFile(info.outputPath("original.json"),"utf8")).toBe(source);
 // An oversized file cannot silently replace the reviewed source or leave Apply enabled.
 await dialog.getByLabel("Workshop JSON file",{exact:true}).setInputFiles({name:"oversized.json",mimeType:"application/json",buffer:Buffer.alloc(1_048_577,32)});await expect(dialog.getByRole("alert")).toContainText("exceeds the 1 MiB");await expect(apply).toBeDisabled();await expect(dialog.getByRole("textbox",{name:"Source module JSON",exact:true})).toHaveValue(source);
 await dialog.getByLabel("Workshop JSON file",{exact:true}).setInputFiles({name:"source.workshop.json",mimeType:"application/json",buffer:Buffer.from(source)});
 await dialog.getByRole("combobox",{name:"Map object Asset",exact:true}).selectOption(type);await dialog.getByRole("combobox",{name:"Map field Asset.name",exact:true}).selectOption("note");await dialog.getByRole("combobox",{name:"Map action finishAsset",exact:true}).selectOption(`${type}.close`);await dialog.getByRole("checkbox").check();
 if(process.env.PLATFORM_SCREENSHOTS){await dialog.screenshot({path:info.outputPath("module-import-dialog.png")});await page.setViewportSize({width:390,height:844});await page.evaluate(()=>new Promise<void>(r=>requestAnimationFrame(()=>requestAnimationFrame(()=>r()))));await dialog.screenshot({path:info.outputPath("module-import-dialog-narrow.png")});await page.setViewportSize({width:1280,height:720});}
 await apply.click();await expect(dialog).not.toBeVisible();await expect(page.getByRole("heading",{name:"Operations",exact:true}).first()).toBeVisible();
 await page.getByRole("button",{name:"Undo",exact:true}).click();await expect(page.getByRole("heading",{name:"Before import",exact:true}).first()).toBeVisible();await expect(page.getByRole("button",{name:"Save",exact:true})).toBeDisabled();await page.getByRole("button",{name:"Redo",exact:true}).click();
 await page.getByRole("button",{name:"Import Workshop module",exact:true}).click();await expect(dialog.getByRole("textbox",{name:"Source module JSON",exact:true})).toHaveValue(source);
 const reportDownload=page.waitForEvent("download");await dialog.getByRole("button",{name:"Download mapping report",exact:true}).click();const report=await reportDownload;await report.saveAs(info.outputPath("mapping.json"));const mapping=JSON.parse(await readFile(info.outputPath("mapping.json"),"utf8"));expect(mapping.source).toBe(source);expect(mapping.draft.document.formatVersion).toBe(2);expect(mapping.bindings.objects.Asset).toBe(type);await page.keyboard.press("Escape");
 const preview=page.getByRole("heading",{name:"Imported action",exact:true}).locator("..");await expect(preview.getByRole("button",{name:"Complete asset",exact:true})).toBeDisabled();
 await page.getByRole("button",{name:"Save",exact:true}).click();let saved:any;await expect.poll(async()=>{saved=(await(await request.get(`/v1/records/build.page/${id}`,{headers:{Authorization:"Bearer manager"}})).json()).record;return saved.sections?.length;}).toBe(5);
 expect(saved.document.formatVersion).toBe(2);expect(saved.sections.find((s:any)=>s.widget==="inline-action").actions).toEqual([`${type}.close`]);
 await page.getByRole("button",{name:"Review release",exact:true}).click();await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
 saved.sections.find((s:any)=>s.widget==="inline-action").actions=[`${type}.edit`];await decide(request,"manager","build","build.page.edit",{type:"build.page",id},{sections:saved.sections});await page.getByRole("button",{name:"Activate release",exact:true}).click();
 const runtime=await page.context().newPage();await open(runtime,"desk",`/page?app=build&kind=page&name=${name}`);
 const table=runtime.getByRole("heading",{name:"Imported notes",exact:true}).locator(".."),detail=runtime.getByRole("heading",{name:"Imported selection",exact:true}).locator(".."),form=runtime.getByRole("heading",{name:"Imported action",exact:true}).locator("..");
 await table.getByRole("row").filter({hasText:"IMPORT-A"}).click();await expect(detail.getByText("IMPORT-A",{exact:true})).toBeVisible();await form.getByRole("textbox",{name:"Reason *",exact:true}).fill("Imported completion");
 if(process.env.PLATFORM_SCREENSHOTS){await runtime.screenshot({path:info.outputPath("module-import-runtime.png"),fullPage:true});await runtime.setViewportSize({width:390,height:844});await runtime.evaluate(()=>new Promise<void>(r=>requestAnimationFrame(()=>requestAnimationFrame(()=>r()))));await runtime.screenshot({path:info.outputPath("module-import-runtime-narrow.png"),fullPage:true});}
 await form.getByRole("button",{name:"Complete asset",exact:true}).click();await expect.poll(async()=>(await(await request.get(`/v1/records/${type}/${name}A`,{headers:{Authorization:"Bearer desk"}})).json()).record.state).toBe("done");await runtime.reload();await table.getByRole("row").filter({hasText:"IMPORT-A"}).click();await expect(detail.getByText("IMPORT-A",{exact:true})).toBeVisible();await expect(runtime.getByRole("textbox",{name:"Scratch",exact:true})).toHaveValue("seed");
});
