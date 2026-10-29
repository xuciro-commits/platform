// Web-owned presentation. Query, rule and asset types remain host contracts.

export type * from "@platform/kernel/host-contract";

import type {AssetBinding, AssetRef, Binding, ModelBinding, ModelViewBinding, Expr, PageEventBinding, PageInterface, PageQuery, PageVariable} from "@platform/kernel/host-contract";

import type {ActionParameterOptions, AnalysisStepOptions, ButtonOptions, DetailPresentationOptions, EventToneOptions, LayoutSizeOptions, LeaderboardOptions, MetricAnnotationOptions, MetricPresentationOptions, RecordCardOptions, RecordEventsOptions, RecordGanttOptions, RecordLinkOptions, RecordScatterOptions, RegionPresentationOptions, SceneConfigOptions, SceneLayerOptions, SceneMappingOptions, StatusTrackerOptions, TableColumnOptions, TablePresentationOptions} from "@platform/ui";

export type Page = {
  presentationWords?: Record<string,string>;
  name: string;
  title: string;
  description?: string;
  object: AssetRef;
  layout: string;
  listFields: string[];
  detailFields: string[];
  actions: AssetRef[];
  sections?: Section[];
  document?: PageDocument;
  selections?: SelectionVariable[];
};

export type PageComposite = {
 bindings:Record<string,ModelViewBinding>;
 value?:Expr;
 tones?:Record<string,string>;
 direction?:"right"|"down";
 acyclic?:boolean;
};
export type PageGraphObject = {object:AssetRef;labelField:string};
export type PageGraphOutput = {id:string;object:AssetRef;variable:string};
export type PageGraphRelation = {id:string;binding:AssetBinding};
export type PageObservationSignal = {field:string;unit:string;group?:string};
export type PageObservationMetadata = {plant?:string;device?:string;asset?:string;status?:string};
export type PageObservation = {
 kind:string;timeField:string;signals:PageObservationSignal[];metadata?:PageObservationMetadata;
 rowHeight?:number;assetObject?:AssetRef;assetField?:string;rowOutput?:string;assetOutput?:string;averageField?:string;
};

export type PageAI = {
  kind: string;
  replyField?: string;
  questionVariable?: string;
  suggestions?: string[];
};

export type PageActionParameter = ActionParameterOptions;

export type PageActionTable = {
  parameters: PageActionParameter[];
};

export type PageAlertBanner = {
  threshold: string;
  tone: string;
  message: string;
};

export type PageAnalysisStep = AnalysisStepOptions;

export type PageAssetDirectory = {
  items: PageAssetDirectoryItem[];
};

export type PageAssetDirectoryItem = {
  id: string;
  label: string;
  asset: AssetBinding;
};

export type PageAvatarStack = {
  labelField: string;
  detailFields?: string[];
  contextVariable?: string;
  contextCollectionVariable?: string;
};

export type PageBreadcrumb = {
  homeLabel: string;
  pageLabel: string;
  labelField?: string;
};

export type PageButton = ButtonOptions;

export type PageChoiceInput = {
  clearable?: boolean;
  variant: string;
  options: string[];
  optionLabels?: string[];
  label?: string;
};

export type PageCollectionAnalysis = {
  kind: string;
  groupField?: string;
  field?: string;
  unit?: string;
  fields?: string[];
  steps?: PageAnalysisStep[];
};

export type PageCollectionBuilder = {
  fields: string[];
};

export type PageDetailPresentation = DetailPresentationOptions;

export type PageDocument = {
  unusedWidgets?: PageUnusedWidget[];
  formatVersion: number;
  uiProfile: string;
  root: string;
  nodes: Record<string, PageLayoutNode>;
  variables?: Record<string, PageVariable>;
  overlays?: Record<string, PageOverlay>;
  events?: PageEventBinding[];
  queries?: Record<string, PageQuery>;
  interface?: PageInterface;
  device?: string;
};

export type PageEmbedding = {
  readOnly?: boolean;
  kind: string;
  page: AssetBinding;
  contentVersion: string;
  interfaceVersion: number;
  inputs?: Record<string, PageValue>;
  results?: Record<string, string>;
};

export type PageEventTone = EventToneOptions;

export type PageExternalFrame = {
  url: string;
  origin: string;
  height?: number;
};

export type PageFacet = {
  field: string;
  variable: string;
  kind: string;
};

export type PageGauge = {
  max: number;
  warnAt?: number;
  label?: string;
  suffix?: string;
};

export type PageGraphExplorer = {
  objects: PageGraphObject[];
  relations: PageGraphRelation[];
  outputs?: PageGraphOutput[];
};

export type PageHistogram = {
  field: string;
  bins: number;
};

export type PageInlineEdit = {
  action: AssetRef;
  fields: string[];
};

export type PageLayoutNode = {
  slot?: string;
  presentation?: PageRegionPresentation;
  size?: PageLayoutSize;
  gap?: number;
  kind: string;
  children?: string[];
  section?: string;
  title?: string;
  valueVariable?: string;
  activeVariable?: string;
  visibleWhen?: string;
  enabledWhen?: string;
  align?: string;
  loop?: PageLoop;
};

