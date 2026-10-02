import {HeadingInspector,CollectionTitleInspector} from "./TitleInspectors";
import {MetricInspector} from "./MetricInspector";
import {StatusTrackerInspector} from "./StatusTrackerInspector";
import {RecordLinksInspector} from "./RecordLinksInspector";
import {FilterInspector} from "./FilterInspector";
import {ButtonGroupInspector} from "./ButtonGroupInspector";
import {RecordViewInspector} from "./RecordViewInspector";
import {DetailInspector} from "./DetailInspector";
import {InlineActionInspector} from "./InlineActionInspector";
import { widgetContract } from "@platform/app";
import { ChartInspector } from "./ChartInspector";
import { PivotInspector } from "./PivotInspector";
import {KanbanInspector} from "./KanbanInspector";
import {RecordTimelineInspector} from "./RecordTimelineInspector";
import { TableInspector,TableSelectionInspector } from "./TableInspector";
import { ButtonInspector } from "./ButtonInspector";

/** Build owns editor implementations, keyed by the shared runtime identity. */
const inspectors = {
 heading:{configVersion:1,bindings:HeadingInspector},
 "collection-title":{configVersion:1,bindings:CollectionTitleInspector},
 metric:{configVersion:1,bindings:MetricInspector},
 "status-tracker":{configVersion:1,bindings:StatusTrackerInspector},
 "record-links":{configVersion:1,bindings:RecordLinksInspector},
 "button-group":{configVersion:1,events:ButtonGroupInspector},
 "record-view":{configVersion:1,bindings:RecordViewInspector},
 detail:{configVersion:1,bindings:DetailInspector},
 filter:{configVersion:1,bindings:FilterInspector},
 "inline-action":{configVersion:1,bindings:InlineActionInspector},
  kanban:{configVersion:1,bindings:KanbanInspector},
  "record-timeline":{configVersion:1,bindings:RecordTimelineInspector},
  chart: {configVersion:1,bindings:ChartInspector},
  pivot: {configVersion:1,bindings:PivotInspector},
  table: { configVersion:1, bindings:TableInspector,events:TableSelectionInspector },
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
