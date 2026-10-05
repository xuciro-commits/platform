import {createContext,useContext,useEffect,useRef,useMemo,useState,type ReactNode} from "react";
import {useQuery} from "@tanstack/react-query";
import {ExternalFrame,Panel,t} from "@platform/ui";
import {pageUIManifest,type Api} from "@platform/kernel";
const limits=pageUIManifest.runtime.embedding;
import {useHost} from "../index";
import {ComposedPage} from "../sections";
import {checkPortValues,navigationValues,readPageEnvelope} from "../runtime/page-values";
import type {VariableResult} from "../runtime/variables";
import {PageEmbeddingBudget,pageEmbeddingCost} from "../runtime/embedding-budget";

const EmbeddingDepth=createContext(0);
const BudgetContext=createContext<PageEmbeddingBudget|undefined>(undefined);
export function ExternalDocumentRenderer({config,title,readCurrent=true}:{config?:Api.PageExternalFrame;title?:string;readCurrent?:boolean}){const budget=useContext(BudgetContext);if(!budget?.valid)return <Panel role="alert">{t("Embedded page instances or reads exceed their shared budget.")}</Panel>;return config?<ExternalFrame key={JSON.stringify(config)} config={config} title={title||t("External document")} active={readCurrent}/>:null;}
export function PageEmbeddingBoundary({page,children}:{page:Api.Page;children:ReactNode}){const parent=useContext(BudgetContext),budget=useMemo(()=>new PageEmbeddingBudget({instances:limits.maxInstances,queries:limits.maxQueries,records:limits.maxRecords,sections:limits.maxSections},pageEmbeddingCost(page)),[page]);return <BudgetContext.Provider value={parent??budget}>{children}</BudgetContext.Provider>;}
type Props={config?:Api.PageEmbedding;title?:string;values:Record<string,VariableResult>;scope:string;live:boolean;enabled?:boolean;readCurrent?:boolean;onReturn?:(values:Record<string,unknown>)=>void};
/** Each original child owns its existing PageSession. Parent values only cross
 * the original interface; no child observes another instance's private state. */
export function EmbeddedPageRenderer(props:Props){const host=useHost();return <EmbeddedSurface key={JSON.stringify([props.scope,host.source.scope,props.config,Object.entries(props.config?.inputs??{}).map(([id,v])=>[id,v.variable?props.values[v.variable]:v.literal])])} {...props}/>;}
function EmbeddedSurface({config,title,values,scope,live,enabled=true,readCurrent=true,onReturn}:Props){
 const {client,source}=useHost(),depth=useContext(EmbeddingDepth),budget=useContext(BudgetContext),e=config,lease=useRef(true),activity=useRef({enabled,readCurrent,onReturn}),owner=useRef({}),envelope=useRef<{key:string;value:{version:number;values:Record<string,unknown>}}|undefined>(undefined),[reserved,setReserved]=useState(false);activity.current={enabled,readCurrent,onReturn};useEffect(()=>{lease.current=true;return()=>{lease.current=false;};},[]);
 const result=useQuery({queryKey:["page-content",source.scope,e?.page,e?.contentVersion],enabled:!!e&&depth<limits.maxDepth&&readCurrent,queryFn:()=>client.pageContent(e!.page.ref,e!.contentVersion)});
 useEffect(()=>{if(!result.data?.page||!budget)return;const granted=budget.reserve(owner.current,pageEmbeddingCost(result.data.page));setReserved(granted);return()=>{budget.release(owner.current);setReserved(false);};},[result.data?.page,budget]);
 if(!e||depth>=limits.maxDepth)return <Panel role="alert">{t("Embedded page depth or bindings are unavailable.")}</Panel>;
 if(!readCurrent||result.isPending)return <p role="status">{t("Loading original embedded page…")}</p>;
 if(result.isError||!result.data?.page||result.data.version!==e.page.sourceVersion||result.data.contentVersion!==e.contentVersion)return <Panel role="alert">{t("The exact embedded page is unavailable.")}</Panel>;
 if(!reserved)return <Panel role="alert">{t("Embedded page instances or reads exceed their shared budget.")}</Panel>;
 const page=result.data.page,iface=page.document?.interface;let inputs:Record<string,unknown>;try{inputs=navigationValues(e.inputs??{},values);}catch{return <p role="status">{t("Embedded page inputs are awaiting their original values.")}</p>;}
 if((iface?.version??0)!==e.interfaceVersion||checkPortValues(iface?.inputs??{},inputs))return <Panel role="alert">{t("Embedded page inputs do not match the original interface.")}</Panel>;
 const key=JSON.stringify([e.interfaceVersion,inputs]);if(envelope.current?.key!==key)envelope.current={key,value:{version:e.interfaceVersion,values:inputs}};
 const input=envelope.current.value,returnValue=(raw:unknown)=>{if(!lease.current||!activity.current.enabled||!activity.current.readCurrent)return;const answer=readPageEnvelope(raw);if(!answer||answer.version!==e.interfaceVersion||checkPortValues(iface?.outputs??{},answer.values))return;activity.current.onReturn?.(answer.values);};
 return <div className="min-w-0" aria-label={title||t("Embedded page")}><EmbeddingDepth.Provider value={depth+1}><ComposedPage page={page} definitionKey={JSON.stringify([scope,e.contentVersion,input])} pageCall={{input,returnValue}} live={live&&enabled&&!e.readOnly}/></EmbeddingDepth.Provider></div>;
}