export type PageLayoutSize = LayoutSizeOptions;

export type PageLeaderboard = LeaderboardOptions;

export type PageLoop = {
  collection: string;
  itemVariable: string;
  limit: number;
};

export type PageMetricAnnotation = MetricAnnotationOptions;

export type PageMetricPresentation = MetricPresentationOptions;

export type PageNavigation = {
  page: AssetRef;
  interfaceVersion: number;
  inputs?: Record<string, PageValue>;
  results?: Record<string, string>;
};

export type PageNeighborhoodGroup = {
  id: string;
  binding: AssetBinding;
  direction: string;
  limit: number;
  badge: string;
  tone: string;
};

export type PageNotice = {
  title?: string;
  message: string;
  tone: string;
};

export type PageOverlay = {
  presentation?: PageOverlayPresentation;
  root: string;
  kind: string;
  title: string;
  openVariable: string;
};

export type PageOverlayPresentation = {
  side: string;
  size: string;
  customWidth?: number;
  backdrop: boolean;
  closeOnBackdrop: boolean;
  closeOnEsc: boolean;
};

export type PageRangeInput = {
  min: string;
  max: string;
  step: string;
  label?: string;
  unit?: string;
};

export type PageRecordCalendar = {
  dateField: string;
  labelField: string;
  initialMonth: string;
};

export type PageRecordCard = RecordCardOptions;

export type PageRecordChart = {
  mark: string;
  xField: string;
  yField: string;
};

export type PageRecordComparison = {
  labelField: string;
};

export type PageRecordEvents = RecordEventsOptions;

export type PageRecordGantt = RecordGanttOptions;

export type PageRecordLink = RecordLinkOptions;

export type PageRecordList = {
  layout: string;
};

export type PageRecordMap = {
  latitudeField: string;
  longitudeField: string;
  labelField: string;
  colorField?: string;
  clusterEnabled: boolean;
};

export type PageRecordPicker = {
  labelField: string;
  label?: string;
};

export type PageRecordScatter = RecordScatterOptions;

export type PageRecordView = {
  tabs: string[];
};

export type PageRegionPresentation = RegionPresentationOptions;

export type PageResourceList = {
  labelField: string;
  statusField: string;
  statusTones?: PageEventTone[];
};

export type PageSceneConfig = SceneConfigOptions;

export type PageSceneLayer = SceneLayerOptions;

export type PageSceneMapping = SceneMappingOptions;

export type PageSeparator = {
  label?: string;
};

export type PageSpacer = {
  size?: number;
};

export type PageSparkline = {
  field?: string;
  label?: string;
  suffix?: string;
};

export type PageStaticImage = {
  url?: string;
  caption?: string;
  height?: number;
};

export type PageStatusTracker = StatusTrackerOptions;

export type PageTableColumn = TableColumnOptions;

export type PageTablePresentation = TablePresentationOptions;

export type PageUnusedWidget = {
  node: string;
  parent: string;
};

export type PageValue = {
  variable?: string;
  literal?: unknown;
};

export type PageVertexGraph = {
  groups: PageNeighborhoodGroup[];
};

