import {Component,Suspense,type ReactNode} from "react";
import {Panel,t} from "@platform/ui";
import {InspectorLoadError} from "./lazy-inspector";
import {widgetInspectorStatus} from "./registry";

class InspectorBoundary extends Component<{children:ReactNode},{failure?:"load"|"render"}>{
 state:{failure?:"load"|"render"}={};
 static getDerivedStateFromError(error:unknown){return {failure:error instanceof InspectorLoadError?"load":"render"};}
 render(){
  return this.state.failure?<Panel role="alert" className="text-xs text-danger">{t(this.state.failure==="load"?"The widget inspector could not be loaded. Your page draft is still here.":"The widget inspector could not be displayed. Your page draft is still here.")}</Panel>:this.props.children;
 }
}

/** The failure belongs to this selected instance, not the editor or its history. */
export function InspectorFrame({id,widget,version,part,children}:{id:string;widget:string;version:number;part:"bindings"|"events";children?:ReactNode}){
 const status=widgetInspectorStatus(widget,version);
 if(status==="unsupported")return part==="events"?null:<Panel role="alert" className="text-xs text-danger">{t("Unsupported widget configuration: {widget}, version {version}. Your page draft is preserved.",{widget,version})}</Panel>;
 if(status==="common")return part==="events"?null:<p className="text-xs text-muted">{t("This widget uses the shared properties and bindings below.")}</p>;
 return <div data-inspector-widget={widget} data-inspector-version={version}><InspectorBoundary key={`${id}/${widget}/${version}/${part}`}><Suspense fallback={<p role="status" className="text-xs text-muted">{t("Loading widget inspector…")}</p>}>{children}</Suspense></InspectorBoundary></div>;
}
