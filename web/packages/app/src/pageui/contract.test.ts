import {expect,test} from "vitest";
import {presentPage,widgetDeclarationIssues} from "./contract";
import {setLanguage} from "@platform/ui";

test("only manifest-declared text uses the resolved owner dictionary",()=>{
 setLanguage("zh-CN");
 const page={presentationWords:{"Instructions":"操作说明","Open items":"未完成项","zero":"零"},sections:[
  {id:"head",widget:"heading",configVersion:1,title:"Instructions",text:"Open items",headingLevel:"h2"},
  {id:"choose",widget:"choice-input",configVersion:1,choiceVariable:"value",choiceInput:{variant:"steps",options:["0"],optionLabels:["zero"],label:"Instructions"}},
 ],document:{nodes:{root:{kind:"rows",title:"Instructions"}},variables:{record:{initial:"Open items"}}}};
 const original=JSON.stringify(page),shown=presentPage(page);
 expect(shown.sections[0]).toMatchObject({title:"操作说明",text:"未完成项",id:"head",widget:"heading"});
 expect(shown.sections[1]).toMatchObject({choiceVariable:"value",choiceInput:{options:["0"],optionLabels:["零"],label:"操作说明"}});
 expect(shown.document.variables.record.initial).toBe("Open items");
 expect(shown.document.nodes.root.title).toBe("操作说明");
 expect(JSON.stringify(page)).toBe(original);
 setLanguage("en");
 expect(presentPage({...page,presentationWords:undefined})).toEqual({...page,presentationWords:undefined});
});

test("presentation ownership keeps zero values and rejects explicit foreign configuration",()=>{
 expect(widgetDeclarationIssues({sections:[{widget:"heading",configVersion:1,text:"Instructions",headingLevel:"h2",fields:[],actions:[]}]})).toEqual([]);
 expect(widgetDeclarationIssues({sections:[{widget:"heading",configVersion:1,text:"Instructions",notice:{tone:"info",message:"Foreign"}}]})).not.toEqual([]);
});

import {pagePresentationIssues} from "./contract";
import {pageLayoutDiagnostics} from "../pages/layout";
import {decodePage,encodePage} from "./wire";
import type * as UI from "./types";
const pageDocument=():UI.PageDocument=>({formatVersion:2,uiProfile:"platform.page.v2.111",root:"root",nodes:{root:{kind:"rows",children:["leaf"]},leaf:{kind:"widget",section:"one"}}});
const heading=()=>({id:"one",widget:"heading",configVersion:1,text:"Instructions",headingLevel:"h2"});

test.each([
 ["heading",{text:"   "}],
 ["gauge",{gauge:{max:0}}],
 ["range-input",{rangeInput:{min:"0",max:"10",step:"0"}}],
 ["choice-input",{choiceInput:{variant:"tabs",options:["1"],optionLabels:["Wrong index"]}}],
 ["date-input",{dateKind:"datetime",dateOffset:"+25:00"}],
 ["record-calendar",{recordCalendar:{dateField:"day",labelField:"name",initialMonth:"2026-13"}}],
 ["record-gantt",{recordGantt:{rangeStart:"2026-02-28",rangeEnd:"2026-02-01"}}],
 ["static-image",{image:{url:"javascript:alert(1)",caption:"Image"}}],
 ["external-frame",{externalFrame:{url:"https://one.example.test/report",origin:"https://another.example.test"}}],
 ["record-events",{recordEvents:{timeField:"created",titleField:"name",severityField:"state",tones:[{value:"same",tone:"info"},{value:"same",tone:"warning"}]}}],
 ["progress",{progressTotal:"0"}],
 ["table",{tablePresentation:{titleTemplate:"{count} {count}"}}],
]as const)("%s presentation rejects invalid visual configuration",(widget,props)=>{
 expect(widgetDeclarationIssues({sections:[{id:"one",configVersion:1,widget,...props}]}).length).toBeGreaterThan(0);
});

