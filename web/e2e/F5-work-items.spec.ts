import {acceptedClient,expect,signIn,test,unique, subjectUpdate } from "./kit";

test.use({actionTimeout:15_000});

test("F5 a member completes work and approves a request from their inbox, and the initiator is notified",async({page,browser})=>{
  const admin=acceptedClient(page),name=unique("work"),project=unique("workproject"),object=unique("OBJECT"),record=unique("RECORD"),task=unique("TASK");
  const type=`build.${name}`,title=`Review ${name}`;
  await admin.submit("build.app",project,"create",{name:project,title:"Work project"});
  await admin.submit("build.object",object,"create",{name,title:"Work subject",project,display:"subject",fields:[{name:"subject",title:"Subject",type:"text",required:true}],access:[{role:"user",read:"all"}],states:[{name:"open",title:"Open"},{name:"pending",title:"Pending"},{name:"approved",title:"Approved"}]});
  await admin.submit("build.actiontype",`${type}.release`,"create",{name:`${type}.release`,object:type,project,definition:{name:"release",title:"Release",from:["open"],transaction:subjectUpdate(type,{state:{kind:"choice",lit:{value:"approved"}}}),approval:{pending:"pending",levels:[{title:"Business review",role:"user"}]}}});
  await admin.publish([{kind:"object",id:object}]);
  await admin.submit(type,record,"create",{subject:`Subject ${name}`});
  await admin.submit("work.task",task,"create",{title,body:"Check the linked subject",ref:`${type}/${record}`,assignee:"user-1"});
  await signIn(page,"user","/home");
  await page.getByRole("button",{name:"Notifications",exact:true}).click();
  await page.getByRole("button",{name:"Tasks and approvals",exact:true}).click();
  const item=page.getByRole("listitem").filter({hasText:title});
  await expect(item).toContainText("Check the linked subject");
  await item.getByRole("button",{name:"Done",exact:true}).click();
  await expect(item).toBeHidden();
  expect((await admin.read<{record:{state:string}}>(`/v1/records/work.task/${task}`)).record.state).toBe("done");
  const notices=await admin.read<{key:string;ref:string}[]>("/v1/notifications");
  expect(notices.some(notice=>notice.key===`task-closed:${task}` && notice.ref===`work.task/${task}`)).toBeTruthy();
  await admin.submit(type,record,"release",{});
  await page.goto("/#/inbox");
  const approval=page.getByRole("listitem").filter({hasText:record});
  await approval.getByRole("button",{name:"Approve",exact:true}).click();
  await expect(approval).toBeHidden();
  expect((await admin.read<{record:{state:string}}>(`/v1/records/${type}/${record}`)).record.state).toBe("approved");
  const initiator=await browser.newPage();
  await signIn(initiator,"admin","/notifications");
  await expect(initiator.getByText("Approved",{exact:false}).first()).toBeVisible();
  await initiator.close();
});
