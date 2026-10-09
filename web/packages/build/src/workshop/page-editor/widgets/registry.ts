import { createWidgetDefinitions } from "@platform/app";
import {lazyInspector} from "./lazy-inspector";
import type {ComponentType} from "react";
import type {Api} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
import type {AuthoringSection} from "../draft";

export type WidgetBindingInspectorPorts={section:AuthoringSection;sections:AuthoringSection[];document:Api.PageDocument;object:string;info?:EntityInfo;overlay?:string;itemOwner?:string;onChange:(patch:Partial<AuthoringSection>)=>void;onResourcesChange:(patch:Partial<AuthoringSection>,variables:Record<string,Api.PageVariable>)=>void};

const CollectionBuilderInspector=lazyInspector(()=>import("./CollectionBuilderInspector").then(module=>module.CollectionBuilderInspector));
const MapInspector=lazyInspector(()=>import("./SpatialInspectors").then(module=>module.MapInspector));
const AnnotationInspector=lazyInspector(()=>import("./SpatialInspectors").then(module=>module.AnnotationInspector));
const SceneInspector=lazyInspector(()=>import("./SpatialInspectors").then(module=>module.SceneInspector));
const EmbeddingInspector=lazyInspector(()=>import("./EmbeddingInspector").then(module=>module.EmbeddingInspector));
const AIInspector=lazyInspector(()=>import("./AIInspector").then(module=>module.AIInspector));
const ExternalFrameInspector=lazyInspector(()=>import("./ExternalFrameInspector").then(module=>module.ExternalFrameInspector));
const ObservationInspector=lazyInspector(()=>import("./ObservationInspector").then(module=>module.ObservationInspector));
const ActionTableInspector=lazyInspector(()=>import("./RecordWorkInspectors").then(module=>module.ActionTableInspector));
const NotepadInspector=lazyInspector(()=>import("./RecordWorkInspectors").then(module=>module.NotepadInspector));
const CollectionAnalysisInspector=lazyInspector(()=>import("./CollectionAnalysisInspector").then(module=>module.CollectionAnalysisInspector));
const ResourceListInspector=lazyInspector(()=>import("./ExplorationInspectors").then(module=>module.ResourceListInspector));
const AssetDirectoryInspector=lazyInspector(()=>import("./ExplorationInspectors").then(module=>module.AssetDirectoryInspector));
const GraphExplorerInspector=lazyInspector(()=>import("./ExplorationInspectors").then(module=>module.GraphExplorerInspector));
const VertexGraphInspector=lazyInspector(()=>import("./ExplorationInspectors").then(module=>module.VertexGraphInspector));
const BreadcrumbInspector=lazyInspector(()=>import("./ContextInspectors").then(module=>module.BreadcrumbInspector));
const BreadcrumbHomeInspector=lazyInspector(()=>import("./ContextInspectors").then(module=>module.BreadcrumbHomeInspector));
const AvatarInspector=lazyInspector(()=>import("./ContextInspectors").then(module=>module.AvatarInspector));
const ImageInspector=lazyInspector(()=>import("./ContextInspectors").then(module=>module.ImageInspector));
const HistoryInspector=lazyInspector(()=>import("./WorkViewsInspector").then(module=>module.HistoryInspector));
const WorkViewsInspector=lazyInspector(()=>import("./WorkViewsInspector").then(module=>module.WorkViewsInspector));
const CollaborationInspector=lazyInspector(()=>import("./CollaborationInspector").then(module=>module.CollaborationInspector));
const TagCountsInspector=lazyInspector(()=>import("./TagCountsInspector").then(module=>module.TagCountsInspector));
const RecordComparisonInspector=lazyInspector(()=>import("./RecordComparisonInspector").then(module=>module.RecordComparisonInspector));
const RecordCardInspector=lazyInspector(()=>import("./RecordCardInspector").then(module=>module.RecordCardInspector));
const SparklineInspector=lazyInspector(()=>import("./SparklineInspector").then(module=>module.SparklineInspector));
const TreemapInspector=lazyInspector(()=>import("./TreemapInspector").then(module=>module.TreemapInspector));
const HeatmapInspector=lazyInspector(()=>import("./HeatmapInspector").then(module=>module.HeatmapInspector));
const HistogramInspector=lazyInspector(()=>import("./HistogramInspector").then(module=>module.HistogramInspector));
const TermsInspector=lazyInspector(()=>import("./TermsInspector").then(module=>module.TermsInspector));
const SpacerInspector=lazyInspector(()=>import("./SpacerInspector").then(module=>module.SpacerInspector));
const SeparatorInspector=lazyInspector(()=>import("./SeparatorInspector").then(module=>module.SeparatorInspector));
const NoticeInspector=lazyInspector(()=>import("./NoticeInspector").then(module=>module.NoticeInspector));
const AlertInspector=lazyInspector(()=>import("./AlertInspector").then(module=>module.AlertInspector));
const RecordPickerInspector=lazyInspector(()=>import("./RecordPickerInspector").then(module=>module.RecordPickerInspector));
const DateInspector=lazyInspector(()=>import("./DateInspector").then(module=>module.DateInspector));
const ChoiceInspector=lazyInspector(()=>import("./ChoiceInspector").then(module=>module.ChoiceInspector));
const BooleanInspector=lazyInspector(()=>import("./BooleanInspector").then(module=>module.BooleanInspector));
const RangeInspector=lazyInspector(()=>import("./RangeInspector").then(module=>module.RangeInspector));
const LeaderboardInspector=lazyInspector(()=>import("./LeaderboardInspector").then(module=>module.LeaderboardInspector));
const SummaryInspector=lazyInspector(()=>import("./SummaryInspector").then(module=>module.SummaryInspector));
const GaugeInspector=lazyInspector(()=>import("./GaugeInspector").then(module=>module.GaugeInspector));
const ProgressInspector=lazyInspector(()=>import("./ProgressInspector").then(module=>module.ProgressInspector));
const RecordListInspector=lazyInspector(()=>import("./RecordListInspector").then(module=>module.RecordListInspector));
const RecordCalendarInspector=lazyInspector(()=>import("./RecordCalendarInspector").then(module=>module.RecordCalendarInspector));
const RecordGanttInspector=lazyInspector(()=>import("./RecordGanttInspector").then(module=>module.RecordGanttInspector));
const RecordEventsInspector=lazyInspector(()=>import("./RecordEventsInspector").then(module=>module.RecordEventsInspector));
const ScatterInspector=lazyInspector(()=>import("./ScatterInspector").then(module=>module.ScatterInspector));
const RecordChartInspector=lazyInspector(()=>import("./RecordChartInspector").then(module=>module.RecordChartInspector));
const HeadingInspector=lazyInspector(()=>import("./TitleInspectors").then(module=>module.HeadingInspector));
const CollectionTitleInspector=lazyInspector(()=>import("./TitleInspectors").then(module=>module.CollectionTitleInspector));
const MetricInspector=lazyInspector(()=>import("./MetricInspector").then(module=>module.MetricInspector));
const StatusTrackerInspector=lazyInspector(()=>import("./StatusTrackerInspector").then(module=>module.StatusTrackerInspector));
const RecordLinksInspector=lazyInspector(()=>import("./RecordLinksInspector").then(module=>module.RecordLinksInspector));
const FilterInspector=lazyInspector(()=>import("./FilterInspector").then(module=>module.FilterInspector));
const ButtonGroupInspector=lazyInspector(()=>import("./ButtonGroupInspector").then(module=>module.ButtonGroupInspector));
const RecordViewInspector=lazyInspector(()=>import("./RecordViewInspector").then(module=>module.RecordViewInspector));
const DetailInspector=lazyInspector(()=>import("./DetailInspector").then(module=>module.DetailInspector));
const InlineActionInspector=lazyInspector(()=>import("./InlineActionInspector").then(module=>module.InlineActionInspector));
const ChartInspector=lazyInspector(()=>import("./ChartInspector").then(module=>module.ChartInspector));
const PivotInspector=lazyInspector(()=>import("./PivotInspector").then(module=>module.PivotInspector));
const KanbanInspector=lazyInspector(()=>import("./KanbanInspector").then(module=>module.KanbanInspector));
const RecordTimelineInspector=lazyInspector(()=>import("./RecordTimelineInspector").then(module=>module.RecordTimelineInspector));
const TableInspector=lazyInspector(()=>import("./TableInspector").then(module=>module.TableInspector));
const TableSelectionInspector=lazyInspector(()=>import("./TableInspector").then(module=>module.TableSelectionInspector));
const ButtonInspector=lazyInspector(()=>import("./ButtonInspector").then(module=>module.ButtonInspector));

