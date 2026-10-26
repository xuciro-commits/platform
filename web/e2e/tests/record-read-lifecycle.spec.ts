import {expect,fresh,stableReadRevision,test} from "./kit";

test("self references do not read the open record again and a saved receipt does not wait for unrelated refreshes",async({page,builder,operator})=>{
 test.setTimeout(60_000);
 const access=[{role:"user",read:"all",create:true,edit:true}],owner=await builder.object({title:"Read owner",fields:[{name:"name",title:"Name",type:"text",search:true},{name:"enabled",title:"Enabled",type:"boolean"}],access});
 const line=await builder.object({title:"Read line",fields:[{name:"parent",title:"Parent",type:"reference",ref:owner.type,inverse:"lines",required:true}],access}),id=fresh("OWNER");
 await operator.decide(`${owner.type}.create`,{type:owner.type,id},{name:"Current owner",enabled:true});
 await builder.records(line.type,Array.from({length:20},()=>({parent:id})));
 // A cold HTTP reader must work without a live snapshot already covering its
 // reference. The original kit keeps this read revision stable for counting.
 await stableReadRevision(page);
 let reads=0;
 page.on("request",request=>{if(request.method()==="GET"&&new URL(request.url()).pathname===`/v1/records/${owner.type}/${id}`)reads++;});
 let hold=false,held=false,release!:()=>void;const refreshGate=new Promise<void>(resolve=>{release=resolve;});
 await page.route("**/v1/definitions",async route=>{if(hold){held=true;await refreshGate;}await route.continue();});
 await operator.open(page,`/record?type=${owner.type}&id=${id}`);
 const references=page.getByRole("button",{name:`${owner.type}/${id}`,exact:true});
 await expect(references).toHaveCount(20);await expect(references.first()).toHaveText("Current owner");expect(reads).toBe(1);
 await page.getByRole("button",{name:"Edit",exact:true}).click();
 const dialog=page.getByRole("dialog");await dialog.getByRole("checkbox",{name:"Enabled",exact:true}).uncheck();hold=true;
 const receipt=page.waitForResponse(response=>new URL(response.url()).pathname==="/v1/submissions"&&response.request().postDataJSON()?.schema?.name===`${owner.type}.edit`);
 try{
  await dialog.getByRole("button",{name:"Save",exact:true}).click();expect((await receipt).ok()).toBe(true);
  await expect.poll(()=>held).toBe(true);
  // Metadata is deliberately still pending; only the actual receipt should
  // complete the edit, and its refreshed self references need no new reads.
  await expect(dialog).toHaveCount(0);
  expect(await operator.record(owner.type,id)).toMatchObject({revision:2,enabled:false});
 }finally{hold=false;release();}
 await expect(references).toHaveCount(20);await expect(references.first()).toHaveText("Current owner");
 await page.getByRole("button",{name:"Edit",exact:true}).click();
 await expect(dialog.getByRole("checkbox",{name:"Enabled",exact:true})).not.toBeChecked();expect(reads).toBe(2);
 await dialog.getByRole("button",{name:"Cancel",exact:true}).click();
});
