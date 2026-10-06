import { widgetContract } from "@platform/app";
import type { Api } from "@platform/kernel";
import { NavigationPanel } from "../NavigationPanel";
import { ButtonEventProperties } from "../OverlayPanel";

export type ButtonInspectorPorts = { buttons?:Api.PageButton[];onGroupChange?:(buttons:Api.PageButton[],document:Api.PageDocument)=>void;document:Api.PageDocument; section:string; owner?:string; overlay?:string; onChange:(document:Api.PageDocument)=>void };
export function ButtonInspector(props:ButtonInspectorPorts) {
  const contract=widgetContract("button")!;
  const eventName="events" in contract?contract.events[0]!.id:"";
  return <><NavigationPanel {...props} eventName={eventName}/>{!props.document.events?.some(e=>e.source===props.section&&(e.navigate||e.return))&&<ButtonEventProperties {...props} eventName={eventName}/>}</>;
}