/** Build owns editor implementations, keyed by the shared runtime identity. */
const inspectors = createWidgetDefinitions({
 text:{configVersion:1},
 input:{configVersion:1},
 actions:{configVersion:1},
 form:{configVersion:1},
 tasks:{configVersion:1},
 function:{configVersion:1},
 compute:{configVersion:1},
 "collection-builder":{configVersion:1,bindings:CollectionBuilderInspector},
 "record-map":{configVersion:1,bindings:MapInspector},
 "image-annotation":{configVersion:1,bindings:AnnotationInspector},
 "scene-3d":{configVersion:1,bindings:SceneInspector},
 "ai-assistant":{configVersion:1,bindings:AIInspector},
 "external-frame":{configVersion:1,bindings:ExternalFrameInspector},
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
});
export function widgetInspector(id:string,version:number) {
  const implementation=inspectors.resolve(id,version);
  return implementation?.configVersion===version?{
    bindings:"bindings" in implementation?implementation.bindings:undefined,
    events:"events" in implementation?implementation.events:undefined,
  }:undefined;
}

/** Bindings receive one owner context; the registry retains per-widget props
 * checking without asking JSX to intersect every specialized component. */
export function widgetBindingInspector(id:string,version:number):ComponentType<WidgetBindingInspectorPorts>|undefined {
  return widgetInspector(id,version)?.bindings;
}

/** Unknown versions are distinct from the seven deliberately common editors. */
export function widgetInspectorStatus(id:string,version:number){
 const inspector=widgetInspector(id,version);
 return !inspector?"unsupported":inspector.bindings||inspector.events?"specialized":"common";
}