test("profile gates stay independent of component versions",()=>{
 const sections=[{...heading(),widget:"metric",text:undefined,headingLevel:undefined,metricPresentation:{annotation:{text:"Before"}}}];
 expect(pagePresentationIssues({sections,document:{...pageDocument(),uiProfile:"platform.page.v2.1"}}).length).toBeGreaterThan(0);
 expect(pagePresentationIssues({sections,document:pageDocument()})).toEqual([]);
});

test("layout validation owns visual budgets, tree identity and weight constraints",()=>{
 const doc=pageDocument();expect(pageLayoutDiagnostics(doc,[heading()])).toEqual([]);
 const cycle=structuredClone(doc);cycle.nodes.leaf!.children=["root"];
 expect(pageLayoutDiagnostics(cycle,[heading()]).some(issue=>issue.code.includes("bounded tree"))).toBe(true);
 const fractional=structuredClone(doc);fractional.nodes.leaf!.size={width:64.5};
 expect(pageLayoutDiagnostics(fractional,[heading()]).some(issue=>issue.code.includes("size budget"))).toBe(true);
 const weight=structuredClone(doc);weight.nodes.leaf!.size={weight:1};
 expect(pageLayoutDiagnostics(weight,[heading()]).some(issue=>issue.code.includes("definite height"))).toBe(true);
 const collapsed=structuredClone(doc);collapsed.nodes.root!.presentation={defaultCollapsed:true};
 expect(pageLayoutDiagnostics(collapsed,[heading()]).some(issue=>issue.code.includes("visible collapse title"))).toBe(true);
});

test("new format roundtrips typed capabilities, exact versions and pure presentation",()=>{
 const page:UI.Page={name:"sample",title:"Sample",layout:"composed",object:{app:"build",kind:"object",name:"build.sample"},listFields:[],detailFields:[],actions:[],document:pageDocument(),sections:[{id:"one",widget:"external-frame",configVersion:1,actions:[],externalFrame:{url:"https://example.test/report",origin:"https://example.test",height:240}}]};
 const wire=encodePage(page),original=JSON.stringify(wire),section=(wire.widgetSpec!.body as {sections:Record<string,unknown>[]}).sections[0]!;
 expect(wire.format).toBe(3);expect(section.actions).toBeUndefined();expect(section.externalFrame).toEqual({height:240});
 const shown=decodePage(wire);
 expect(shown.sections?.[0]?.actions).toEqual([]);expect(shown.sections?.[0]?.externalFrame).toEqual(page.sections?.[0]?.externalFrame);
 expect(shown.document?.nodes.leaf?.section).toBe("one");expect(JSON.stringify(wire)).toBe(original);
});

test("opaque presentation cannot invent reads, actions, resource addresses or scope variables",()=>{
 const wire=encodePage({name:"sample",title:"Sample",layout:"composed",object:{app:"build",kind:"object",name:"build.sample"},listFields:[],detailFields:[],actions:[],document:pageDocument(),sections:[heading()]});
 const body=wire.widgetSpec!.body as {sections:Record<string,unknown>[];document:UI.PageDocument};
 Object.assign(body.sections[0]!,{object:{app:"private",kind:"object",name:"secrets"},actions:[{app:"system",kind:"action",name:"control"}],function:{ref:{app:"ai",kind:"function",name:"hidden"}},recordVariable:"secret",externalFrame:{url:"https://evil.test",origin:"https://evil.test"}});
 body.document.nodes.leaf!.visibleWhen="secret";body.document.variables={secret:{type:"boolean",mode:"state",scope:"page",initial:true}};
 const shown=decodePage(wire),section=shown.sections![0]!;
 expect(section.object?.name).toBe("build.sample");expect(section.actions).toBeUndefined();expect(section.function).toBeUndefined();expect(section.recordVariable).toBeUndefined();expect(section.externalFrame?.url).toBeUndefined();
 expect(shown.document?.nodes.leaf?.visibleWhen).toBe("");expect(shown.document?.variables).toBeUndefined();
});

