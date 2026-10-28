import type {PageUI} from "@platform/app";
import { widgetContract } from "@platform/app";
import { EventEffects, clickEffects } from "../EffectsPanel";

export type ButtonInspectorPorts = { buttons?:PageUI.PageButton[];onGroupChange?:(buttons:PageUI.PageButton[],document:PageUI.PageDocument)=>void;document:PageUI.PageDocument; section:string; owner?:string; overlay?:string; onChange:(document:PageUI.PageDocument)=>void };
export function ButtonInspector(props:ButtonInspectorPorts) {
  const contract=widgetContract("button")!;
  const eventName="events" in contract?contract.events[0]!.id:"";
  return <EventEffects {...props} eventName={eventName} allowed={clickEffects}/>;
}
