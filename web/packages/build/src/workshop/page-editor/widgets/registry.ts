import type {PageUI} from "@platform/app";
import { createWidgetDefinitions,widgetContracts } from "@platform/app";
import {lazyInspector} from "./lazy-inspector";
import type {ComponentType} from "react";
import type {Api} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
import type {AuthoringSection} from "../draft";
import type {ButtonInspectorPorts} from "./ButtonInspector";

export type WidgetBindingInspectorPorts={section:AuthoringSection;sections:AuthoringSection[];document:PageUI.PageDocument;object:string;info?:EntityInfo;overlay?:string;itemOwner?:string;onChange:(patch:Partial<AuthoringSection>)=>void;onResourcesChange:(patch:Partial<AuthoringSection>,variables:Record<string,Api.PageVariable>)=>void};

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

const SchemaPropertyInspector=lazyInspector(()=>import("./SchemaPropertyInspector").then(module=>module.SchemaPropertyInspector));

type WidgetID=typeof widgetContracts[number]['componentID'];
type Inspectors={bindings?:ComponentType<WidgetBindingInspectorPorts>;events?:ComponentType<ButtonInspectorPorts>};
const specialized:Partial<Record<WidgetID,Inspectors>>= {
 "collection-builder":{bindings:CollectionBuilderInspector},
 "record-map":{bindings:MapInspector},
 "image-annotation":{bindings:AnnotationInspector},
 "scene-3d":{bindings:SceneInspector},
 "ai-assistant":{bindings:AIInspector},
 "external-frame":{bindings:ExternalFrameInspector},
 "embedded-page":{bindings:EmbeddingInspector},
 observation:{bindings:ObservationInspector},
 "action-table":{bindings:ActionTableInspector},
 notepad:{bindings:NotepadInspector},
 "collection-analysis":{bindings:CollectionAnalysisInspector},
 "resource-list":{bindings:ResourceListInspector},
 "asset-directory":{bindings:AssetDirectoryInspector},
 "graph-explorer":{bindings:GraphExplorerInspector},
 "vertex-graph":{bindings:VertexGraphInspector},
 breadcrumb:{bindings:BreadcrumbInspector,events:BreadcrumbHomeInspector},
 "avatar-stack":{bindings:AvatarInspector},
 "static-image":{bindings:ImageInspector},
 "approval-inbox":{bindings:WorkViewsInspector},
 "notification-feed":{bindings:WorkViewsInspector},
 timeline:{bindings:HistoryInspector},
 "record-comments":{bindings:CollaborationInspector},
 "record-uploader":{bindings:CollaborationInspector},
 "media-preview":{bindings:CollaborationInspector},
 "pdf-viewer":{bindings:CollaborationInspector},
 "tag-counts":{bindings:TagCountsInspector},
 "record-comparison":{bindings:RecordComparisonInspector},
 "record-card":{bindings:RecordCardInspector},
 "sparkline-kpi":{bindings:SparklineInspector},
 treemap:{bindings:TreemapInspector},
 heatmap:{bindings:HeatmapInspector},
 histogram:{bindings:HistogramInspector},
 "term-counts":{bindings:TermsInspector},
 "record-picker":{bindings:RecordPickerInspector},
 "alert-banner":{bindings:AlertInspector},
 "date-input":{bindings:DateInspector},
 "choice-input":{bindings:ChoiceInspector},
 "boolean-input":{bindings:BooleanInspector},
 "range-input":{bindings:RangeInspector},
 "record-leaderboard":{bindings:LeaderboardInspector},
 "summary-stats":{bindings:SummaryInspector},
 gauge:{bindings:GaugeInspector},
 progress:{bindings:ProgressInspector},
 "record-gantt":{bindings:RecordGanttInspector},
 "record-calendar":{bindings:RecordCalendarInspector},
 "record-events":{bindings:RecordEventsInspector},
 "record-scatter":{bindings:ScatterInspector},
 "record-chart":{bindings:RecordChartInspector},
 "record-list":{bindings:RecordListInspector},
 "collection-title":{bindings:CollectionTitleInspector},
 metric:{bindings:MetricInspector},
 "status-tracker":{bindings:StatusTrackerInspector},
 "record-links":{bindings:RecordLinksInspector},
 "button-group":{events:ButtonGroupInspector},
 "record-view":{bindings:RecordViewInspector},
 detail:{bindings:DetailInspector},
 filter:{bindings:FilterInspector},
 "inline-action":{bindings:InlineActionInspector},
  kanban:{bindings:KanbanInspector},
  "record-timeline":{bindings:RecordTimelineInspector},
  chart: {bindings:ChartInspector},
  pivot: {bindings:PivotInspector},
  table: {  bindings:TableInspector,events:TableSelectionInspector },
  button: {  events:ButtonInspector },
};
const inspectors=createWidgetDefinitions(Object.fromEntries(widgetContracts.map(contract=>[contract.componentID,{configVersion:contract.configVersion,...specialized[contract.componentID],...("propertyEditor" in contract&&contract.propertyEditor==="schema"?{bindings:SchemaPropertyInspector}:{})}])) as Record<WidgetID,Inspectors&{configVersion:number}>);
export function widgetInspector(id:string,version:number) {
  const implementation=inspectors.resolve(id,version);
  return implementation?.configVersion===version?{
    bindings:"bindings" in implementation?implementation.bindings:undefined,
    events:"events" in implementation?implementation.events:undefined,
  }:undefined;
}

export function widgetBindingInspector(id:string,version:number):ComponentType<WidgetBindingInspectorPorts>|undefined {
  return widgetInspector(id,version)?.bindings;
}

export function widgetInspectorStatus(id:string,version:number){
 const inspector=widgetInspector(id,version);
 return !inspector?"unsupported":inspector.bindings||inspector.events?"specialized":"common";
}
