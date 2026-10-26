import {expect,fresh,test} from "./kit";

test("lost edit replies recover on reload and reconnect, then reopen a complete current form",async({page,builder,operator,context})=>{
  test.setTimeout(90_000);
  const object=await builder.object({title:"Receipt record",fields:[{name:"note",title:"Note",type:"text"},{name:"enabled",title:"Enabled",type:"boolean"}],access:[{role:"user",read:"all",create:true,edit:true}]}),id=fresh("RECEIPT");
  await operator.decide(`${object.type}.create`,{type:object.type,id},{note:"Original note",enabled:true});
  const intents:{body:any;version?:string}[]=[];
  await page.route("**/v1/submissions",async route=>{
    const request=route.request(),body=request.postDataJSON();
    if(body.schema?.name!==`${object.type}.edit`){await route.continue();return;}
    intents.push({body,version:request.headers()["platform-schema-version"]});
    if(intents.length===1||intents.length===3){
      const response=await route.fetch();expect(response.ok()).toBe(true);
      await route.abort("connectionreset");return;
    }
    await route.continue();
  });
  const dialog=page.getByRole("dialog");
  const saved=()=>page.evaluate(()=>Object.keys(localStorage).filter(name=>name.startsWith("outbox:")).flatMap(name=>JSON.parse(localStorage.getItem(name)!)));
  await operator.open(page,`/record?type=${object.type}&id=${id}`);
  await page.getByRole("button",{name:"Edit",exact:true}).click();
  await dialog.getByRole("checkbox",{name:"Enabled",exact:true}).uncheck();
  await dialog.getByRole("button",{name:"Save",exact:true}).click();
  await expect(dialog.getByRole("button",{name:"Retry confirmation",exact:true})).toBeVisible();
  expect(await operator.record(object.type,id)).toMatchObject({revision:2,enabled:false,note:"Original note"});

  await page.reload();
  await expect.poll(async()=>{const entry=(await saved()).find(item=>item.submission.idempotencyKey===intents[0]!.body.idempotencyKey);return entry?.state;}).toBe("SUBMISSION_STATE_CONFIRMED");
  expect(intents).toHaveLength(2);expect(intents[1]).toEqual(intents[0]);
  expect((await operator.record(object.type,id)).revision).toBe(2);
  await page.getByRole("button",{name:"Edit",exact:true}).click();
  await expect(dialog.getByRole("textbox",{name:"Note",exact:true})).toHaveValue("Original note");
  await expect(dialog.getByRole("checkbox",{name:"Enabled",exact:true})).not.toBeChecked();
  await expect(dialog.getByRole("textbox",{name:"Note",exact:true})).toBeEnabled();

  await dialog.getByRole("textbox",{name:"Note",exact:true}).fill("Reconnected note");
  await dialog.getByRole("button",{name:"Save",exact:true}).click();
  await expect(dialog.getByRole("button",{name:"Retry confirmation",exact:true})).toBeVisible();
  await context.setOffline(true);await context.setOffline(false);
  await expect(dialog).toHaveCount(0);
  await expect.poll(async()=>{const entry=(await saved()).find(item=>item.submission.idempotencyKey===intents[2]!.body.idempotencyKey);return entry?.state;}).toBe("SUBMISSION_STATE_CONFIRMED");
  expect(intents).toHaveLength(4);expect(intents[3]).toEqual(intents[2]);
  expect(await operator.record(object.type,id)).toMatchObject({revision:3,enabled:false,note:"Reconnected note"});
  await page.getByRole("button",{name:"Edit",exact:true}).click();
  await expect(dialog.getByRole("textbox",{name:"Note",exact:true})).toHaveValue("Reconnected note");
  await expect(dialog.getByRole("button",{name:"Save",exact:true})).toBeEnabled();
  await dialog.getByRole("button",{name:"Cancel",exact:true}).click();
});
