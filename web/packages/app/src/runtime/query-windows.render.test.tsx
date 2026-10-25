import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { Api } from "@platform/kernel";
import type { RecordSource } from "@platform/ui";
import { ApplicationSession } from "./ApplicationSessions";
import { PageSessionStore } from "./Session";
import { useApplicationQueries } from "./ApplicationQueries";
import { usePageQueries } from "./PageQueries";

const host=vi.hoisted(()=>({source:undefined as RecordSource|undefined,definitions:[] as Api.Definition[]}));
vi.mock("../index",()=>({useHost:()=>host,findDefinition:(definitions:Api.Definition[],ref:Api.AssetRef)=>definitions.find(definition=>definition.ref.app===ref.app&&definition.ref.kind===ref.kind&&definition.ref.name===ref.name)}));
afterEach(cleanup);
const object={app:"sample",kind:"object" as const,name:"sample.item"};
const plan:Api.PageQuery={object,limit:10,conditions:[{field:"state",op:"in",value:{literal:{kind:"string-set",values:["open","held"]}}}]};
const variable=(scope:"page"|"application"):Api.PageVariable=>({scope,type:"object-set",mode:"resource",source:{kind:"plan",query:"items"}});
function source():RecordSource {
 host.definitions=[];
 return host.source={scope:"member",entity:()=>({type:object.name,app:"sample",title:"Item",plural:"Items",display:"id",standard:[],fields:[{name:"state",title:"State",type:"choice",choices:["open","held"]}]}),get:vi.fn(),list:vi.fn(async()=>({records:[],total:0}))};
}

test("application query controls retain selected filters against the base signature and count only without their own selection",()=>{
 const session=new ApplicationSession({rows:variable("application")},{source:source(),queries:{items:plan}});
 const {result,rerender}=renderHook(()=>useApplicationQueries(session,{}));
 act(()=>result.current.windows.rows!.onChange({filters:{state:["open"]},offset:0}));rerender();
 const window=result.current.windows.rows!;
 expect(window.filters).toEqual({state:["open"]});
 expect(window.query.domain).toEqual([["state","in",["open","held"]],["state","in",["open"]]]);
 expect(window.baseQuery?.domain).toEqual([["state","in",["open","held"]]]);
 expect(window.facetQuery?.("state")?.domain).toEqual([["state","in",["open","held"]]]);
 session.clear();
});

test("an application interface window exposes the original compiler's fixed ordering",()=>{
 const from=source(),binding={ref:{app:"sample",kind:"query" as const,name:"coded"},sourceVersion:"one"};
 host.definitions=[{ref:binding.ref,version:"one",query:{interface:"sample.coded",interfaceShape:{name:"sample.coded",fields:[{name:"name",type:"text"}]},implementations:["sample.item"],limit:10}} as Api.Definition];
 const session=new ApplicationSession({rows:variable("application")},{source:from,queries:{items:{object:{app:"",kind:"",name:""},interface:"sample.coded",query:binding,limit:10}}});
 const {result}=renderHook(()=>useApplicationQueries(session,{})),window=result.current.windows.rows!;
 expect(window.query.sort).toEqual(["id"]);expect(window.sortLocked).toBe(true);
 act(()=>window.onChange({sort:["-id"]}));expect(session.reads!.snapshot().views).toEqual({});
 session.clear();
});

test("page query controls retain the same owner and fixed predicates after an enum selection",()=>{
 const from=source(),session=new PageSessionStore(from,{objects:new Map(),children:new Map(),queryParents:new Map()});
 const page:Api.Page={name:"query-samples",title:"Query samples",layout:"composed",object,listFields:[],detailFields:[],actions:[],sections:[{id:"list",widget:"table",configVersion:1,collectionVariable:"rows"}],document:{formatVersion:2,uiProfile:"platform.page.v2.103",root:"root",nodes:{root:{kind:"rows",children:["list"]},list:{kind:"widget",section:"list"}},variables:{rows:variable("page")},queries:{items:plan}}};
 const {result,rerender}=renderHook(()=>usePageQueries(page,{},session,session.snapshot()));
 act(()=>result.current.windows.items!.onChange({filters:{state:["held"]},offset:0}));rerender();
 const window=result.current.windows.items!;
 expect(window.filters).toEqual({state:["held"]});
 expect(window.query.domain).toEqual([["state","in",["open","held"]],["state","in",["held"]]]);
 expect(window.facetQuery("state")?.domain).toEqual([["state","in",["open","held"]]]);
 session.dispose();
});
