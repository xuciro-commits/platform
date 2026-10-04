import {expect,test} from "@playwright/test";
import {decide,fresh,open} from "./host";

test("the original function editor saves explicit conversation mode and retains its published versions",async({page,request})=>{
 test.setTimeout(60_000);page.setDefaultTimeout(10_000);const name=fresh("conversation").replace(/[^a-z0-9]/gi,"").toLowerCase(),object=fresh("OBJ"),id=fresh("FN"),headers={Authorization:"Bearer manager"};
 await decide(request,"manager","build","build.object.create",{type:"build.object",id:object},{name,title:"Conversation source",fields:[{name:"note",title:"Note",type:"text"}]});await decide(request,"manager","build","build.object.publish",{type:"build.object",id:object},{});
 await decide(request,"manager","build","build.function.create",{type:"build.function",id},{name,title:"Conversation function",description:"Explicit original conversation input",object:`build.${name}`,fields:["note"],instructions:"Answer the member question from the authorized record and retained history.",output:[{name:"summary",type:"string",required:true,description:"Factual answer"}],maxInputBytes:8192,maxOutputBytes:1024,maxTokens:256,roles:["builder","user"]});
 await open(page,"manager",`/function?id=${id}`);
 const model=page.getByRole("figure",{name:"Function map",exact:true}).locator('[data-id="model"]');await model.focus();await model.press("Enter");
 const checkbox=page.getByRole("checkbox",{name:"Accept conversation questions and retained call history",exact:true});await expect(checkbox).not.toBeChecked();await checkbox.check();await page.getByRole("button",{name:"Save function",exact:true}).click();
 await expect.poll(async()=>{const response=await(await request.get(`/v1/records/build.function/${id}`,{headers})).json();return response.record.conversation;}).toBe(true);
 await decide(request,"manager","build","build.function.publish",{type:"build.function",id},{});
 const before=await(await request.get("/v1/definitions",{headers})).json(),fn=before.find((d:any)=>d.ref.kind==="function"&&d.ref.name===name);expect(fn.function.conversation).toBe(true);
 await decide(request,"manager","build","build.function.edit",{type:"build.function",id},{conversation:false});await decide(request,"manager","build","build.function.publish",{type:"build.function",id},{});
 const after=await(await request.get("/v1/definitions",{headers})).json(),latest=after.find((d:any)=>d.ref.kind==="function"&&d.ref.name===name);expect(latest.function.conversation??false).toBe(false);const saved=await(await request.get(`/v1/records/build.function/${id}`,{headers})).json();expect(JSON.parse(saved.record.versions[0]).conversation).toBe(true);
});
