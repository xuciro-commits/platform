// Primitive presentation props. The caller owns data bindings and execution.
export type ActionParameterOptions = {
  parameter: string;
  field: string;
};

export type AnalysisStepOptions = {
  value: string;
  label: string;
  positive: boolean;
};

export type ButtonOptions = {
  id: string;
  title: string;
  variant?: string;
  icon?: string;
};

export type DetailPresentationOptions = {
  columns: number;
  hideNull?: boolean;
  barcode?: string;
  print?: boolean;
};

export type EventToneOptions = {
  value: string;
  tone: string;
};

export type LayoutSizeOptions = {
  weight?: number;
  width?: number;
  height?: number;
  minWidth?: number;
  maxWidth?: number;
  minHeight?: number;
  maxHeight?: number;
  scroll?: string;
};

export type LeaderboardOptions = {
  valueField: string;
  labelField: string;
  limit: number;
  ascending: boolean;
};

export type MetricAnnotationOptions = {
  direction: string;
  text: string;
};

export type MetricPresentationOptions = {
  annotation?: MetricAnnotationOptions;
  prefix?: string;
  suffix?: string;
  formatter: string;
  variant: string;
  tone: string;
};

export type RecordCardOptions = {
  labelField: string;
  tone: string;
};

export type RecordEventsOptions = {
  timeField: string;
  titleField: string;
  severityField: string;
  tones: EventToneOptions[];
};

export type RecordGanttOptions = {
  startField: string;
  endField: string;
  titleField: string;
  statusField: string;
  rangeStart: string;
  rangeEnd: string;
  tones: EventToneOptions[];
};

export type RecordLinkOptions = {
  object: { app: string; kind: string; name: string };
  field: string;
  title?: string;
};

export type RecordScatterOptions = {
  xField: string;
  yField: string;
  colorField: string;
  labelField: string;
};

export type RegionPresentationOptions = {
  padding?: number;
  background?: string;
  border?: boolean;
  showHeader?: boolean;
  collapsible?: boolean;
  defaultCollapsed?: boolean;
};

export type SceneConfigOptions = {
  background: string;
  showGrid: boolean;
  quality: string;
  layers: SceneLayerOptions[];
  mappings: SceneMappingOptions[];
  sampleTimeField?: string;
  sampleAssetField?: string;
};

export type SceneLayerOptions = {
  id: string;
  name: string;
  nodes: string[];
  visible: boolean;
  opacity: number;
  wireframe: boolean;
  color?: string;
};

export type SceneMappingOptions = {
  id: string;
  node: string;
  source: string;
  field: string;
  mode: string;
  axis: string;
  inputMin: number;
  inputMax: number;
  outputMin: number;
  outputMax: number;
  threshold?: number;
  colorLow?: string;
  colorHigh?: string;
  smooth?: number;
  enabled: boolean;
};

export type StatusTrackerOptions = {
  field: string;
  stages: string[];
};

export type TableColumnOptions = {
  field: string;
  title?: string;
  width?: number;
  formatter?: string;
};

export type TablePresentationOptions = {
  density: string;
  showToolbar: boolean;
  titleTemplate?: string;
};
