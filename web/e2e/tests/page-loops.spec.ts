import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("bounded loop cards keep item identity and state and invoke original actions after candidate activation", async ({ page, request }, testInfo) => {
  const name=fresh("loop").replace(/[^a-z0-9]/gi,"").toLowerCase(), object=fresh("OBJ"), id=fresh("PAGE"), type=`build.${name}`;
  await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Loop notes",fields:[{name:"note",title:"Note",type:"text"},{name:"group",title:"Group",type:"choice",choices:"a,b"}],states:[{name:"open",title:"Open"},{name:"done",title:"Done"}],actions:[{name:"complete",title:"Complete note",from:["open"],to:"done"},{name:"review",title:"Review note",from:["open"],to:"done",inputs:[{name:"reason",title:"Reason",type:"text",required:true}],conditions:[{field:"input.reason",operator:"=",value:"approved",message:"Reason must be approved"}]}]});
  await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
  for(let n=0;n<36;n++) await decide(request,"desk","build",`${type}.create`,{type,id:fresh("NOTE")},{note:`LOOP-${n<18?"A":"B"}-${n}`,group:n<18?"a":"b"});
  await decide(request,"manager","build","build.page.create",{type:"build.page",id},{name,title:"Loop operation desk",object:type,sections:[
    {widget:"table",title:"Source notes",fields:["note","group"]},{widget:"filter",title:"Group filter",fields:["group"]},
    {widget:"detail",title:"Note detail",fields:["note","group"]},{widget:"actions",title:"Note actions",actions:[`${type}.complete`,`${type}.review`]},
  ]});
  await open(page,"manager",`/compose?id=${id}`);
  const tree=page.getByRole("region",{name:"Widgets and layout",exact:true}), inspector=page.getByRole("region",{name:"The widget in hand",exact:true});
  await tree.getByRole("button",{name:"Page variables",exact:true}).click();
  await inspector.getByRole("button",{name:"Add variable",exact:true}).click();
  await inspector.getByLabel("Variable label",{exact:true}).fill("Matching notes");
  await inspector.getByRole("combobox",{name:"Variable mode",exact:true}).selectOption("resource");
  await inspector.getByRole("combobox",{name:"Resource output kind",exact:true}).selectOption("query");
  await inspector.getByRole("combobox",{name:"Source widget",exact:true}).selectOption({label:"Source notes"});
  await tree.getByRole("button",{name:"Note detail",exact:true}).click();
  await tree.getByRole("button",{name:"Loop",exact:true}).click();
  await inspector.getByLabel("Container title",{exact:true}).fill("Record cards");
  await inspector.getByLabel("Loop item limit",{exact:true}).fill("50");
  const loopButton=tree.getByRole("button",{name:"Commands for Record cards",exact:true});
  const chooseLoop=async()=>{await loopButton.click();await page.getByRole("menuitem",{name:"Select layout",exact:true}).click();};
  await tree.getByRole("button",{name:"Page variables",exact:true}).click();
  await inspector.getByRole("button",{name:"Add variable",exact:true}).click();
  await inspector.getByLabel("Variable label",{exact:true}).fill("Item context shown");
  await inspector.getByRole("combobox",{name:"Value type",exact:true}).selectOption("boolean");
  await inspector.getByRole("combobox",{name:"Variable scope",exact:true}).selectOption({label:"Record cards"});
  const flag=await inspector.getByRole("combobox",{name:"Choose page variable",exact:true}).inputValue();
  await chooseLoop();
  await tree.getByRole("button",{name:"Button",exact:true}).click();
  await inspector.getByLabel("Title",{exact:true}).fill("Show context");
  await inspector.getByRole("combobox",{name:"Target state variable",exact:true}).selectOption(flag);
  await inspector.getByRole("checkbox",{name:"Event value",exact:true}).check();
  await chooseLoop();
  await tree.getByRole("button",{name:"Text",exact:true}).click();
  await inspector.getByLabel("Title",{exact:true}).fill("Item guidance");
  await inspector.getByPlaceholder("Write markdown here…",{exact:true}).fill("Context belongs to this record.");
  await inspector.getByRole("combobox",{name:"Visible when",exact:true}).selectOption(flag);
  const preview=page.getByRole("region",{name:"The page",exact:true}).getByRole("list",{name:"Record cards",exact:true});
  await expect(preview.getByText("Actions do not run while you compose.",{exact:true}).first()).toBeVisible();
  await expect(preview.getByRole("button",{name:"Complete note",exact:true})).toHaveCount(0);
  await page.getByRole("button",{name:"Save",exact:true}).click();
  await expect(page.getByRole("button",{name:"Save",exact:true})).toBeDisabled();
  const saved=(await (await request.get(`/v1/records/build.page/${id}`,{headers:{Authorization:"Bearer manager"}})).json()).record;
  const loop=Object.values(saved.document.nodes).find((node:any)=>node.kind==="loop") as any;
  expect(loop.loop.limit).toBe(50);
  expect(saved.sections.filter((s:any)=>["detail","actions"].includes(s.widget)).every((s:any)=>s.recordVariable===loop.loop.itemVariable)).toBe(true);
  if(process.env.PLATFORM_SCREENSHOTS) {
    await loopButton.scrollIntoViewIfNeeded();await chooseLoop();
    await preview.getByText("Actions do not run while you compose.",{exact:true}).first().scrollIntoViewIfNeeded();
    await page.screenshot({path:testInfo.outputPath("loop-designer.png"),fullPage:true});
  }
  await page.getByRole("button",{name:"Review release",exact:true}).click();
  await page.getByRole("button",{name:"Check draft and dependencies",exact:true}).click();
  await page.getByRole("button",{name:"Save immutable candidate",exact:true}).click();
  await page.getByRole("button",{name:"Activate release",exact:true}).click();
  const operation=await page.context().newPage(), reads=new Set<string>();let getCount=0;
  operation.on("request",(r)=>{const url=new URL(r.url());if(r.method()==="GET"&&url.pathname.startsWith(`/v1/records/${type}/`)){reads.add(url.pathname);getCount++;}});
  await open(operation,"desk",`/page?app=build&kind=page&name=${name}`);
  const list=operation.getByRole("list",{name:"Record cards",exact:true}), items=list.getByRole("listitem");
  await expect(items.first().getByRole("button",{name:"Complete note",exact:true})).toBeVisible();
  expect(await items.count()).toBeLessThan(36);expect(reads.size).toBeLessThan(36);
  const firstID=await items.first().getByRole("region").getAttribute("aria-label"), secondID=await items.nth(1).getByRole("region").getAttribute("aria-label");
  const first=list.getByRole("region",{name:firstID!,exact:true}), second=list.getByRole("region",{name:secondID!,exact:true});
  await first.getByRole("button",{name:"Show context",exact:true}).click();
  await expect(first.getByText("Context belongs to this record.",{exact:true})).toBeVisible();
  await expect(second.getByText("Context belongs to this record.",{exact:true})).toHaveCount(0);
  await list.focus();await list.evaluate((element)=>{element.scrollTop=element.scrollHeight;});
  await expect(first).toHaveCount(0);
  await list.evaluate((element)=>{element.scrollTop=0;});
  await expect(first.getByText("Context belongs to this record.",{exact:true})).toBeVisible();
  if(process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({path:testInfo.outputPath("loop-runtime.png"),fullPage:true});
  // A changed source query clears item state, even when the old identity later returns.
  const filter=operation.getByRole("search",{name:"Group filter",exact:true}).getByRole("combobox");
  await filter.selectOption("a");await expect(first).toHaveCount(0);
  await filter.selectOption("");await expect(first.getByRole("button",{name:"Show context",exact:true})).toBeVisible();
  await expect(first.getByText("Context belongs to this record.",{exact:true})).toHaveCount(0);
  await first.getByRole("button",{name:"Review note",exact:true}).click();
  const action=operation.getByRole("dialog",{name:`Review note ${firstID}`,exact:true});
  await action.getByRole("textbox",{name:/Reason/}).fill("Keep this draft");
  const previousReads=getCount;
  await action.getByRole("button",{name:"Review note",exact:true}).click();
  await expect(action.getByRole("alert")).toContainText("Reason must be approved");
  await expect.poll(()=>getCount).toBeGreaterThan(previousReads);
  await expect(action.getByRole("textbox",{name:/Reason/})).toHaveValue("Keep this draft");
  await action.getByRole("button",{name:"Cancel",exact:true}).click();
  await first.getByRole("button",{name:"Complete note",exact:true}).click();
  const state=async(record:string)=>(await (await request.get(`/v1/records/${type}/${record}`,{headers:{Authorization:"Bearer desk"}})).json()).record.state;
  await expect.poll(()=>state(firstID!)).toBe("done");expect(await state(secondID!)).toBe("open");
  if(process.env.PLATFORM_SCREENSHOTS){await expect(operation.getByText("Refreshing loop records…",{exact:true})).toHaveCount(0);await operation.setViewportSize({width:390,height:844});await first.scrollIntoViewIfNeeded();await operation.screenshot({path:testInfo.outputPath("loop-runtime-narrow.png"),fullPage:true});}
  await operation.reload();await expect(list.getByText("Context belongs to this record.",{exact:true})).toHaveCount(0);
});
