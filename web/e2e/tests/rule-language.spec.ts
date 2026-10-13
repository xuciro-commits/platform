import {expect,pageUIProfile,runtime,slug,test} from "./kit";

test("published Expr controls visibility through the shared rule engine",async({page,builder,operator})=>{
 const {type}=await builder.object({name:slug("rule"),title:"Rule records",fields:[{name:"name",title:"Name",type:"text"}]});
 const document={formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["toggle","conditional"]},toggle:{kind:"widget",section:"toggle"},conditional:{kind:"rows",visibleWhen:"visible",children:["notice"]},notice:{kind:"widget",section:"notice"}},variables:{enabled:{scope:"page",type:"boolean",mode:"state",initial:true},visible:{scope:"page",type:"boolean",mode:"derived",rule:{op:"and",kind:"boolean",args:[{kind:"boolean",ref:"enabled"},{kind:"boolean",lit:{value:true}}]}}}};
 const draft=await builder.page({title:"Rule visibility",object:type,document,sections:[{id:"toggle",widget:"boolean-input",configVersion:1,title:"Enable rule",booleanVariable:"enabled",booleanLabel:"Enable rule"},{id:"notice",widget:"notice",configVersion:1,title:"Rule result",notice:{title:"Allowed",message:"The shared rule allows this section.",tone:"info"}}]});
 await builder.decide("build.page.publish",{type:"build.page",id:draft.id},{});
 const tab=await runtime(page,operator,draft.name);
 const notice=tab.getByRole("note",{name:"Rule result",exact:true});
 await expect(notice).toBeVisible();
 const toggle=tab.getByRole("switch",{name:"Enable rule",exact:true});
 await toggle.click();await expect(notice).toBeHidden();
 await toggle.click();await expect(notice).toBeVisible();
 expect(await tab.evaluate(()=>({version:(globalThis as any).platformRuleVersion,eval:typeof (globalThis as any).platformRuleEval}))).toEqual({version:"platform.expr.v1",eval:"function"});
});
