import { widgetContract } from "@platform/app";
import { PivotInspector } from "./PivotInspector";
import { TableInspector } from "./TableInspector";
import { ButtonInspector } from "./ButtonInspector";

/** Build owns editor implementations, keyed by the shared runtime identity. */
const inspectors = {
  pivot: {configVersion:1,bindings:PivotInspector},
  table: { configVersion:1, bindings:TableInspector },
  button: { configVersion:1, events:ButtonInspector },
};
for(const [id,implementation] of Object.entries(inspectors)) {
  if(widgetContract(id)?.configVersion!==implementation.configVersion)throw new Error(`Unsupported widget inspector: ${id}`);
}
export function widgetInspector(id:string,version:number) {
  const implementation=inspectors[id as keyof typeof inspectors];
  return implementation?.configVersion===version?{
    bindings:"bindings" in implementation?implementation.bindings:undefined,
    events:"events" in implementation?implementation.events:undefined,
  }:undefined;
}
