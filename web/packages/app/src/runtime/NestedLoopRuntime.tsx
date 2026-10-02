import type {ReactNode} from "react";
import type {Api} from "@platform/kernel";
import {Panel,t} from "@platform/ui";
import {LoopRuntime,type LoopContext} from "./LoopRuntime";
import {planKey} from "./query-plans";

/** The parent session owns this child's lifetime and state. Rendering a new
 * instance never creates a separate query interpreter or business data copy. */
export function NestedLoopRuntime({page,node,owner,parent,overlay,children}:{page:Api.Page;node:Api.PageLayoutNode;owner:string;parent:LoopContext;overlay?:string;children:(context:LoopContext)=>ReactNode}) {
 const session=parent.querySession,queries=parent.queries;
 const loop=node.loop,variable=page.document?.variables?.[loop?.collection??""],id=variable?.source?.query;
 if(!session||!queries||!loop||!id||page.document?.queries?.[id]?.itemOwner!==parent.owner)return <Panel role="alert">{t("Choose a parent-owned loop query.")}</Panel>;
 return <LoopRuntime page={page} owner={owner} queryKey={planKey(id)} expectedSignature={queries.signatures[id]} loop={loop} label={node.title||t("Repeated records")} result={queries.resources[loop.collection]} session={session} snapshot={session.snapshot()} variables={page.document?.variables??{}} resources={{...parent.values,...queries.resources}} overlay={overlay} inherited={parent.values} preserveOnUnmount>{children}</LoopRuntime>;
}