test("a detail cannot consume a table filter or produce its selection port",()=>{
 expect(widgetDeclarationIssues({sections:[{id:"detail",widget:"detail",configVersion:1,filterVariable:"shared_filter"}]}).map(issue=>issue.path)).toContainEqual(["sections","0","filterVariable"]);
 expect(widgetDeclarationIssues({sections:[{id:"detail",widget:"detail",configVersion:1,selectionVariable:"selected_record"}]}).map(issue=>issue.path)).toContainEqual(["sections","0","selectionVariable"]);
 expect(widgetDeclarationIssues({sections:[{id:"table",widget:"table",configVersion:1,filterVariable:"shared_filter",selectionVariable:"selected_record"}]})).toEqual([]);
});


test("overlay and unused widgets retain their original local scope parents",()=>{
 const document=pageDocument();
 document.nodes.overlayRoot={kind:"rows",children:["overlayLeaf"]};
 document.nodes.overlayLeaf={kind:"widget",section:"overlayText"};
 document.nodes.unusedLeaf={kind:"widget",section:"unusedText"};
 document.overlays={dialog:{root:"overlayRoot",kind:"modal",title:"Details",openVariable:"dialogOpen"}};
 document.unusedWidgets=[{node:"unusedLeaf",parent:"overlayRoot"}];
 document.variables={dialogOpen:{type:"boolean",mode:"state",scope:"page",initial:false},local:{type:"string",mode:"state",scope:"overlay",owner:"dialog",initial:""}};
 const page:UI.Page={name:"sample",title:"Sample",layout:"composed",object:{app:"build",kind:"object",name:"build.sample"},listFields:[],detailFields:[],actions:[],document,sections:[heading(),{id:"overlayText",widget:"input",configVersion:1},{id:"unusedText",widget:"input",configVersion:1}]};
 document.nodes.overlayLeaf!.valueVariable="local";
 document.nodes.unusedLeaf!.valueVariable="local";
 const wire=encodePage(page),scopes=wire.scopeVisibility!.scopes;
 expect(scopes.find(scope=>scope.id==="overlayRoot")?.parent).toBe("dialog");
 expect(scopes.find(scope=>scope.id==="overlayLeaf")?.parent).toBe("overlayRoot");
 expect(scopes.find(scope=>scope.id==="unusedLeaf")).toMatchObject({parent:"overlayRoot",inactive:true});
 const restored=decodePage(wire);
 expect(restored.document?.nodes.overlayRoot?.children).toEqual(["overlayLeaf"]);
 expect(restored.document?.nodes.unusedLeaf?.valueVariable).toBe("local");
 expect(restored.document?.unusedWidgets).toEqual(document.unusedWidgets);
});


test("current access projection removes unavailable widgets without blocking the remaining page",()=>{
 const document=pageDocument();
 document.nodes.root!.children!.push("private-group");
 document.nodes["private-group"]={kind:"columns",children:["private-leaf"]};
 document.nodes["private-leaf"]={kind:"widget",section:"private"};
 const wire=encodePage({name:"sample",title:"Sample",layout:"composed",object:{app:"build",kind:"object",name:"build.sample"},listFields:[],detailFields:[],actions:[],document,sections:[heading(),{...heading(),id:"private"}]});
 const original=JSON.stringify(wire.widgetSpec);
 wire.dataBindings=wire.dataBindings!.filter(binding=>binding.id!=="private");
 wire.scopeVisibility!.scopes=wire.scopeVisibility!.scopes.filter(scope=>scope.id!=="private-leaf");
 const shown=decodePage(wire);
 expect(shown.sections?.map(section=>section.id)).toEqual(["one"]);
 expect(shown.document?.nodes.root?.children).toEqual(["leaf"]);
 expect(shown.document?.nodes["private-leaf"]).toBeUndefined();
 expect(shown.document?.nodes["private-group"]).toBeUndefined();
 expect(pagePresentationIssues(shown)).toEqual([]);
 expect(JSON.stringify(wire.widgetSpec)).toBe(original);
 wire.dataBindings=[];wire.scopeVisibility!.scopes=wire.scopeVisibility!.scopes.filter(scope=>scope.id!=="leaf");
 expect(pagePresentationIssues(decodePage(wire))).toEqual([]);
});
