import { useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Notice, t, routeToHash, useWorkspace, type View } from "@platform/ui";
import { useEffect } from "react";
import { BranchDecisions } from "../branches/decisions";
import { ApplicationScope } from "./application-scope";
import { WorkingState } from "../session/WorkingState";
import { resourceKinds } from "./resources";

export function ResourceIdentity({ view, params }: { view: View; params: Record<string, string> }) {
  const { open } = useWorkspace();
  const nativeType=view.id==="project" || view.id==="module" && !params.page?"build.app":resourceKinds.find(kind=>kind.view===view.id)?.type;
  const nativeID=params.page||params.id;
  const directory=useReadQuery<Api.ResourceDirectory>("/v1/resources",undefined,!!nativeType && !!nativeID && nativeID!=="new" && !nativeID.startsWith("ri."));
  const native=directory.data?.resources.find(resource=>resource.type===nativeType && resource.draft===nativeID);
  useEffect(()=>{
    if(!native || !nativeID || nativeID.startsWith("ri."))return;
    const { application: _application, page: _page, ...rest } = params;
    const canonical: Record<string,string>={...rest,id:native.id};
    history.replaceState(history.state,"",`${location.pathname}${location.search}${routeToHash({view:view.id,params:canonical})}`);
  },[native?.id,nativeID,view.id]);
  const rid = params.id?.startsWith("ri.") ? params.id : params.page?.startsWith("ri.") ? params.page : undefined;
  const query = useReadQuery<Api.ResourceEntry>(`/v1/resources/${encodeURIComponent(rid ?? "")}`, undefined, !!rid);
  useEffect(()=>{
    if(!rid || !query.data || !(params.application || params.page))return;
    const { application: _application, page: _page, ...rest } = params;
    history.replaceState(history.state,"",`${location.pathname}${location.search}${routeToHash({view:view.id,params:{...rest,id:rid}})}`);
  },[query.data?.id,rid,view.id]);
  const scoped = (resolved: Record<string,string>) => {
    const type = view.id==="project" || view.id==="module" && !resolved.page ? "build.app" : resourceKinds.find(kind=>kind.view===view.id)?.type;
    const id=resolved.page || resolved.id;
    const project=resolved.application || (view.id==="project" || view.id==="module"&&!resolved.page?resolved.id:undefined);
    const body=<ApplicationScope application={project}><BranchDecisions project={project}>{view.render(resolved)}</BranchDecisions></ApplicationScope>;
    return type && id ? <WorkingState key={`${type}/${id}`} type={type} id={id} project={project} onShared={target => { if (id === "new") open({ view: view.id, params: { ...resolved, id: target } }, { ifCurrent: { view: view.id, params: resolved }, replace: true }); }}>{body}</WorkingState> : body;
  };
  if (!rid) return <>{scoped(params)}</>;
  if (query.isLoading) return <p role="status">{t("Loading…")}</p>;
  const resource = query.data;
  if (!resource || !resource.canOpen) return <Notice tone="warning" message={t("Request project access to open this resource.")} />;
  const resolved: Record<string,string> = { ...params, id: resource.draft || params.id!, application: resource.project };
  if (resource.type==="build.page") {resolved.id=resource.project;resolved.page=resource.draft!;}
  return <>{scoped(resolved)}</>;
}
