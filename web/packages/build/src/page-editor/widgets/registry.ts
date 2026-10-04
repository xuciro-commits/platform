import {EmbeddingInspector} from "./EmbeddingInspector";
import {ObservationInspector} from "./ObservationInspector";
import {ActionTableInspector,NotepadInspector} from "./RecordWorkInspectors";
import {CollectionAnalysisInspector} from "./CollectionAnalysisInspector";
import {ResourceListInspector,AssetDirectoryInspector,GraphExplorerInspector,VertexGraphInspector} from "./ExplorationInspectors";
import {BreadcrumbInspector,BreadcrumbHomeInspector,AvatarInspector,ImageInspector} from "./ContextInspectors";
import {HistoryInspector,WorkViewsInspector} from "./WorkViewsInspector";
import {CollaborationInspector} from "./CollaborationInspector";
import {TagCountsInspector} from "./TagCountsInspector";
import {RecordComparisonInspector} from "./RecordComparisonInspector";
import {RecordCardInspector} from "./RecordCardInspector";
import {SparklineInspector} from "./SparklineInspector";
import {TreemapInspector} from "./TreemapInspector";
import {HeatmapInspector} from "./HeatmapInspector";
import {HistogramInspector} from "./HistogramInspector";
import {TermsInspector} from "./TermsInspector";
import {SpacerInspector} from "./SpacerInspector";
import {SeparatorInspector} from "./SeparatorInspector";
import {NoticeInspector} from "./NoticeInspector";
import {AlertInspector} from "./AlertInspector";
import {RecordPickerInspector} from "./RecordPickerInspector";
import {DateInspector} from "./DateInspector";
import {ChoiceInspector} from "./ChoiceInspector";
import {BooleanInspector} from "./BooleanInspector";
import {RangeInspector} from "./RangeInspector";
import {LeaderboardInspector} from "./LeaderboardInspector";
import {SummaryInspector} from "./SummaryInspector";
import {GaugeInspector} from "./GaugeInspector";
import {ProgressInspector} from "./ProgressInspector";
import {RecordListInspector} from "./RecordListInspector";
import {RecordCalendarInspector} from "./RecordCalendarInspector";
import {RecordGanttInspector} from "./RecordGanttInspector";
import {RecordEventsInspector} from "./RecordEventsInspector";
import {ScatterInspector} from "./ScatterInspector";
import {RecordChartInspector} from "./RecordChartInspector";
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
 "embedded-page":{configVersion:1,bindings:EmbeddingInspector},
 observation:{configVersion:1,bindings:ObservationInspector},
 "action-table":{configVersion:1,bindings:ActionTableInspector},
 notepad:{configVersion:1,bindings:NotepadInspector},
 "collection-analysis":{configVersion:1,bindings:CollectionAnalysisInspector},
 "resource-list":{configVersion:1,bindings:ResourceListInspector},
 "asset-directory":{configVersion:1,bindings:AssetDirectoryInspector},
 "graph-explorer":{configVersion:1,bindings:GraphExplorerInspector},
 "vertex-graph":{configVersion:1,bindings:VertexGraphInspector},
 breadcrumb:{configVersion:1,bindings:BreadcrumbInspector,events:BreadcrumbHomeInspector},
 "avatar-stack":{configVersion:1,bindings:AvatarInspector},
 "static-image":{configVersion:1,bindings:ImageInspector},
 "approval-inbox":{configVersion:1,bindings:WorkViewsInspector},
 "notification-feed":{configVersion:1,bindings:WorkViewsInspector},
 timeline:{configVersion:1,bindings:HistoryInspector},
 "record-comments":{configVersion:1,bindings:CollaborationInspector},
 "record-uploader":{configVersion:1,bindings:CollaborationInspector},
 "media-preview":{configVersion:1,bindings:CollaborationInspector},
 "pdf-viewer":{configVersion:1,bindings:CollaborationInspector},
 "tag-counts":{configVersion:1,bindings:TagCountsInspector},
 "record-comparison":{configVersion:1,bindings:RecordComparisonInspector},
 "record-card":{configVersion:1,bindings:RecordCardInspector},
 "sparkline-kpi":{configVersion:1,bindings:SparklineInspector},
 treemap:{configVersion:1,bindings:TreemapInspector},
 heatmap:{configVersion:1,bindings:HeatmapInspector},
 histogram:{configVersion:1,bindings:HistogramInspector},
 "term-counts":{configVersion:1,bindings:TermsInspector},
 "record-picker":{configVersion:1,bindings:RecordPickerInspector},
 spacer:{configVersion:1,bindings:SpacerInspector},
 separator:{configVersion:1,bindings:SeparatorInspector},
 notice:{configVersion:1,bindings:NoticeInspector},
 "alert-banner":{configVersion:1,bindings:AlertInspector},
 "date-input":{configVersion:1,bindings:DateInspector},
 "choice-input":{configVersion:1,bindings:ChoiceInspector},
 "boolean-input":{configVersion:1,bindings:BooleanInspector},
 "range-input":{configVersion:1,bindings:RangeInspector},
 "record-leaderboard":{configVersion:1,bindings:LeaderboardInspector},
 "summary-stats":{configVersion:1,bindings:SummaryInspector},
 gauge:{configVersion:1,bindings:GaugeInspector},
 progress:{configVersion:1,bindings:ProgressInspector},
 "record-gantt":{configVersion:1,bindings:RecordGanttInspector},
 "record-calendar":{configVersion:1,bindings:RecordCalendarInspector},
 "record-events":{configVersion:1,bindings:RecordEventsInspector},
 "record-scatter":{configVersion:1,bindings:ScatterInspector},
 "record-chart":{configVersion:1,bindings:RecordChartInspector},
 "record-list":{configVersion:1,bindings:RecordListInspector},
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
