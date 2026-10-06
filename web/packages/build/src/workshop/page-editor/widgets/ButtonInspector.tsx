import { widgetContract } from "@platform/app";
import type { Api } from "@platform/kernel";
import { EventEffects, clickEffects } from "../EffectsPanel";

export type ButtonInspectorPorts = { buttons?:Api.PageButton[];onGroupChange?:(buttons:Api.PageButton[],document:Api.PageDocument)=>void;document:Api.PageDocument; section:string; owner?:string; overlay?:string; onChange:(document:Api.PageDocument)=>void };
export function ButtonInspector(props:ButtonInspectorPorts) {
  const contract=widgetContract("button")!;
  const eventName="events" in contract?contract.events[0]!.id:"";
  return <EventEffects {...props} eventName={eventName} allowed={clickEffects}/>;
}