export type Section = {
  composite?: PageComposite;
  model?: ModelBinding;
  tablePresentation?: PageTablePresentation;
  collectionBuilder?: PageCollectionBuilder;
  collectionOutputVariable?: string;
  map?: PageRecordMap;
  scene?: PageSceneConfig;
  sceneSampleCollectionVariable?: string;
  sceneSampleVariable?: string;
  scenePartVariable?: string;
  ai?: PageAI;
  externalFrame?: PageExternalFrame;
  embedding?: PageEmbedding;
  observation?: PageObservation;
  observationHistoryVariable?: string;
  observationContextVariable?: string;
  observationSignalVariable?: string;
  observationThresholdVariable?: string;
  observationRowsVariable?: string;
  observationCountVariable?: string;
  observationMeanVariable?: string;
  actionTable?: PageActionTable;
  actionDefaults?: PageActionParameter[];
  notepadVariable?: string;
  analysis?: PageCollectionAnalysis;
  analysisXVariable?: string;
  analysisYVariable?: string;
  analysisCountVariable?: string;
  analysisMeanVariable?: string;
  resourceList?: PageResourceList;
  assetDirectory?: PageAssetDirectory;
  graphExplorer?: PageGraphExplorer;
  vertexGraph?: PageVertexGraph;
  breadcrumb?: PageBreadcrumb;
  avatar?: PageAvatarStack;
  image?: PageStaticImage;
  historyLimit?: number;
  commentDraftVariable?: string;
  fileVariable?: string;
  pdfPageVariable?: string;
  recordGantt?: PageRecordGantt;
  recordCalendar?: PageRecordCalendar;
  recordEvents?: PageRecordEvents;
  recordComparison?: PageRecordComparison;
  recordSetVariable?: string;
  recordCard?: PageRecordCard;
  sparkline?: PageSparkline;
  sparklineDecimalVariable?: string;
  sparklineNumberVariable?: string;
  groupValueVariable?: string;
  groupSetVariable?: string;
  rowValueVariable?: string;
  rowSetVariable?: string;
  columnValueVariable?: string;
  columnSetVariable?: string;
  scatter?: PageRecordScatter;
  recordChart?: PageRecordChart;
  recordList?: PageRecordList;
  headingLevel?: string;
  histogram?: PageHistogram;
  inputKind?: string;
  pickerValueVariable?: string;
  recordPicker?: PageRecordPicker;
  spacer?: PageSpacer;
  separator?: PageSeparator;
  notice?: PageNotice;
  alertValueVariable?: string;
  alertBanner?: PageAlertBanner;
  dateKind?: string;
  dateOffset?: string;
  dateVariable?: string;
  dateLabel?: string;
  choiceSetVariable?: string;
  choiceVariable?: string;
  choiceInput?: PageChoiceInput;
  booleanVariant?: string;
  booleanVariable?: string;
  booleanLabel?: string;
  rangeInput?: PageRangeInput;
  rangeMinVariable?: string;
  rangeMaxVariable?: string;
  leaderboard?: PageLeaderboard;
  summaryField?: string;
  statisticsVariable?: string;
  gauge?: PageGauge;
  gaugeValueVariable?: string;
  progressLabel?: string;
  progressValueVariable?: string;
  progressTotalVariable?: string;
  progressTotal?: string;
  countVariable?: string;
  metricPresentation?: PageMetricPresentation;
  statusTracker?: PageStatusTracker;
  recordLinks?: PageRecordLink[];
  buttons?: PageButton[];
  recordView?: PageRecordView;
  detailPresentation?: PageDetailPresentation;
  tableColumns?: PageTableColumn[];
  showSearch?: boolean;
  selectionSetVariable?: string;
  inlineEdit?: PageInlineEdit;
  facets?: PageFacet[];
  filterSearchVariable?: string;
  id?: string;
  configVersion?: number;
  widget: string;
  title?: string;
  width?: string;
  object?: AssetRef;
  selection?: string;
  collectionVariable?: string;
  recordVariable?: string;
  selectionVariable?: string;
  filterVariable?: string;
  parentSelection?: string;
  relation?: string;
  query?: AssetRef&{sourceVersion?:string};
  fields?: string[];
  actions?: AssetRef[];
  group?: string;
  chartVariant?: string;
  mark?: string;
  columnGroup?: string;
  timeStart?: string;
  timeEnd?: string;
  timeLabel?: string;
  timeGroup?: string;
  cardLabel?: string;
  measure?: string;
  text?: string;
  function?: AssetBinding;
  operation?: AssetBinding;
  inputs?: Record<string, Binding>;
};

export type SelectionVariable = {
  name: string;
  object: AssetRef;
};

import type {pageUIManifest} from "./manifest";

type Contract = typeof pageUIManifest.widgets[number];
export type PageWidgetID = Contract['componentID'];
type RequiredKeys<S> = S extends {required: readonly (infer K)[]} ? K : never;
type Flatten<T> = {-readonly [K in keyof T]:T[K]};
type SchemaObject<P, R> = Flatten<{-readonly [K in keyof P as K extends R ? K : never]: SchemaValue<P[K]>}
  & {-readonly [K in keyof P as K extends R ? never : K]?: SchemaValue<P[K]>}>;
type SchemaValue<S> = S extends {enum: readonly (infer E)[]} ? E
  : S extends {type: 'object'; properties: infer P} ? SchemaObject<P, RequiredKeys<S>>
  : S extends {type: 'array'; items: infer I} ? SchemaValue<I>[]
  : S extends {type: 'boolean'} ? boolean
  : S extends {type: 'number' | 'integer'} ? number
  : S extends {type: 'string'} ? string : never;
type PortPath<P extends string> = P extends `${infer Head}.${infer Tail}` ? {[K in Head]?: PortPath<Tail>} : {[K in P]?: string};
type UnionIntersection<U> = (U extends unknown ? (item: U) => void : never) extends ((item: infer I) => void) ? I : never;
type Ports<C extends Contract> = C extends {inputPorts: infer I extends readonly unknown[]; outputPorts: infer O extends readonly unknown[]}
  ? UnionIntersection<(I[number] | O[number]) extends infer P ? P extends {bindingField: infer F extends string} ? F extends 'enabledWhen' ? {} : PortPath<F> : never : never> : {};
type ConfigurationFields<C extends Contract> = C extends {configurationFields: readonly (infer K)[]}
  ? Pick<Section, Exclude<Extract<K, keyof Section>,keyof C["propsSchema"]["properties"]>> : {};

/** The same manifest generates the source author's discriminated presentation
 * and port types. Business models and actions keep their original declarations;
 * the retained flat Section remains the installed/history wire representation. */
export type PageWidgetConfiguration = {
  [C in Contract as C['componentID']]: {widget: C['componentID']; configVersion: C['configVersion']}
    & {id?: string} & SchemaValue<C['propsSchema']> & Ports<C> & ConfigurationFields<C>
}[PageWidgetID];
export type PageWidgetConfigurationFor<K extends PageWidgetID> = Extract<PageWidgetConfiguration, {widget: K}>;
