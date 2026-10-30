import type { AuthorityDeclarationJson } from "./platform/kernel/v1alpha1/authority_pb";
import type { ChangeRecordJson, SubmissionJson } from "./platform/kernel/v1alpha1/change_pb";
import type { EntityRefJson } from "./platform/kernel/v1alpha1/identity_pb";

export type AIApplicationTotal = {
  resource?: AssetRef;
  application: AssetBinding;
  calls: number;
  sharedCalls: number;
  input: number;
  output: number;
  cost: number;
  costUnknown: number;
};

export type AIFunction = {
  proposal?: ProposalPolicy;
  conversation?: boolean;
  name: string;
  title: string;
  description: string;
  object: string;
  fields: string[];
  instructions: string;
  output: Field[];
  model?: string;
  modelOptions?: ModelOptions;
  maxInputBytes: number;
  maxOutputBytes: number;
  maxTokens: number;
  roles: string[];
};

export type AIModelBinding = {
  stage?: string;
  policyRevision?: string;
  providerOrigin?: string;
  inferenceLocation?: string;
  purpose?: string;
  model: string;
  options: ModelOptions;
  revision?: number;
  capabilities?: ModelCapabilities;
  provider?: Provider;
};

export type AIQuota = {
  reservedTokens: number;
  unsettledCalls: number;
  dailyTokens: number;
  usedTokens: number;
  perMinute: number;
  usedCalls: number;
};

export type AIReservation = {
  requestHash?: string;
  call: string;
  at: string;
  member: string;
  agent?: boolean;
  model: string;
  run?: string;
  input: number;
  output: number;
  accounted?: number;
  state: string;
};

export type AITrainingCandidate = {
  authorization?: string;
  actionDefinition?: string;
  modelConfig?: string;
  target?: string;
  expectedSources?: Record<string, number>;
  id: string;
  tenant: string;
  intent: string;
  run: string;
  step: number;
  app: string;
  action: string;
  definition: string;
  sources: Record<string, number>;
  sourceFields?: Record<string, string[]>;
  model?: string;
  policyRevision?: string;
  evidence: string;
  at: string;
  policy?: string;
  policyVersion?: number;
  outcome: string;
  chosen?: Record<string, unknown>;
  rejected?: Record<string, unknown>;
  critique?: string;
  qualityEvidence?: string;
  approved: boolean;
  revoked: boolean;
  problems?: string[];
};

export type AITrainingExport = {
  id: string;
  format: string;
  jsonl: string;
  manifest: unknown;
};

export type AITrainingPolicy = {
  id: string;
  revision: number;
  enabled: boolean;
  effectiveAt: string;
  fields: string[];
  trainingAllowed: boolean;
  providerAllowed: boolean;
  qualityRule?: string;
  retainDays: number;
};

export type AITrainingReport = {
  policies: AITrainingPolicy[];
  candidates: AITrainingCandidate[];
};

export type AIUsage = {
  reservations?: AIReservation[];
  applications?: AIApplicationTotal[];
  calls: Usage[];
  totals: Total[];
};

export type AccessConfig = {
  roles: CustomRole[];
  policies: PolicyRecord[];
  teams: Team[];
};

export type Account = {
  member: string;
  displayName?: string;
  givenName?: string;
  familyName?: string;
  title?: string;
  pronouns?: string;
  email?: string;
  phone?: string;
  language?: string;
  timezone?: string;
  dateFormat?: string;
  numberFormat?: string;
  weekStart?: string;
  inApp?: boolean;
  mail?: boolean;
  digest?: string;
  quietFrom?: string;
  quietTo?: string;
  homePage?: string;
  theme?: string;
  density?: string;
  lastSeen?: string;
  effective: {
    language: string;
    timezone: string;
    dateFormat: string;
    numberFormat: string;
    weekStart: string;
    email: string;
    digest: string;
  };
  subjects: string[];
};

export type Action = {
  adoption?: ProposalAdoption;
  schemaVersion?: SchemaVersion;
  schema: string;
  target: string;
  capability: string;
  title: string;
  description: string;
  payload: Field[];
  uses?: string[];
  needsApproval?: boolean;
  new?: boolean;
  idPrefix?: string;
  automation?: boolean;
  confirmationOnly?: boolean;
};

export type AgentInfo = {
  id: string;
  app: string;
  title: string;
  instructions: string;
  tools: string[];
  budget: Budget;
};

export type AgentOverview = {
  member: string;
  title: string;
  outside?: boolean;
  suspended: boolean;
  runs: number;
  live: number;
  actions: number;
  calls: number;
  tokens: number;
  cost: number;
  judged: Record<string, number>;
};

export type AgentReleaseDescriptor = {
  name: string;
  title: string;
  description: string;
  model?: string;
  modelOptions?: ModelOptions;
  instructions: string;
  tools: string[];
  toolBindings?: Record<string, AssetRef>;
  budget: Budget;
  checkpoints?: string[];
  handoffRole?: string;
  cases?: unknown;
  native?: boolean;
  guard?: boolean;
  handoff?: boolean;
};

export type AgentRunRecord = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  scopes?: string[];
  reviewers?: string[];
  toolOnly?: boolean;
  handoff?: string;
  applications?: AssetBinding[];
  release?: string;
  definitionVersion?: string;
  agent: string;
  title: string;
  goal: string;
  ref?: string;
  seen?: string;
  onBehalf?: string;
  acts?: boolean;
  language?: string;
  flow?: string;
  token?: number;
  step?: string;
  state: "running" | "waiting" | "done" | "stopped";
  model?: string;
  modelBinding?: AIModelBinding;
  protocol?: Record<string, unknown>;
  steps: RunStep[];
  pending?: ToolCall[];
  stepsUsed: number;
  tokensUsed: number;
  actionsUsed: number;
  cost?: number;
  result?: string;
  task?: string;
  stopped?: string;
  proposals?: Draft[];
  draft?: Draft[];
  citations?: Citation[];
  signals?: Signal[];
  withheld?: boolean;
};

export type Aggregate = {
  source: string;
  field?: string[];
};

export type AggregateWindowEdge = {
  id: string;
  revision: number;
  time: string;
};

export type AggregateWindowQuery = {
  timeField: string;
  field: string;
  rows: number;
  threshold: string;
};

export type AggregateWindowResult = {
  timeField: string;
  field: string;
  requestedRows: number;
  threshold: string;
  generation: string;
  total: number;
  timed: number;
  missingTime: number;
  count: number;
  valid: number;
  missing: number;
  above: number;
  min?: number;
  mean?: number;
  max?: number;
  first?: AggregateWindowEdge;
  last?: AggregateWindowEdge;
};

export type AlertRule = {
  name: string;
  title: string;
  object: AssetRef;
  field: string;
  operator: string;
  value?: string;
  message: string;
  role?: string;
  every?: boolean;
  active: boolean;
};

export type AppConcept = {
  id: string;
  title: string;
  assets: AssetRef[];
};

export type AppEntry = {
  id: string;
  title: string;
  role: string;
};

export type AppGroup = {
  title: string;
  pages: string[];
};

export type AppInfo = {
  id: string;
  title: string;
  version: string;
  reads: string[];
  roles: string[];
  capabilities: CapabilityInfo[];
  inputs: string[];
  uses: string[];
  subscribes: string[];
  provides: string[];
  consumes: string[];
  emits: EffectKind[];
  interfaces: Interface[];
};

export type AppPermissions = {
  app: string;
  title: string;
  reads: string[];
  everyone: string[];
  roles: RolePermissions[];
};

export type AppSettings = {
  app: string;
  settings: SettingValue[];
};

export type Application = {
  languages?: Record<string, Record<string, string>>;
  profile?: ApplicationProfile;
  header?: ApplicationHeader;
  uiProfile?: string;
  variables?: Record<string, PageVariable>;
  queries?: Record<string, PageQuery>;
  extends?: AssetRef;
  entry?: string;
  replacements?: ApplicationPageReplacement[];
  name: string;
  title: string;
  description?: string;
  icon?: string;
  pages: string[];
  groups?: AppGroup[];
  resources?: AssetRef[];
  concepts?: AppConcept[];
};

export type ApplicationAI = {
  resource: AssetBinding;
  placement: string;
  model?: string;
  handoffRole?: string;
  adoptions: string[];
  checkpoints: string[];
  evaluations: ApplicationAIEvaluation[];
  calls: number;
  sharedCalls: number;
  input: number;
  output: number;
  cost: number;
  costUnknown: number;
};

export type ApplicationAIEvaluation = {
  id: string;
  candidate: string;
  state: string;
  model: string;
  quality: number;
  costUsd: number;
};

export type ApplicationAgentBudget = {
  resource: AssetBinding;
  budget: Budget;
};

export type ApplicationArtifact = {
  samples: AssetBinding[];
  ref: AssetRef;
  version: string;
  title: string;
  profile?: ApplicationProfile;
  model: AssetBinding[];
  rules: AssetBinding[];
  pages: AssetBinding[];
  flows: AssetBinding[];
  ai: AssetBinding[];
  integrations: AssetBinding[];
  roles: ApplicationRole[];
  depends: AssetBinding[];
  environment: ApplicationBinding[];
};

export type ApplicationBinding = {
  ref: AssetRef;
  kind: string;
  name?: string;
  credential?: boolean;
};

export type ApplicationData = {
  ref: AssetRef;
  records: number;
  expected?: number;
};

export type ApplicationDecisionTrace = {
  change: string;
  schema: string;
  at: string;
  content?: SchemaVersion;
  versions: ApplicationVersion[];
  definition?: DecisionDefinition;
};

export type ApplicationDiagnostic = {
  application: AssetRef;
  type: string;
  record: string;
  decisions: ApplicationDecisionTrace[];
  runs: ApplicationRun[];
};

export type ApplicationExtension = {
  resource: AssetBinding;
  base: string;
  source?: AssetBinding;
};

export type ApplicationHeader = {
  variant: string;
  title: string;
  logo?: string;
  collapsed?: boolean;
  items: ApplicationHeaderItem[];
};

export type ApplicationHeaderItem = {
  kind: string;
  pages?: string[];
  label?: string;
  text?: string;
  action?: string;
};

export type ApplicationMaintenance = {
  extensions: ApplicationExtension[];
  retention: ApplicationRetention[];
};

export type ApplicationMonitoring = {
  businessRefusals: number;
  refusalCodes: RefusalCodeCount[];
  accepted: number;
  refused: number;
  constraintFailures: number;
  refusalRate: number;
  runs: ApplicationRunSummary;
  memberQuota?: AIQuota;
  agentBudgets: ApplicationAgentBudget[];
};

export type ApplicationPage = {
  name: string;
  title: string;
  description?: string;
  object?: string;
};

export type ApplicationPageReplacement = {
  view: string;
  page: string;
};

export type ApplicationProfile = {
  expectedRecords?: Record<string, number>;
  expectedRuns?: number;
  retention?: Record<string, string>;
  rpo?: string;
  rto?: string;
};

export type ApplicationRecovery = {
  state: string;
  predecessors: string;
  quarantined: boolean;
  scope: string;
  pendingEffects: number;
  failedRuns: number;
};

export type ApplicationRetention = {
  category: string;
  duration: string;
  state: string;
};

export type ApplicationRole = {
  resource: AssetBinding;
  role: string;
  scope?: string;
  readFields?: string[];
  writeFields?: string[];
};

export type ApplicationRun = {
  applications?: AssetBinding[];
  subject?: string;
  id: string;
  kind: string;
  title: string;
  resource?: AssetRef;
  resourceName: string;
  state: string;
  version?: string;
  release?: string;
  dependencies?: string;
  module?: string;
  node?: string;
  error?: string;
  association: string;
  shared: string[];
  created: string;
  aiTokens?: number;
  aiUsd?: number;
  costReported?: boolean;
};

export type ApplicationRunPage = {
  application: AssetRef;
  title: string;
  runs: ApplicationRun[];
  total: number;
  summary: ApplicationRunSummary;
};

export type ApplicationRunSummary = {
  failed: number;
  pendingEffects: number;
  waitingFlows: number;
  aiTokens: number;
  aiUsd: number;
  unmeteredAI: number;
};

export type ApplicationRuntime = {
  ai: ApplicationAI[];
  artifact: ApplicationArtifact;
  state: string;
  scope: string;
  versions: ApplicationVersion[];
  previous?: string;
  data: ApplicationData[];
  runs: number;
  warnings: ApplicationWarning[];
  recovery: ApplicationRecovery;
  monitoring: ApplicationMonitoring;
  maintenance: ApplicationMaintenance;
};

export type ApplicationVersion = {
  version: string;
  release?: string;
  at?: string;
  active: boolean;
};

export type ApplicationView = {
  app: string;
  name: string;
  title: string;
  description?: string;
  icon?: string;
  source: string;
  pages: ApplicationPage[];
};

export type ApplicationWarning = {
  kind: string;
  resource?: string;
  actual: number;
  expected: number;
};

export type ApprovalRequest = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  action: string;
  title: string;
  app: string;
  target: string;
  requester: string;
  submission: string;
  level: number;
  levels: ApprovalStep[];
  state: "pending" | "approved" | "rejected" | "refused" | "withdrawn" | "redirected";
  outcome?: string;
  rejectedBy?: string;
  release?: string;
  redirectAction?: string;
  redirectRelease?: string;
  redirectTo?: string;
  redirectedBy?: string;
};

export type ApprovalStep = {
  title: string;
  approvers: string[];
  all?: boolean;
  count?: number;
  approved: string[];
  due?: string;
  delegates?: Record<string, string>;
  decidedBy?: Record<string, string>;
};

export type AssetBinding = {
  ref: AssetRef;
  sourceVersion: string;
};

export type AssetRef = {
  app: string;
  kind: string;
  name: string;
};

export type AuditEntry = {
  at: string;
  member: string;
  app: string;
  action: string;
  target?: string;
};

export type AuthorityDeclaration = AuthorityDeclarationJson;

export type AuthzCollection = {
  decision: Verdict;
  elements: AuthzElement[];
  filtered?: Verdict;
};

export type AuthzElement = {
  record: string;
  decision: Verdict;
};

export type AuthzRequest = {
  principal: string;
  tenant: string;
  kind: "read" | "write" | "create" | "archive" | "action" | "derive" | "export" | "protocol";
  type?: string;
  record?: string;
  fields?: string[];
  action?: string;
  from?: string;
};

export type Binding = {
  source: "literal" | "input" | "subject" | "step" | "item" | "index" | "answer";
  step?: string;
  path?: string[];
  value?: unknown;
};

export type BlockPort = {
  id: string;
  title: string;
  direction: string;
  channel: string;
  type: string;
};

export type Breaker = {
  destination: string;
  state: string;
  failures: number;
  until?: string;
};

export type Budget = {
  Steps: number;
  Tokens: number;
  Actions: number;
  Cost: number;
};

export type BuildProject = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  members?: ProjectMember[];
  assets?: ProjectAsset[];
};

export type BusinessInputChange = {
  field: string;
  before?: unknown;
  after?: unknown;
};

export type BusinessRefusalEntry = {
  key: string;
  app: string;
  type: string;
  id: string;
  action: string;
  member: string;
  at: string;
  category: string;
  code: string;
  message: string;
  state: string;
  inputs?: unknown;
  issues?: FieldIssue[];
  facts?: BusinessRefusalFact[];
  correctionOf?: string;
  changes?: BusinessInputChange[];
};

export type BusinessRefusalFact = {
  level: string;
  code: string;
  message: string;
  translations?: Record<string, string>;
  step?: string;
  subject: string;
  action: string;
  member: string;
  at: string;
  original: unknown;
  inputs?: Record<string, unknown>;
  record?: string;
  route?: TxnRef;
  escalation?: RefusalEscalation;
};

export type BusinessRefusalPage = {
  records: BusinessRefusalEntry[];
  total: number;
};

export type BusinessTime = {
  at?: string;
  all?: boolean;
};

export type BusinessTxn = {
  subject: string;
  steps: TxnStep[];
  guard?: Expr;
  message?: string;
  reads?: RuleReadSpec;
  confirmation?: Confirmation;
  dispatch?: Dispatch;
  refusals?: Refusal[];
};

export type Calendar = {
  id: string;
  name: string;
  workdays?: number[];
  holidays?: string[];
};

export type CandidateSimulation = {
  actors?: SimulationActor[];
  candidateId: string;
  testId?: string;
  model?: string;
  fixture?: boolean;
  steps: Simulation[];
  recovered: boolean;
  passed?: boolean;
};

export type CandidateSimulationRequest = {
  organization?: OrgSeed;
  roots?: AssetRef[];
  applicationId?: string;
  candidateId?: string;
  actors?: string[];
  sample?: string;
  objectId?: string;
  processId?: string;
  functionId?: string;
  model?: string;
  as?: string;
  at: string;
  steps: SimulationStep[];
  samples?: SimulationSample[];
};

export type Capability = {
  kind: string;
  name: string;
  inputs: ValueSchema;
  outputs: ValueSchema;
  roles: string[];
  side: string;
  parameters?: Field[];
};

export type CapabilityBinding = {
  id: string;
  version: string;
  semantics: string;
  provider: string;
};

export type CapabilityContract = {
  id: string;
  version: string;
  semantics: string;
};

export type CapabilityDescriptor = {
  ref: AssetRef;
  kind: string;
  side: string;
  title: string;
  description: string;
  group: string;
  icon: string;
  tone: string;
  source: string;
  version: string;
  revision?: number;
  target?: string;
  input?: ValueSchema;
  output?: ValueSchema;
  config?: ValueSchema;
  parameters?: Field[];
  effects: string[];
  ports: BlockPort[];
};

export type CapabilityInfo = {
  name: string;
  enabled: boolean;
  actions: string[];
};

export type CapabilityInvocation = {
  ref: AssetRef;
  key: string;
  inputs: unknown;
  target?: string;
  version?: number;
  expectedRevision?: number;
  sources?: string[];
  record?: string;
  bindings?: Record<string, Binding>;
};

export type CapabilityModule = {
  id: string;
  version: string;
  title: string;
  scope: string;
  replacement: string;
  stateFormat?: string;
  provides: CapabilityContract[];
  requires?: CapabilityContract[];
};

export type CapabilityResult = {
  issues?: FieldIssue[];
  ref: AssetRef;
  state: string;
  call?: string;
  result?: unknown;
};

export type CatalogModel = {
  id: string;
  name?: string;
  context?: number;
  free?: boolean;
  priceKnown?: boolean;
  capabilities?: ModelCapabilities;
};

export type Chain = {
  nodes: ChainNode[];
  edges: ChainEdge[];
};

export type ChainEdge = {
  from: string;
  to: string;
};

export type ChainNode = {
  ref: string;
  kind: string;
  title: string;
  state?: string;
  detail?: string;
};

export type ChangeRecord = ChangeRecordJson;

export type ChatAnswer = {
  content: string;
  toolCalls?: ToolCall[];
  usage: Usage;
};

export type ChatRequest = {
  model: string;
  messages: Message[];
  tools?: Tool[];
  maxTokens?: number;
  temperature?: number;
  options?: ModelOptions;
  promptCache?: boolean;
  stream?: boolean;
};

export type Citation = {
  document: string;
  title: string;
  chunk: number;
  step: number;
};

export type Collection = {
  status?: string;
  items: Item[];
  complete: boolean;
  withheld?: boolean;
};

export type Column = {
  name: string;
  title: string;
  kind: string;
  type: string;
  field?: string;
  money?: boolean;
};

export type CompositeAnswer = {
  key: string;
  applied: number;
  digest?: string;
};

export type CompositeEdit = {
  schema: string;
  target: EntityRef;
  payload?: unknown;
};

export type CompositionPlan = {
  modules: CapabilityModule[];
  bindings: CapabilityBinding[];
  order: string[];
};

export type CompositionRequest = {
  selection: Record<string, string>;
  reason: string;
  at: string;
  disabledOwners?: string[];
};

export type ComputeFixture = {
  app?: string;
  name: string;
  output?: string;
  error?: string;
  expectState: "completed" | "failed";
  expectOutput?: string;
};

export type ComputeSDK = {
  source: string;
};

export type ComputeSDKRequest = {
  abi?: string;
  input: ValueSchema;
  output: ValueSchema;
};

export type Confirmation = {
  parties: ConfirmationParty[];
  deadlineSeconds: number;
  onPartial: string;
  onExpire: string;
  onComplete: string;
  onPartialAction?: string;
  reviewerRole: string;
};

export type ConfirmationDifference = {
  at: string;
  matched: boolean;
  observed?: unknown;
  error?: string;
};

export type ConfirmationParty = {
  name: string;
  title: string;
  writeback?: string;
  protocol?: string;
  action?: string;
  target?: string;
  compensateWriteback?: string;
  compensateAction?: string;
  reconcile?: ConfirmationRead;
};

export type ConfirmationProgress = {
  name: string;
  title: string;
  state: string;
  intent: string;
  key: string;
  at?: string;
  answer?: unknown;
  reason?: string;
  request?: unknown;
  requestVersion?: number;
  source?: unknown;
  connection?: unknown;
  sourceVersion?: string;
  protocol?: ConfirmationProtocol;
  compensationRequest?: unknown;
  compensation?: string;
  compensationState?: string;
  compensationAt?: string;
  compensationAnswer?: unknown;
  compensationReason?: string;
  reconcileDue?: string;
  differences?: ConfirmationDifference[];
};

export type ConfirmationProtocol = {
  provider: string;
  version: string;
  action: string;
  compensateAction?: string;
};

export type ConfirmationRead = {
  keyInput?: string;
  source: string;
  keyField: string;
  resultField: string;
  expected: unknown;
  everySeconds: number;
};

export type ConfirmationRun = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  subject: string;
  action: string;
  member: string;
  state: "pending" | "partial" | "confirmed" | "expired" | "rejected" | "compensating" | "compensated";
  deadline: string;
  partial: boolean;
  escalated?: boolean;
  definition: Confirmation;
  release?: string;
  assets: ReleaseAsset[];
  input: unknown;
  parties: ConfirmationProgress[];
  reviewers: string[];
  completion?: string;
  completionError?: string;
  withheld?: boolean;
};

export type Connection = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  kind: "http" | "odata" | "postgres";
  address: string;
  secret?: string;
  allowPrivate?: boolean;
  marking?: "internal" | "confidential" | "restricted";
  state: string;
  requested?: boolean;
  last?: ConnectionCheck;
  definitionVersion?: number;
  published?: string;
  definitionVersions?: string[];
};

export type ConnectionCheck = {
  at: string;
  ok: boolean;
  error?: string;
  detail?: string;
};

export type ConnectionKind = {
  id: string;
  title: string;
  kind: string;
  addressPlaceholder?: string;
};

export type ConnectionKindPage = {
  items: ConnectionKind[];
};

export type ConnectorError = {
  at: string;
  input: string;
  error: string;
};

export type ConnectorView = {
  id: string;
  direction: string;
  dataClasses: string[];
  heartbeat: string;
  health: string;
  lastSeen?: string;
  cursor?: string;
  disabled: boolean;
  lastError?: ConnectorError;
};

export type Constraint = {
  name: string;
  kind: "required" | "unique" | "range" | "pattern" | "expression" | "cardinality" | "immutable" | "temporal";
  fields: string[];
  expr?: Expr;
  message: string;
  translations?: Record<string, string>;
  scope?: string;
  level?: "record" | "txn";
  reads?: RuleReadSpec;
};

export type Context = {
  sources?: string[];
  windows?: Record<string, WindowSummary>;
  refs: Record<string, Value>;
  sets?: Record<string, Collection>;
  me: string;
  now: string;
  today: string;
};

export type ContextView = {
  type: string;
  record: unknown;
  history: RecordChange[];
  references: string[];
  related: Related[];
  links: string[];
  flows: FlowSummary[];
  tasks: TaskSummary[];
};

export type Continuous = {
  Source: string;
  entry?: string;
  Batch: number;
  State: number;
  FrameBytes: number;
  checkpointEvery?: number;
  Window: StreamWindow;
  Intake?: StreamIntake;
  DeadLetter: boolean;
  deadLetterMaxRecords?: number;
  deadLetterTtlMs?: number;
  concurrency?: number;
  aggregate?: StreamAggregate;
  threshold?: StreamThreshold;
  output?: StreamOutput;
  errorPolicy?: string;
  deadLetterRetry?: boolean;
};

export type Contract = {
  stereotype: string;
  client?: string[];
  supplier?: string[];
  basis?: string[];
  extension?: Pair[];
  platform?: boolean;
  note?: string;
};

export type Contribution = {
  key: string;
  view: string;
  surface?: string;
  title?: string;
  kind?: string;
  app?: string;
  source: string;
  state: string;
};

export type CreateTenantRequest = {
  id: string;
  name: string;
  template?: string;
  admin: string;
  settings?: Record<string, string>;
};

export type CredentialBinding = {
  consumer: string;
  origin: string;
};

export type CredentialCatalog = {
  credentials: CredentialInfo[];
  managedAvailable: boolean;
};

export type CredentialInfo = {
  id: string;
  title: string;
  kind: string;
  source: string;
  reference?: string;
  bindings: CredentialBinding[];
  version: number;
  state: string;
  updatedAt: string;
  updatedBy: string;
};

export type CredentialSealRequest = {
  id: string;
  title: string;
  kind: string;
  source: string;
  reference?: string;
  bindings: CredentialBinding[];
  expectedVersion: number;
  sealed?: string;
  value: string;
};

export type CredentialWrite = {
  id: string;
  title: string;
  kind: string;
  source: string;
  reference?: string;
  bindings: CredentialBinding[];
  expectedVersion: number;
  sealed?: string;
};

export type CustomRole = {
  id: string;
  app: string;
  title: string;
  actions: string[];
};

export type DateRange = {
  end: string;
  inclusive?: boolean;
};

export type DecisionDefinition = {
  observation?: ObservationDecisionEvidence;
  definition: AssetBinding;
  contract: SchemaVersion;
  closure?: string;
  dependenciesState: string;
  release?: string;
  dependencies: AssetBinding[];
  applications: AssetBinding[];
};

export type DecisionTrace = {
  kind: string;
  name?: string;
  type?: string;
  id?: string;
  operation?: string;
  expression?: Expr;
  reads?: Context;
  result?: Result;
  request?: AuthzRequest;
  authorization?: Verdict;
  before?: unknown;
  after?: unknown;
  refusal?: string;
};

export type Definition = {
  alert?: AlertRule;
  observation?: ObservationDefinition;
  sampleSet?: SampleSet;
  interface?: Interface;
  protocol?: ProtocolReleaseDescriptor;
  contentVersion?: string;
  propertyType?: PropertyType;
  propertyVersions?: Record<string, PropertyType>;
  valueType?: ValueType;
  valueVersions?: Record<string, ValueType>;
  linkType?: LinkType;
  linkVersions?: Record<string, LinkType>;
  ref: AssetRef;
  rid?: string;
  source: string;
  version: string;
  contractVersion: number;
  requires: AssetRef[];
  provides: Capability[];
  entity?: EntityInfo;
  action?: Action;
  page?: Page;
  application?: Application;
  query?: NamedQuery;
  queryVersions?: Record<string, NamedQuery>;
  flow?: FlowDefinition;
  function?: AIFunction;
  evaluation?: EvaluationSetDescriptor;
  testPlan?: TestPlanDescriptor;
  agent?: AgentReleaseDescriptor;
  operation?: Operation;
};

export type Delivery = {
  at: string;
  app: string;
  action: string;
  target: string;
  subscriber: string;
  outcome: string;
  attempt?: number;
};

export type Dependencies = {
  ref: AssetRef;
  requires: AssetRef[];
  provides: Capability[];
  version: string;
};

export type DependencyView = {
  descriptors: Dependencies[];
  requires: AssetRef[];
  usedBy: AssetRef[];
};

export type DirectoryEnumeration = {
  name: string;
  literals: string[];
};

export type DirectoryProperty = {
  name: string;
  type: string;
  many?: boolean;
};

export type DirectoryType = {
  name: string;
  id: string;
  domain: string;
  abstract?: boolean;
  generals?: string[];
  bases?: string[];
  properties?: DirectoryProperty[];
  client?: string[];
  supplier?: string[];
  constraints?: string[];
};

export type Dispatch = {
  command: string;
  target: string;
  payload?: TxnSet[];
  key: string;
  deadlineSeconds: number;
  readback: DispatchRead;
  onStuck: string;
  onFail: string;
  reviewerRole: string;
};

export type DispatchAttempt = {
  number: number;
  intent: string;
  target: string;
  state: string;
  at: string;
  reason?: string;
  answer?: unknown;
  compensation?: boolean;
};

export type DispatchCommand = {
  name: string;
  title: string;
  endpointKind: string;
  writeback: string;
  idempotent: boolean;
  receiverDeduplicates?: boolean;
  compensate?: string;
};

export type DispatchRead = {
  name: string;
  source?: string;
  observation?: string;
  record?: string;
  keyField?: string;
  resultField?: string;
  reasonField?: string;
  states: Record<string, string>;
  everySeconds: number;
};

export type DispatchReading = {
  at: string;
  state: string;
  value?: string;
  reason?: string;
  observed?: unknown;
};

export type DispatchRun = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  subject: string;
  action: string;
  member: string;
  command: DispatchCommand;
  definition: Dispatch;
  target: string;
  key: string;
  state: "pending" | "accepted" | "running" | "completed" | "stuck" | "failed" | "unknown" | "cancelled" | "compensating" | "compensated";
  reason?: string;
  startedAt: string;
  stateAt: string;
  deadline: string;
  readDue: string;
  readback: DispatchReading[];
  attempts: DispatchAttempt[];
  attempt: number;
  intent: string;
  request: unknown;
  source?: unknown;
  connection?: unknown;
  observation?: unknown;
  device?: string;
  compensationCommand?: DispatchCommand;
  compensationRequest?: unknown;
  compensating?: boolean;
  release?: string;
  assets: ReleaseAsset[];
  input: unknown;
  reviewers: string[];
  escalated?: boolean;
  policyApplied?: boolean;
  withheld?: boolean;
};

export type DispatchTarget = {
  id: string;
  title: string;
};

export type Draft = {
  authorization?: string;
  expectedSources?: Record<string, number>;
  evidence?: string;
  accepted?: string;
  rejected?: string;
  result?: string;
  outcome?: string;
  owner?: string;
  definition?: string;
  sources?: Record<string, number>;
  simulation?: string;
  kind: string;
  action: string;
  target: string;
  type?: string;
  payload: string;
  rationale?: string;
  step: number;
};

export type DraftChange = {
  ref: AssetRef;
  type: string;
  id: string;
  title: string;
  variant?: string;
  published: boolean;
  changed: Stamp;
  fields: DraftChangeField[];
  diagnostic?: string;
};

export type DraftChangeField = {
  name: string;
  before?: unknown;
  after?: unknown;
};

export type Edge = {
  periods?: Period[];
  structure: string;
  unit: string;
  parent: string;
  relation?: string;
  share?: number;
  from?: string;
  until?: string;
};

export type Effect = {
  compensates?: string;
  applications?: AssetBinding[];
  id: string;
  endpoint: string;
  event: string;
  app?: string;
  key?: string;
  target: string;
  at: string;
  state: string;
  agent?: string;
  run?: string;
  attempts: number;
  last?: string;
  due?: string;
  error?: string;
  publicMessage?: string;
  callbackError?: string;
  digest?: string;
  body?: string;
  generation?: number;
  output?: unknown;
  millis?: number;
  withheld?: boolean;
};

export type EffectKind = {
  name: string;
  title: string;
  description: string;
  irreversible?: boolean;
};

export type Element = {
  id: string;
  stereotype: string;
  name: string;
  shortName?: string;
  kind?: string;
  legal?: boolean;
  external?: boolean;
  from?: string;
  until?: string;
  closed?: string;
  calendar?: string;
  owner?: string;
  published?: boolean;
  properties?: Record<string, unknown>;
  periods?: Period[];
  deleted?: boolean;
};

export type EndpointView = {
  id: string;
  kind: string;
  url: string;
  secret?: string;
  events?: string[];
  effects?: string[];
  from?: string;
  notifications?: string[];
  allowPrivate?: boolean;
  irreversible?: boolean;
  pending: number;
  failing: number;
  health: string;
  delivered: number;
};

export type EnterpriseModel = {
  uaf: string;
  scale?: string;
  kinds: Kind[];
  elements: Element[];
  relationships: Relationship[];
  views: View[];
  calendars?: Calendar[];
  tenantLinks?: TenantLink[];
};

export type EnterpriseQueryElement = {
  id: string;
  stereotype: string;
  name: string;
  kind?: string;
  shortName?: string;
  legal?: boolean;
  from?: string;
  until?: string;
  periods?: Period[];
  closed?: boolean;
};

export type EnterpriseQueryResult = {
  elements: EnterpriseQueryElement[];
};

export type EnterpriseReference = {
  type: string;
  field: string;
  fieldTitle: string;
  stereotype?: string;
  id: string;
  name: string;
};

export type EnterpriseReferenceGroup = {
  type: string;
  title: string;
  field: string;
  fieldTitle: string;
  stereotype?: string;
  total: number;
  records: EnterpriseReference[];
};

export type EnterpriseTarget = {
  source: string;
  stereotype: string;
  id: string;
  name: string;
  shortName?: string;
  kind?: string;
  parent?: string;
  root?: string;
  in?: string;
  relation?: string;
  at?: string;
  atKind?: string;
  from?: string;
  until?: string;
  properties?: string[];
};

export type EnterpriseUsage = {
  canDelete: boolean;
  canClose: boolean;
  hasRestrictedUsage: boolean;
  items: EnterpriseUsageItem[];
};

export type EnterpriseUsageItem = {
  kind: string;
  title: string;
  ref?: string;
  field?: string;
  current: boolean;
};

export type EntityInfo = {
  localRules?: Record<string, Expr>;
  enterpriseUse?: Expr;
  versioning?: Versioning;
  versionPins?: VersionPinBinding[];
  sets?: RecordSet[];
  constraints?: Constraint[];
  type: string;
  title: string;
  plural: string;
  description?: string;
  synonyms?: string;
  knowledgeFiles?: boolean;
  app: string;
  display: string;
  icon?: string;
  color?: string;
  fields: FieldInfo[];
  standard: string[];
  implements?: string[];
  extends?: string;
  lifecycle?: LifecycleInfo;
};

export type EntityRef = EntityRefJson;

export type ErrorBody = {
  message?: string;
  issues?: FieldIssue[];
  code: string;
  status?: number;
  detail?: string;
};

export type EvaluationCase = {
  name: string;
  input: unknown;
  expected: unknown;
};

export type EvaluationPolicy = {
  compareFields?: string[];
  minQuality: number;
  maxCostUsd: number;
  maxLatencyMillis: number;
  cases: EvaluationCase[];
};

export type EvaluationSetDescriptor = {
  name: string;
  title: string;
  function: AssetBinding;
  model?: string;
  policy: EvaluationPolicy;
};

export type Explanation = {
  member: string;
  permission: string;
  app: string;
  roles: string[];
  allowed: string[];
  verdict: Verdict;
};

export type Expr = {
  op: string;
  kind: string;
  args?: Expr[];
  lit?: Literal;
  ref?: string;
  path?: string[];
  agg?: Aggregate;
  in?: Expr;
  window?: WindowReference;
};

export type Field = {
  name: string;
  type: string;
  required?: boolean;
  description: string;
  choices?: string[];
  ref?: string;
  fields?: Field[];
  stereotype?: string;
  stereotypes?: string[];
  default?: unknown;
  from?: string;
  key?: string;
  label?: string;
  constraints?: InputConstraints;
  valueType?: AssetBinding;
  range?: DateRange;
  group?: string;
  manual?: boolean;
};

export type FieldChange = {
  field: string;
  before?: unknown;
  after?: unknown;
};

export type FieldCondition = {
  field: string;
  in: string[];
};

export type FieldFormat = {
  tone: "neutral" | "info" | "success" | "warning" | "danger" | "purple" | "pink" | "teal" | "orange";
  icon?: string;
  label?: string;
};

export type FieldInfo = {
  aiInput?: boolean;
  sourceType?: "record" | "user-input" | "external-input" | "generated";
  constraints?: InputConstraints;
  range?: DateRange;
  group?: string;
  property?: AssetBinding;
  valueType?: AssetBinding;
  format?: FormatRule[];
  name: string;
  title: string;
  type: "text" | "longtext" | "integer" | "decimal" | "money" | "date" | "datetime" | "boolean" | "choice" | "reference" | "references" | "tags" | "lines" | "json";
  required?: boolean;
  search?: boolean;
  readOnly?: boolean;
  aliasOf?: string;
  aside?: boolean;
  choices?: string[];
  choiceTitles?: string[];
  ref?: string;
  stereotype?: string;
  stereotypes?: string[];
  inverse?: string;
  knowledge?: boolean;
  read?: string[];
  write?: string[];
  personal?: string;
  help?: string;
  synonyms?: string;
  example?: string;
  when?: FieldCondition;
  fields?: FieldInfo[];
};

export type FieldIssue = {
  businessCode?: string;
  record?: string;
  constraint?: string;
  translations?: Record<string, string>;
  values?: Record<string, unknown>;
  reason?: string;
  code: string;
  message: string;
  path: string[];
  relatedPaths?: string[][];
};

export type FieldRef = {
  name?: string;
  path?: string[];
  slot?: string;
  transform?: string;
};

export type FlowDefinition = {
  id: string;
  app: string;
  title: string;
  version: number;
  start: string[];
  steps: FlowStep[];
};

export type FlowStep = {
  name: string;
  title: string;
  kind: "action" | "wait" | "ask" | "subflow" | "fork" | "agent" | "transform" | "branch" | "foreach" | "compute" | "ai" | "end" | "break" | "continue";
  next: string[];
  chooses?: boolean;
};

export type FlowSummary = {
  id: string;
  flow: string;
  state: string;
  trace: TraceLine[];
};

export type FormatRule = {
  when: Expr;
  tone: "neutral" | "info" | "success" | "warning" | "danger" | "purple" | "pink" | "teal" | "orange";
  icon?: string;
  label?: string;
};

export type Frame = {
  node: string;
  index: number;
  item?: unknown;
};

export type FunctionFixture = {
  output: string;
  inputTokens: number;
  outputTokens: number;
  expectState: "ready" | "rejected";
  expectOutput?: string;
};

export type FunctionRef = {
  app?: string;
  name: string;
  version: number;
};

export type FunctionRun = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  trainingAuthorization?: string;
  trainingSources?: Record<string, number>;
  sourceValues?: Record<string, unknown>;
  sourceRevision?: number;
  decision?: "pending" | "adopted" | "rejected" | "expired";
  expiresAt?: string;
  reviewers?: string[];
  decidedBy?: string;
  decidedAt?: string;
  adoptedAction?: string;
  adoptedChange?: string;
  amendments?: unknown;
  applications?: AssetBinding[];
  question?: string;
  history?: string[];
  function: string;
  app?: string;
  contract: AIFunction;
  version: number;
  member: string;
  source: string;
  state: "pending" | "ready" | "rejected";
  output?: string;
  code?: string;
  reason?: string;
  definition: string;
  dependencies: string;
  model: string;
  inputHash: string;
  release?: string;
  metered?: boolean;
  tokensReported?: boolean;
  inputTokens?: number;
  outputTokens?: number;
  costReported?: boolean;
  costUsd?: number;
  latencyMillis?: number;
  servedModel?: string;
  sources: string[];
  withheld?: boolean;
};

export type Grant = {
  app: string;
  role: string;
  unit?: string;
  structure?: string;
  from?: string;
  until?: string;
  by?: string;
  reason?: string;
  at?: string;
};

export type HistogramBucket = {
  lower: string;
  upper: string;
  upperInclusive: boolean;
  count: number;
};

export type HistogramQuery = {
  field: string;
  bins: number;
};

export type HistogramResult = {
  field: string;
  requestedBins: number;
  valid: number;
  missing: number;
  minimum: string;
  maximum: string;
  buckets: HistogramBucket[];
};

export type Hit = {
  type: string;
  id: string;
  title: string;
};

export type HostArtifactView = {
  candidate: string;
  active?: boolean;
  digest?: string;
  size: number;
  assets: number;
  verified: boolean;
  note?: string;
};

export type HostComposition = {
  current: Record<string, string>;
  desired: Record<string, string>;
  available: CapabilityModule[];
  state: string;
  reason?: string;
  owners: CapabilityModule[];
  disabledOwners: string[];
  desiredDisabledOwners: string[];
  storage: CapabilityModule;
};

export type HostOverview = {
  now: string;
  tenants: HostTenantView[];
  admins: number;
};

export type HostTenantView = {
  id: string;
  quarantined?: boolean;
  fault?: string;
  lifecycle?: string;
  apps: string[];
  members: number;
  audit: number;
  activeRelease?: string;
  candidates: number;
  support: number;
  failedWork: number;
  connectors: number;
  started?: string;
};

export type Identity = {
  token: string;
  tenant: string;
  member: string;
  roles: Record<string, string>;
};

export type ImageRegion = {
  id: string;
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
};

export type ImageRegionsRequest = {
  hash: string;
  regions: ImageRegion[];
};

export type ImportRow = {
  row: number;
  id: string;
  action: string;
  outcome: string;
  error?: ErrorBody;
};

export type InboxTask = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  title: string;
  body?: string;
  withheld?: boolean;
  ref?: string;
  app: string;
  key?: string;
  candidates: string[];
  assignee?: string;
  due?: string;
  state: "open" | "done" | "canceled";
  answers?: string[];
  answer?: string;
  answerTitles?: string[];
};

export type InputConstraints = {
  min?: number;
  max?: number;
  exclusiveMin?: boolean;
  exclusiveMax?: boolean;
  minLength?: number;
  maxLength?: number;
  before?: string;
  after?: string;
  earliest?: string;
  latest?: string;
  inclusive?: boolean;
  dateTime?: boolean;
  pattern?: string;
};

export type InputOption = {
  id: string;
  name: string;
  unit?: string;
};

export type InputOptions = {
  manual?: boolean;
  items: InputOption[];
};

export type InstalledPackage = {
  id: string;
  version: string;
  namespace: string;
  title?: string;
  state: string;
  contributions?: PackageContribution[];
  digest?: string;
  artifact?: string;
  installedAt?: string;
  changedAt?: string;
  retained?: RetainedArtifact[];
};

export type IntegrationEffect = {
  id: string;
  endpoint: string;
  event: string;
  state: string;
  due: string;
  last?: string;
  error?: string;
};

export type IntegrationExecution = {
  id: string;
  member: string;
  scopes?: string[];
  pending?: boolean;
  snapshot: string;
  inputs?: Record<string, number>;
};

export type Interface = {
  name: string;
  title: string;
  description?: string;
  fields: InterfaceField[];
};

export type InterfaceField = {
  name: string;
  type: string;
  title: string;
};

export type InterfaceRecord = {
  type: string;
  id: string;
  record: Record<string, unknown>;
};

export type InterfaceRecordPage = {
  records: InterfaceRecord[];
  total: number;
};

export type Issue = {
  code: string;
  at: string;
  message: string;
};

export type Item = {
  id: string;
  fields: Record<string, Value>;
};

export type JointDraftRef = {
  kind: string;
  id: string;
};

export type Kind = {
  id: string;
  name: string;
  kind: string;
  matrix?: boolean;
  owner?: string;
};

export type KnowledgeIndexStatus = {
  needsRebuild?: boolean;
  active: string;
  preparing: string;
  total: number;
  completed: number;
  dimensions?: number;
  ready: boolean;
  problem?: string;
};

export type Layer = {
  name: string;
  allowed: boolean;
  reason: string;
  role?: string;
  policy?: string;
};

export type LifecycleInfo = {
  field: string;
  initial: string;
  states: State[];
  transitions: TransitionInfo[];
};

export type LifecycleRequest = {
  action: string;
  reason?: string;
};

export type Limit = {
  member: string;
  dailyTokens?: number;
  perMinute?: number;
};

export type Link = {
  from: string;
  to: string;
  by: string;
  at: string;
};

export type LinkTraversal = {
  binding: AssetBinding;
  direction: string;
  id: string;
};

export type LinkType = {
  roles?: string[];
  name: string;
  title: string;
  description: string;
  parent: AssetRef;
  child: AssetRef;
  via: string;
  forward: string;
  reverse: string;
  storage: string;
  cardinality: string;
  required: boolean;
  deletePolicy: string;
};

export type Literal = {
  value: unknown;
  element?: string;
};

export type LiveQueryFrame = {
  sequence: number;
  results: LiveQueryResult[];
};

export type LiveQueryResult = {
  path: string;
  status: number;
  body: unknown;
};

export type LoopFrame = {
  items?: unknown[];
  outer?: string[];
  next: number;
  results?: Record<string, unknown>;
  state?: unknown;
  while?: boolean;
};

export type MeView = {
  tenantId: string;
  principalId: string;
  profile: Member;
  apps: AppEntry[];
  tenants: string[];
  language: string;
  languages: string[];
  preferred?: string;
  currency: string;
  account: Account;
  tenant: TenantRecord;
};

export type Member = {
  id: string;
  tenant: string;
  roles: Record<string, string>;
  grants?: Grant[];
  status?: string;
  scopes?: string[];
  agent?: boolean;
  language?: string;
  timezone?: string;
};

export type MemberView = {
  id: string;
  tenant: string;
  roles: Record<string, string>;
  grants?: Grant[];
  status?: string;
  scopes?: string[];
  agent?: boolean;
  language?: string;
  timezone?: string;
  subjects: string[];
  profile: Profile;
};

export type Membership = {
  periods?: Period[];
  party: string;
  unit: string;
  role: string;
  primary?: boolean;
  from?: string;
  until?: string;
};

export type Memory = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  agent: string;
  fact: string;
  for?: string;
  run?: string;
  sources?: string[];
  state: "proposed" | "active" | "forgotten";
  expires?: string;
  withheld?: boolean;
};

export type Message = {
  role: string;
  content: string;
  toolCalls?: ToolCall[];
  toolCallId?: string;
};

export type Metamodel = {
  version: string;
  uri: string;
  domains: string[];
  profile: ProfileEntry[];
  stereotypes: Record<string, DirectoryType>;
  enumerations: Record<string, DirectoryEnumeration>;
  views: Viewpoint[];
  contracts: Contract[];
};

export type MigrationManifest = {
  at: string;
  from: string;
  to: string;
  key: string;
  member: string;
  types: MigrationType[];
};

export type MigrationResult = {
  from: string;
  to: string;
  key: string;
  types: MigrationType[];
};

export type MigrationType = {
  type: string;
  rows: number;
  written: number;
  refused?: ImportRow[];
};

export type Model = {
  provider: string;
  model: string;
  access: string;
  dailyTokens?: number;
  capabilities?: ModelCapabilities;
  defaults?: ModelOptions;
  revision?: number;
};

export type ModelBinding = {
  type: string;
  fields?: FieldRef[];
  relation?: ModelRelation;
  set?: ModelRecordSet;
  where?: Expr[];
  label?: Expr;
  options?: OptionsRef;
  visibility?: "hide" | "mark";
};

export type ModelCapabilities = {
  source: string;
  observedAt: string;
  purposes?: string[];
  inputTokens?: number;
  outputTokens?: number;
  contextTokens?: number;
  tools?: boolean;
  structured?: boolean;
  thinkingModes?: string[];
  effortLevels?: string[];
  declaredThinkingModes?: string[];
  parameters?: string[];
  inputModalities?: string[];
  outputModalities?: string[];
  adapter?: string;
};

export type ModelChoice = {
  model: Model;
  effective?: AIModelBinding;
  problem?: string;
  purposeUnknown?: boolean;
  references?: string[];
};

export type ModelOptions = {
  inputTokens?: number;
  maxTokens?: number;
  thinking?: string;
  effort?: string;
};

export type ModelRecordSet = {
  recordVariable?: string;
  name?: string;
  query?: AssetBinding;
};

export type ModelRelation = {
  parentType: string;
  parentSelection?: string;
  field: string;
  name?: string;
};

export type ModelRule = {
  action: AssetRef;
  inputs?: Record<string, string>;
};

export type ModelSelection = {
  purpose: string;
  default: string;
  defaultProblem?: string;
  resolvedDefault?: string;
  options: ModelOptions;
  models: ModelChoice[];
};

export type ModelViewBinding = {
  model: ModelBinding;
  roles?: Record<string, FieldRef>;
  expressions?: Record<string, Expr>;
};

export type NamedQuery = {
  name: string;
  title: string;
  description: string;
  object: string;
  interface?: string;
  interfaceShape?: Interface;
  implementations?: string[];
  by?: string;
  domain?: unknown;
  sort?: string[];
  limit?: number;
};

export type NodePosition = {
  x: number;
  y: number;
};

export type Note = {
  entity: string;
  by: string;
  at: string;
  title?: string;
  text: string;
};

export type Notification = {
  id: string;
  member: string;
  app: string;
  title: string;
  body?: string;
  ref?: string;
  key?: string;
  at: string;
  read: boolean;
  withheld?: boolean;
};

export type ObjectSet = {
  object?: string;
  interface?: string;
  ids?: string[];
  filter?: Expr;
  domain?: unknown;
  search?: string;
  around?: SearchAround;
  union?: ObjectSet[];
  intersect?: ObjectSet[];
  subtract?: ObjectSet[];
  archived?: boolean;
  asOf?: BusinessTime;
};

export type ObjectSetCount = {
  count: number;
  partial?: boolean;
};

export type ObjectSetRequest = {
  set: ObjectSet;
  sort?: string[];
  offset?: number;
  limit?: number;
  groups?: string[];
  measures?: string[];
  window?: AggregateWindowQuery;
  histogram?: HistogramQuery;
  maxRows?: number;
  collection?: RecordSetRef;
  format?: boolean;
};

export type ObservationCoverage = {
  availableSince: string;
  completeUntil: string;
  truncated: boolean;
  good: number;
  bad: number;
  uncertain: number;
  stale: number;
};

export type ObservationDecision = {
  mode?: "summary" | "intervals";
  when: Expr;
  action: AssetRef;
  inputs: Record<string, Expr>;
};

export type ObservationDecisionEvidence = {
  key: string;
  definition: AssetBinding;
  record: string;
  device: string;
  signal: string;
  window: WindowSummary;
  intervals?: ObservationIntervalResult;
};

export type ObservationDefinition = {
  name: string;
  title: string;
  description?: string;
  connection: AssetRef;
  object: AssetRef;
  deviceField: string;
  signal: string;
  unit: string;
  path: string;
  windowSeconds: number;
  expectedRetentionSeconds: number;
  expectedGranularitySeconds: number;
  roles: string[];
  scheduled?: boolean;
  requireComplete?: boolean;
  decision?: ObservationDecision;
  intervals?: ObservationIntervals;
};

export type ObservationEvaluate = {
  record: string;
  until: string;
  key: string;
};

export type ObservationEvaluation = {
  observation?: ObservationResult;
  matched: boolean;
  receipt?: SubmissionAnswer;
  reused?: boolean;
};

export type ObservationInfo = {
  ref: AssetRef;
  version: string;
  definition: ObservationDefinition;
};

export type ObservationInterval = {
  since: string;
  until: string;
  openStart?: boolean;
  openEnd?: boolean;
};

export type ObservationIntervalResult = {
  version: string;
  revision: string;
  since: string;
  until: string;
  ready: boolean;
  count: number;
  intervals: ObservationInterval[];
  unknown: ObservationUnknown[];
};

export type ObservationIntervals = {
  valueKind: "decimal" | "choice" | "boolean";
  when: Expr;
  maxGapSeconds: number;
  latePolicy: "reject" | "correct";
  latenessSeconds: number;
  maxIntervals: number;
};

export type ObservationPoint = {
  at: string;
  value: string;
};

export type ObservationRead = {
  record: string;
  since: string;
  until: string;
  mode: string;
  limit?: number;
};

export type ObservationResult = {
  ref: AssetRef;
  version: string;
  object: AssetRef;
  record: string;
  device: string;
  signal: string;
  window: WindowSummary;
  points?: ObservationPoint[];
  sources: string[];
  matched?: boolean;
  coverage?: ObservationCoverage;
  latest?: ObservationSample;
  intervals?: ObservationIntervalResult;
};

export type ObservationSample = {
  device: string;
  signal: string;
  type: string;
  value?: unknown;
  unit?: string;
  quality: string;
  at: string;
  receivedAt: string;
  reason?: string;
  source: ObservationSampleSource;
};

export type ObservationSampleSource = {
  connection: string;
  revision: string;
  generation: string;
  sequence?: string;
  clock?: string;
};

export type ObservationUnknown = {
  since: string;
  until: string;
  reason: "coverage" | "gap" | "bad" | "uncertain" | "stale" | "late" | "rule";
};

export type Operation = {
  rules?: RuleTablePlan;
  name: string;
  title: string;
  description: string;
  input: ValueSchema;
  output: ValueSchema;
  roles: string[];
  binding: OperationBinding;
  limits: OperationLimits;
};

export type OperationBinding = {
  kind: string;
  module?: string;
  abi?: string;
};

export type OperationLimits = {
  timeoutMillis: number;
  memoryPages: number;
  maxInputBytes: number;
  maxOutputBytes: number;
  stagedOutputBytes?: number;
  dataInputBytes?: number;
};

export type OperationRef = {
  app?: string;
  name: string;
  version: number;
};

export type OperationResult = {
  ownerVersion?: string;
  ref?: AssetRef;
  definition?: string;
  version?: number;
  module?: string;
  dependencies?: string;
  release?: string;
  id: string;
  state: string;
  output?: unknown;
  error?: string;
  generation: number;
  millis?: number;
};

export type OptionsRef = {
  set?: ModelRecordSet;
  rule?: ModelRule;
};

export type OrgSeed = {
  calendars?: Calendar[];
  structures: Structure[];
  units: Unit[];
  edges: Edge[];
  memberships: Membership[];
};

export type PackageAIRequirement = {
  problem?: string;
  ref: AssetRef;
  draftId?: string;
  model?: string;
  ready: boolean;
};

export type PackageApplication = {
  ref: AssetRef;
  title: string;
  draftId?: string;
  state?: string;
  published: boolean;
  pages: string[];
};

export type PackageBlueprint = {
  roots: AssetRef[];
  assets: ReleaseAsset[];
  guide?: string;
  setup?: PackageSetup;
};

export type PackageConnection = {
  ref: AssetRef;
  title: string;
  draftId?: string;
  kind: string;
  credential: boolean;
  configured: boolean;
  ready: boolean;
};

export type PackageContribution = {
  view: string;
  surface?: string;
  title?: string;
  kind?: string;
  app?: string;
};

export type PackageDescriptor = {
  blueprint?: PackageBlueprint;
  id: string;
  version: string;
  title?: string;
  namespace: string;
  requires?: string[];
  capabilities?: CapabilityContract[];
  compatibility?: string;
  contributions?: PackageContribution[];
  artifactDigest?: string;
  description?: string;
};

export type PackageDraft = {
  ref: AssetRef;
  type: string;
  id: string;
};

export type PackageDraftRequest = {
  version: string;
  key: string;
};

export type PackageDraftResult = {
  package: string;
  version: string;
  key: string;
  application: string;
  applicationID: string;
  drafts: PackageDraft[];
};

export type PackageInspectRequest = {
  url?: string;
  content?: unknown;
};

export type PackagePreparation = {
  package: string;
  version: string;
  applications: PackageApplication[];
  connections: PackageConnection[];
  ai: PackageAIRequirement[];
  roles: PackageRoleRequirement[];
  setup?: PackageSetup;
  installed: boolean;
  installedVersion?: string;
  installationState?: string;
};

export type PackagePreview = {
  descriptor: PackageDescriptor;
  digest: string;
  origin: string;
  precheck: Precheck;
};

export type PackageRemoval = {
  id: string;
  ok: boolean;
  blockers: string[];
};

export type PackageRoleRequirement = {
  app: string;
  role: string;
  held: boolean;
  available: boolean;
};

export type PackageSetup = {
  roles: PackageSetupRole[];
  steps: PackageSetupStep[];
};

export type PackageSetupRole = {
  app: string;
  role: string;
};

export type PackageSetupStep = {
  name: string;
  app: string;
  schema: string;
  target: EntityRef;
  payload: string;
};

export type PackageView = {
  origin?: string;
  sourceDigest?: string;
  removal?: PackageRemoval;
  descriptor: PackageDescriptor;
  installed?: InstalledPackage;
  precheck: Precheck;
  source: string;
};

export type Page = {
  format?: number;
  dataBindings?: PageDataBinding[];
  actionTriggers?: PageActionTrigger[];
  inputSchema?: PageInputSchema;
  scopeVisibility?: PageScopeVisibility;
  widgetSpec?: PageWidgetSpec;
  presentationWords?: Record<string, string>;
  name: string;
  title: string;
  description?: string;
  object: AssetRef;
};

export type PageActionEffect = {
  ref: AssetRef;
  recordVariable?: string;
  inputs?: Record<string, PageValue>;
};

export type PageActionParameter = {
  parameter: string;
  field: string;
};

export type PageActionTrigger = {
  id: string;
  owner?: string;
  binding: string;
  actions?: AssetRef[];
  function?: AssetBinding;
  operation?: AssetBinding;
  inputs?: Record<string, Binding>;
  defaults?: PageActionParameter[];
  inlineEdit?: PageInlineEdit;
  events?: PageEventBinding[];
};

export type PageCollectionInput = {
  kind: string;
  object: AssetRef;
  predicate: RecordSetPredicate;
  sort: string[];
  sortLocked?: boolean;
  traversal?: LinkTraversal;
  bindings?: AssetBinding[];
};

export type PageCompositeResult = {
  sources: Record<string, PageCompositeSource>;
  cells?: PageMatrixCell[];
};

export type PageCompositeSource = {
  object: string;
  records: Record<string, unknown>[];
};

export type PageComputeResource = {
  operation: AssetBinding;
  recordVariable: string;
  inputs: Record<string, Binding>;
  outputField?: string;
};

export type PageDataBinding = {
  value?: Expr;
  acyclic?: boolean;
  id: string;
  owner?: string;
  kind: string;
  object: AssetRef;
  model?: ModelBinding;
  models?: Record<string, ModelViewBinding>;
  fields?: FieldRef[];
  fieldSlots?: Record<string, FieldRef[]>;
  ports?: Record<string, string>;
  query?: AssetBinding;
  resources?: PageResourceBinding[];
  limit?: number;
};

export type PageEffect = {
  kind: string;
  target?: string;
  value?: unknown;
  from?: string;
  action?: PageActionEffect;
  navigate?: PageNavigation;
};

export type PageEventBinding = {
  control?: string;
  source: string;
  event: string;
  effects: PageEffect[];
};

export type PageExpression = {
  op: string;
  args: PageValue[];
};

export type PageInlineEdit = {
  action: AssetRef;
  fields: string[];
};

export type PageInputSchema = {
  variables?: Record<string, PageVariable>;
  queries?: Record<string, PageQuery>;
  interface?: PageInterface;
  selections?: SelectionVariable[];
};

export type PageInterface = {
  version: number;
  inputs?: Record<string, PagePort>;
  outputs?: Record<string, PagePort>;
};

export type PageMatrixCell = {
  row: string;
  column: string;
  value: unknown;
  kind: string;
};

export type PageModelRead = {
  page: AssetRef;
  contentVersion?: string;
  query?: Query;
  draft?: Page;
  section: string;
  records?: Record<string, PageModelRecord>;
  values?: Record<string, unknown>;
  offset?: number;
  limit?: number;
  search?: string;
  sort?: string[];
};

export type PageModelRecord = {
  object: string;
  id: string;
};

export type PageModelResult = {
  composite?: PageCompositeResult;
  object: string;
  records: Record<string, unknown>[];
  total: number;
  partial: boolean;
  hidden: boolean;
  fields: FieldInfo[];
  access: Record<string, Record<string, Verdict>>;
  authorization?: AuthzCollection;
  model: ModelBinding;
  labelField?: string;
  sort?: string[];
  sortLocked?: boolean;
};

export type PageModelRule = {
  action: AssetRef;
  title: string;
  criteria: RuleCriteria;
};

export type PageNavigation = {
  page: AssetRef;
  interfaceVersion: number;
  inputs?: Record<string, PageValue>;
  results?: Record<string, string>;
};

export type PagePort = {
  variable: string;
  type: string;
  object?: AssetRef;
  required?: boolean;
};

export type PageQuery = {
  input?: string;
  direction?: string;
  itemOwner?: string;
  set?: PageQuerySet;
  owner?: string;
  title?: string;
  object: AssetRef;
  interface?: string;
  query?: AssetBinding;
  conditions?: PageQueryCondition[];
  search?: PageValue;
  for?: PageValue;
  sort?: string[];
  limit: number;
  offset?: number;
};

export type PageQueryCondition = {
  optional?: boolean;
  asDateTime?: boolean;
  asDate?: boolean;
  asDecimal?: boolean;
  field: string;
  op: string;
  value: PageValue;
};

export type PageQuerySet = {
  op: string;
  inputs: string[];
};

export type PageRequest = {
  app: string;
  owner?: string;
  page: string;
};

export type PageResourceBinding = {
  readOnly?: boolean;
  id: string;
  kind: string;
  asset?: AssetBinding;
  object?: AssetRef;
  url?: string;
  origin?: string;
  contentVersion?: string;
  interfaceVersion?: number;
  inputs?: Record<string, PageValue>;
  outputs?: Record<string, string>;
  fields?: FieldRef[];
  source?: string;
  limit?: number;
};

export type PageResourceSource = {
  compute?: PageComputeResource;
  port?: string;
  measure?: string;
  field?: string;
  fields?: string[];
  object?: AssetRef;
  query?: string;
  variable?: string;
  kind: string;
  section?: string;
  node?: string;
};

export type PageScope = {
  id: string;
  parent?: string;
  kind: string;
  binding?: string;
  bindings?: Record<string, string>;
  maxIterations?: number;
  inactive?: boolean;
};

export type PageScopeVisibility = {
  scopes: PageScope[];
  owners: Record<string, string>;
  root: string;
};

export type PageValue = {
  variable?: string;
  literal?: unknown;
};

export type PageVariable = {
  ruleType?: Type;
  rule?: Expr;
  writable?: boolean;
  title?: string;
  scope: string;
  owner?: string;
  type: string;
  mode: string;
  initial?: unknown;
  expression?: PageExpression;
  source?: PageResourceSource;
};

export type PageView = {
  application: ApplicationView;
  page: Page;
  entity?: EntityInfo;
  actions: Action[];
};

export type PageWidgetSpec = {
  format: string;
  body: unknown;
};

export type Pair = {
  source: string;
  target: string;
};

export type Passage = {
  document: string;
  title: string;
  chunk: number;
  text: string;
  score: number;
};

export type Period = {
  from?: string;
  until?: string;
};

export type PermissionAction = {
  id: string;
  title: string;
  scope: string;
};

export type PersonalRead = {
  at: string;
  member: string;
  type: string;
  ids: string[];
  fields: string[];
};

export type Pin = {
  ref: string;
  label?: string;
  anchor: string;
  at: number[];
};

export type Pipeline = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  input: string;
  steps: TransformStep[];
  expectations?: TransformExpectation[];
  outputDataset?: string;
  outputObject?: string;
  key?: string;
  marking?: string;
  outputEnterprise?: EnterpriseTarget;
  every?: string;
  state: string;
  runner?: string;
  runnerScopes?: string;
  requested?: boolean;
  last?: PipelineRun;
  definitionVersion?: number;
  published?: string;
  definitionVersions?: string[];
};

export type PipelinePreview = {
  inputVersion: number;
  inputRows: number;
  sampledRows: number;
  truncated: boolean;
  rows: Record<string, unknown>[];
  quarantine: QuarantinedRow[];
  marking?: string;
};

export type PipelinePreviewRequest = {
  pipeline: Pipeline;
};

export type PipelineRun = {
  execution?: IntegrationExecution;
  definitionVersion?: string;
  at: string;
  inputVersion: number;
  rows: number;
  written: number;
  marking?: string;
  merged?: number;
  quarantined: number;
  failed: number;
  error?: string;
  quarantine?: QuarantinedRow[];
  failures?: SourceFailure[];
  outputVersion?: number;
};

export type PolicyRecord = {
  id: string;
  title: string;
  effect: string;
  permission: string;
  where?: Record<string, string>;
  from?: string;
  until?: string;
};

export type Precheck = {
  id: string;
  version: string;
  ok: boolean;
  problems?: string[];
  missing?: string[];
  bindings?: CapabilityBinding[];
};

export type Predicate = {
  op: "eq" | "neq" | "gt" | "gte" | "lt" | "lte" | "contains" | "exists" | "all" | "any" | "not";
  left?: Binding;
  right?: Binding;
  terms?: Predicate[];
};

export type Process = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  kind?: "flow" | "automation";
  object?: string;
  when?: string;
  manual?: boolean;
  every?: string;
  scheduler?: string;
  input?: unknown;
  inputSchema?: ValueSchema;
  continuous?: Continuous;
  steps: ProcessStep[];
  lanes?: ProcessLane[];
  layout?: Record<string, NodePosition>;
  state: "draft" | "published";
  version?: number;
  published?: string;
  versions?: string[];
};

export type ProcessDiagnostic = {
  node?: string;
  code: string;
  message: string;
};

export type ProcessDiagnostics = {
  valid: boolean;
  issues: ProcessDiagnostic[];
};

export type ProcessLane = {
  name: string;
  title?: string;
};

export type ProcessStep = {
  name: string;
  title?: string;
  kind: "payload" | "query" | "action" | "transform" | "branch" | "switch" | "foreach" | "while" | "fork" | "join" | "ask" | "wait" | "subflow" | "ai" | "compute" | "end" | "fail" | "break" | "continue";
  lane?: string;
  inputs?: Record<string, Binding>;
  value?: Binding;
  target?: Binding;
  condition?: Predicate;
  collection?: Binding;
  waitUntil?: Binding;
  cases?: Record<string, string>;
  branches?: string[];
  next?: string;
  error?: string;
  ask?: string;
  answers?: string[];
  act?: string;
  protocol?: string;
  app?: string;
  query?: string;
  queryVersion?: number;
  function?: FunctionRef;
  operation?: OperationRef;
  body?: string;
  maxIterations?: number;
  concurrency?: number;
  mode?: "all" | "any";
  timeoutSeconds?: number;
  untilSeconds?: number;
  retryAttempts?: number;
  retryBackoffMs?: number;
  flow?: string;
  flowVersion?: number;
};

export type Profile = {
  member: string;
  displayName?: string;
  givenName?: string;
  familyName?: string;
  title?: string;
  pronouns?: string;
  email?: string;
  phone?: string;
  language?: string;
  timezone?: string;
  dateFormat?: string;
  numberFormat?: string;
  weekStart?: string;
  inApp?: boolean;
  mail?: boolean;
  digest?: string;
  quietFrom?: string;
  quietTo?: string;
  homePage?: string;
  theme?: string;
  density?: string;
  lastSeen?: string;
};

export type ProfileEntry = {
  stereotype: string;
  title: string;
  plural: string;
  kinds?: string[];
  icon?: string;
};

export type ProjectAsset = {
  app?: string;
  kind: string;
  name: string;
};

export type ProjectMember = {
  member: string;
  role?: string;
};

export type PromotionResult = {
  from: string;
  to: string;
  candidate: string;
  digest: string;
  active: boolean;
  key: string;
  assets: number;
};

export type PromotionTarget = {
  tenant: string;
  member: string;
  at: string;
  active: boolean;
};

export type PropertyType = {
  name: string;
  title: string;
  description: string;
  type: string;
};

export type ProposalAdoption = {
  function: FunctionRef;
  input: string;
  mapping: ProposalInput[];
};

export type ProposalInput = {
  output: string;
  input: string;
};

export type ProposalPolicy = {
  handoffRole?: string;
  confidenceField?: string;
  minimumConfidence?: number;
  expiresAfterSeconds: number;
};

export type Protocol = {
  Name: string;
  Version: number;
  Actions: Action[];
  Reads: string[];
  Events: ProtocolEvent[];
};

export type ProtocolCall = {
  target: string;
  idempotencyKey: string;
  payload: unknown;
};

export type ProtocolEvent = {
  name: string;
  title: string;
};

export type ProtocolInfo = {
  contract?: Protocol;
  id: string;
  actions: string[];
  reads: string[];
  events: ProtocolEvent[];
  providers: string[];
  consumers: string[];
  bound?: string;
};

export type ProtocolReleaseDescriptor = {
  name: string;
  version: number;
  actions: Action[];
  reads: string[];
  actionBindings?: Record<string, AssetRef>;
  actionRoles?: Record<string, string[]>;
  readBindings?: Record<string, AssetRef>;
};

export type ProvidedCapability = {
  ref: AssetRef;
  version: string;
  capability: Capability;
};

export type Provider = {
  id: string;
  kind: string;
  vendor?: string;
  baseUrl: string;
  secret?: string;
  wire: string;
};

export type QuarantinedRow = {
  reason: string;
  row: Record<string, unknown>;
};

export type Query = {
  businessTime?: BusinessTime;
  traversal?: LinkTraversal;
  set?: QuerySet;
  domain?: unknown;
  search?: string;
  sort?: string[];
  offset?: number;
  limit?: number;
  archived?: boolean;
};

export type QuerySet = {
  op: string;
  inputs: RecordSetPredicate[];
};

export type QueueHealth = {
  app: string;
  depth: number;
  oldestSeconds: number;
};

export type RecordChange = {
  change: string;
  schema: string;
  by: string;
  at: string;
  fields: FieldChange[];
};

export type RecordDelivery = {
  id: string;
  event: string;
  state: string;
  at: string;
  message?: string;
  receipt?: string;
};

export type RecordPage = {
  records: unknown[];
  total: number;
  partial?: boolean;
  formats?: Record<string, Record<string, FieldFormat>>;
};

export type RecordSet = {
  businessTime?: BusinessTime;
  name: string;
  from: string;
  binding?: AssetBinding;
  direction?: string;
  object: string;
  path?: string[];
  where?: Expr[];
  order: RecordSetSort[];
  limit: number;
};

export type RecordSetPredicate = {
  domain?: unknown;
  search?: string;
  set?: QuerySet;
};

export type RecordSetRef = {
  object: string;
  id: string;
  name: string;
};

export type RecordSetResult = {
  authorization?: AuthzCollection;
  object: string;
  records: unknown[];
  partial: boolean;
  sources?: string[];
  definition: string;
};

export type RecordSetSort = {
  expr: Expr;
  desc?: boolean;
};

export type RecordVersionPin = {
  reference: string;
  versionField: string;
  source: string;
  version: unknown;
  at: string;
};

export type RecordView = {
  effective?: boolean;
  values?: Record<string, unknown>;
  formats?: Record<string, FieldFormat>;
  valueErrors?: Record<string, string>;
  record: unknown;
  history: RecordChange[];
  related: Related[];
  processes: unknown[];
  approvals: unknown[];
  tasks: unknown[];
  files: unknown[];
  comments: unknown[];
  following: boolean;
  linked: Related[];
  activity: unknown[];
  deliveries?: RecordDelivery[];
};

export type RecordsAggregate = {
  partial?: boolean;
  window?: AggregateWindowResult;
  histogram?: HistogramResult;
  columns: Column[];
  rows: Record<string, unknown>[];
};

export type Refusal = {
  when: Expr;
  level: string;
  code: string;
  message: string;
  translations?: Record<string, string>;
  route?: TxnRef;
  record?: TxnStep;
  escalation?: RefusalEscalation;
};

export type RefusalCodeCount = {
  category: string;
  code: string;
  type: string;
  count: number;
};

export type RefusalEscalation = {
  count: number;
  windowSeconds: number;
  reviewerRole: string;
};

export type Related = {
  type: string;
  field: string;
  relation?: string;
  title: string;
  records: unknown[];
  total: number;
};

export type RelationMigrationPlan = {
  binding: AssetBinding;
  links: Link[];
  partial: boolean;
  migrated: boolean;
};

export type Relationship = {
  id: string;
  stereotype: string;
  kind?: string;
  source: string;
  target: string;
  role?: string;
  relation?: string;
  share?: number;
  primary?: boolean;
  from?: string;
  until?: string;
  periods?: Period[];
};

export type ReleaseActivateRequest = {
  candidateId: string;
  key: string;
  upgradeId?: string;
};

export type ReleaseActive = {
  id: string;
};

export type ReleaseAsset = {
  ref: AssetRef;
  contractVersion: number;
  sourceVersion: string;
  requires: AssetRef[];
  provides?: Capability[];
  body: unknown;
};

export type ReleaseDraftChanges = {
  changes: DraftChange[];
};

export type ReleaseDraftClosure = {
  drafts: JointDraftRef[];
};

export type ReleaseDraftsRequest = {
  kind: string;
  id: string;
};

export type ReleaseEvaluationRequest = {
  candidateId: string;
  planId?: string;
  agent?: string;
  key: string;
};

export type ReleaseEvaluationStarted = {
  id: string;
};

export type ReleaseExtensionReview = {
  type: string;
  base: AssetBinding;
  targetVersion: string;
  fields: string[];
  issue?: string;
};

export type ReleaseFieldAddition = {
  type: string;
  field: string;
  kind: string;
  records: number;
  sourceShape: string;
};

export type ReleasePage = {
  candidates: ReleaseSummary[];
  total: number;
  activeId: string;
};

export type ReleasePreview = {
  currentId?: string;
  candidateId?: string;
  included: AssetRef[];
  added: AssetRef[];
  removed: AssetRef[];
  changed: AssetRef[];
  diagnostic?: string;
  drafts?: JointDraftRef[];
  candidateActions: Action[];
};

export type ReleasePreviewRequest = {
  applicationDrafts?: boolean;
  kind?: string;
  id?: string;
  drafts?: JointDraftRef[];
};

export type ReleaseSaveRequest = {
  kind?: string;
  id?: string;
  drafts?: JointDraftRef[];
  candidateId: string;
  key: string;
};

export type ReleaseSaved = {
  id: string;
};

export type ReleaseSummary = {
  id: string;
  title: string;
  assets: number;
  from?: string;
  promotedAt?: string;
  promotedTo?: PromotionTarget[];
};

export type ReleaseUpgradePlan = {
  id: string;
  candidateId: string;
  activeId: string;
  additions: ReleaseFieldAddition[];
};

export type Result = {
  sources?: string[];
  value: Value;
  reasons?: Issue[];
};

export type RetainedArtifact = {
  version: string;
  digest?: string;
  at: string;
};

export type RolePermissions = {
  role: string;
  holders: number;
  actions: PermissionAction[];
};

export type RuleCollection = {
  set?: string;
  app: string;
  query: string;
  version: number;
  object: string;
  of?: string;
  policy: string;
};

export type RuleCriteria = {
  reads: RuleReadSpec;
  conditions: RuleCriterion[];
};

export type RuleCriterion = {
  expr: Expr;
  message: string;
};

export type RuleReadSpec = {
  object?: string;
  inputs?: Record<string, Type>;
  collections?: Record<string, RuleCollection>;
};

export type RuleTablePlan = {
  reads: RuleReadSpec;
  rows: RuleTableRow[];
  default?: unknown;
};

export type RuleTableRow = {
  condition: Expr;
  result: unknown;
};

export type RunStep = {
  at: string;
  tool: string;
  arguments?: string;
  rationale?: string;
  outcome: string;
  tokens?: number;
  calls?: ToolCall[];
  callId?: string;
  content?: string;
  sources?: string[];
};

export type SampleRecords = {
  type: string;
  records: unknown[];
};

export type SampleSet = {
  name: string;
  title: string;
  samples: SampleRecords[];
};

export type SavedReleaseReview = {
  ai?: ApplicationAI[];
  preview: ReleasePreview;
  assets: ReleaseAsset[];
  active: boolean;
  runningMatches: boolean;
  runningDiagnostic?: string;
  canActivate: boolean;
  activationDiagnostic?: string;
  upgradePlan?: ReleaseUpgradePlan;
  extensions?: ReleaseExtensionReview[];
  from?: string;
  promotedAt?: string;
  promotedTo?: PromotionTarget[];
};

export type SavedView = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  title: string;
  entity: string;
  state: string;
  owner: string;
};

export type SchemaVersion = {
  version: string;
  family: number;
};

export type SearchAround = {
  from: ObjectSet;
  link: AssetBinding;
  direction: string;
};

export type SelectionVariable = {
  name: string;
  object: AssetRef;
};

export type Sequence = {
  Name: string;
  Pattern: string;
  Yearly: boolean;
  SeedRecord?: string;
  SeedField?: string;
};

export type Session = {
  id: string;
  kind: string;
  token?: string;
  agent?: string;
  first: string;
  last: string;
  current: boolean;
  ended?: string;
};

export type SettingValue = {
  name: string;
  title: string;
  description: string;
  type: "boolean" | "integer" | "text" | "choice";
  default: string;
  choices?: string[];
  value: string;
};

export type SignIn = {
  issuer?: string;
  client?: string;
  identities?: Identity[];
};

export type Signal = {
  at: string;
  kind: string;
  by: string;
  detail?: string;
  value?: string;
};

export type SimulatedChange = {
  before?: unknown;
  type: string;
  id: string;
  record: unknown;
};

export type SimulatedFlow = {
  id: string;
  flow: string;
  version: number;
  dependencies?: string;
  release?: string;
  state: string;
  tokens: Token[];
  trace: TraceLine[];
  outputs?: Record<string, unknown>;
};

export type Simulation = {
  issues?: FieldIssue[];
  refusalCode?: string;
  unsupported?: boolean;
  traceTruncated?: boolean;
  input?: SimulationStep;
  trace?: DecisionTrace[];
  read?: unknown;
  rolledBack?: boolean;
  accepted: boolean;
  refusal?: string;
  changes: SimulatedChange[];
  matched?: boolean;
  flows?: SimulatedFlow[];
  tasks?: WorkTask[];
  functions?: FunctionRun[];
  computeMatched?: boolean;
  functionMatched?: boolean;
};

export type SimulationActor = {
  member: string;
  steps: Simulation[];
  testId: string;
  recovered: boolean;
  passed?: boolean;
};

export type SimulationSample = {
  type: string;
  records: unknown[];
};

export type SimulationStep = {
  read?: boolean;
  type: string;
  id: string;
  action: string;
  payload: unknown;
  expect?: string;
  as?: string;
  advanceSeconds?: number;
  flow?: string;
  step?: string;
  answer?: string;
  function?: FunctionFixture;
  compute?: ComputeFixture;
};

export type Source = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  connection?: string;
  profile?: "json" | "csv" | "odata" | "table";
  url?: string;
  entity?: string;
  filter?: string;
  since?: string;
  cursor?: string;
  allowPrivate?: boolean;
  header?: string;
  path?: string;
  dataset?: string;
  object?: string;
  key?: string;
  mapping?: SourceField[];
  stream?: boolean;
  every?: string;
  state: string;
  publisher?: string;
  publisherScopes?: string;
  puller?: string;
  pullerScopes?: string;
  requested?: boolean;
  last?: SourcePull;
  definitionVersion?: number;
  published?: string;
  definitionVersions?: string[];
};

export type SourceFailure = {
  id: string;
  outcome: string;
};

export type SourceField = {
  from: string;
  to: string;
  convert?: string;
};

export type SourcePull = {
  execution?: IntegrationExecution;
  definitionVersion?: string;
  at: string;
  rows: number;
  applied: number;
  failed: number;
  merged?: number;
  error?: string;
  failures?: SourceFailure[];
  cursor?: string;
};

export type Stamp = {
  by?: string;
  at?: string;
  change?: string;
};

export type State = {
  name: string;
  title: string;
  tone?: "info" | "success" | "warning" | "danger" | "neutral";
  description?: string;
};

export type StreamAggregate = {
  node: string;
  kernel?: string;
  above?: number;
  signal?: string;
  group?: string[];
  measures: string[];
};

export type StreamIntake = {
  sourceRecord: string;
  key: string;
  partition: string[];
  eventTime: string;
  value: string;
  offset?: "latest" | "earliest";
  fields?: string[];
  retry?: StreamPreparationRetry;
};

export type StreamOutput = {
  node: string;
  name: string;
  mode: string;
};

export type StreamPreparationRetry = {
  attempts: number;
  backoffMs: number;
};

export type StreamThreshold = {
  node: string;
  field: string;
  high: number;
  low: number;
  debounceMs: number;
  severity?: string;
};

export type StreamWindow = {
  node: string;
  windowMs: number;
  slideMs: number;
  watermarkMs: number;
  maxRecords: number;
  retry?: StreamPreparationRetry;
  lateEvents: string;
};

export type Structure = {
  id: string;
  name: string;
  kind: string;
  matrix?: boolean;
};

export type Submission = SubmissionJson;

export type SubmissionAnswer = {
  record?: ChangeRecord;
  error?: ErrorBody;
};

export type SupportAnswer = {
  read: string;
  answer?: unknown;
  error?: string;
};

export type SupportGrant = {
  id: string;
  tenant: string;
  member: string;
  reason: string;
  opened: string;
  expires: string;
  uses: number;
};

export type SupportRead = {
  tenant: string;
  grant: string;
  at: string;
  health: TenantHealth;
  audit: AuditEntry[];
  answers?: SupportAnswer[];
};

export type Table = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  name: string;
  title: string;
  description?: string;
  reads?: RuleReadSpec;
  inputs: TableColumn[];
  outputs: TableColumn[];
  rows: TableRow[];
  default?: string[];
  state: string;
  version?: number;
  versions?: string[];
  published?: string;
};

export type TableColumn = {
  name: string;
  title?: string;
  type: string;
};

export type TableRow = {
  expressions?: Expr[];
  when?: string[];
  then: string[];
};

export type Task = {
  applications?: AssetBinding[];
  id: string;
  kind: string;
  app: string;
  title: string;
  state: string;
  attempts: number;
  last?: string;
  due?: string;
  error?: string;
  withheld?: boolean;
};

export type TaskSummary = {
  id: string;
  title: string;
  state: string;
  answer?: string;
};

export type Team = {
  id: string;
  name: string;
  members: string[];
  grants: Grant[];
};

export type TenantHealth = {
  status: string;
  recoveryError?: string;
  started: string;
  built?: string;
  apps: number;
  queues: QueueHealth[];
  failed: number;
  deferred: string[];
  breakers: Breaker[];
  openBreakers: number;
  connectorsFailing: number;
  endpointsFailing: number;
};

export type TenantLink = {
  id: string;
  organization: string;
  tenant: string;
  root: string;
  connection: string;
  updatedAt?: string;
  content?: string;
};

export type TenantRecord = {
  id: string;
  name: string;
  settings: Record<string, string>;
  apps: string[];
  members: number;
  status: string;
  since?: string;
};

export type TenantTemplate = {
  name: string;
  title: string;
  description?: string;
  settings?: Record<string, string>;
  adminRoles?: Record<string, string>;
};

export type TestPlanDescriptor = {
  organization?: OrgSeed;
  name: string;
  title: string;
  roots: AssetRef[];
  at: string;
  samples?: SampleRecords[];
  steps: TestStep[];
};

export type TestStep = {
  read?: boolean;
  type: string;
  id: string;
  action: string;
  payload: string;
  expect: string;
  as?: string;
  advanceSeconds?: number;
  flow?: string;
  step?: string;
  answer?: string;
  function?: FunctionFixture;
  compute?: ComputeFixture;
};

export type Token = {
  id: number;
  step: string;
  branch?: string;
  waits?: string;
  attempts?: number;
  due?: string;
  task?: string;
  child?: string;
  error?: string;
  parent?: number;
  frames?: Frame[];
  input?: unknown;
  outputs?: Record<string, unknown>;
  operation?: string;
  loop?: LoopFrame;
  results?: Record<string, unknown>;
};

export type TokenView = {
  id: string;
  label: string;
  scopes: string[];
  until?: string;
  issued: string;
  lastUsed?: string;
  expired: boolean;
};

export type Tool = {
  name: string;
  description: string;
  properties: Record<string, unknown>;
  required?: string[];
};

export type ToolCall = {
  id: string;
  name: string;
  arguments: unknown;
};

export type Total = {
  day: string;
  member: string;
  model: string;
  calls: number;
  failed: number;
  input: number;
  output: number;
  cost: number;
  reported: number;
  estimated: number;
  costUnknown: number;
};

export type TraceLine = {
  at: string;
  step?: string;
  what: string;
  detail?: string;
  by?: string;
};

export type Transcript = {
  at: string;
  member: string;
  model: string;
  run?: string;
  request: unknown;
  answer: unknown;
  outcome: string;
};

export type TransformExpectation = {
  column: string;
  rule: string;
  value?: string;
};

export type TransformMeasure = {
  fn: string;
  column?: string;
  to: string;
};

export type TransformStep = {
  kind: string;
  columns?: string[];
  from?: string;
  to?: string;
  column?: string;
  type?: string;
  op?: string;
  value?: string;
  formulaExpr?: Expr;
  formula?: string;
  dataset?: string;
  match?: string;
  as?: string;
  outer?: boolean;
  desc?: boolean;
  measures?: TransformMeasure[];
};

export type TransitionInfo = {
  toInput?: string;
  name: string;
  schema: string;
  title: string;
  from: string[];
  to: string[];
};

export type TxnCall = {
  ref: AssetRef;
  version: number;
  view: string;
  inputs?: TxnSet[];
  resultPath?: string[];
  resultType: Type;
};

export type TxnEffect = {
  writeback: string;
  payload?: TxnSet[];
};

export type TxnForEach = {
  of?: string;
  set?: string;
  range?: TxnRange;
  steps: TxnStep[];
};

export type TxnIntervalFields = {
  subject: string;
  source: string;
  sourceVersion: string;
  sourceRevision: string;
  processingVersion: string;
  since: string;
  until: string;
  openStart: string;
  openEnd: string;
  retired: string;
  review: string;
};

export type TxnIntervals = {
  source: string;
  set: string;
  predecessors: AssetBinding;
  fields: TxnIntervalFields;
};

export type TxnJournal = {
  reverses?: string;
  set?: string;
  closedPeriod?: "reject" | "next-open";
  date?: Expr;
  text?: Expr;
  currency?: Expr;
  lines: TxnJournalLine[];
};

export type TxnJournalLine = {
  when?: Expr;
  account: Expr;
  debit?: Expr;
  credit?: Expr;
  text?: Expr;
  object?: Expr;
  partner?: Expr;
};

export type TxnLink = {
  binding: AssetBinding;
  parent: TxnRef;
  child: TxnRef;
};

export type TxnPost = {
  match: TxnSet[];
  field: string;
  amount: Expr;
  subtract?: boolean;
  floor?: boolean;
};

export type TxnRange = {
  from: Expr;
  until: Expr;
  unit: "day" | "hour" | "item";
  step: number;
  limit: number;
};

export type TxnRef = {
  type: string;
  action?: string;
  id?: string;
  via?: string;
};

export type TxnSet = {
  field: string;
  expr?: Expr;
  from?: string;
};

export type TxnStep = {
  name: string;
  kind: string;
  target?: TxnRef;
  changes?: TxnSet[];
  sequence?: Sequence;
  when?: Expr;
  link?: TxnLink;
  post?: TxnPost;
  journal?: TxnJournal;
  effect?: TxnEffect;
  work?: TxnWork;
  call?: TxnCall;
  forEach?: TxnForEach;
  intervals?: TxnIntervals;
  refusals?: Refusal[];
};

export type TxnWork = {
  title: string;
  body?: Expr;
  role?: string;
  structure?: string;
  unit?: Expr;
  member?: Expr;
  due?: Expr;
  answers?: string[];
  key?: Expr;
  close?: boolean;
};

export type Type = {
  object?: string;
  kind: string;
  fields?: Record<string, Type>;
  element?: Type;
  resource?: string;
  currency?: string;
};

export type Unit = {
  periods?: Period[];
  id: string;
  name: string;
  kind: string;
  legal?: boolean;
  external?: boolean;
  from?: string;
  until?: string;
  closed?: string;
  calendar?: string;
};

export type Upload = {
  hash: string;
  size: number;
  contentType: string;
  name: string;
};

export type Usage = {
  call?: string;
  applications?: AssetBinding[];
  resource?: AssetRef;
  run?: string;
  at: string;
  member: string;
  agent?: boolean;
  model: string;
  served?: string;
  input: number;
  output: number;
  cacheReadInput?: number;
  cacheWriteInput?: number;
  tokensReported?: boolean;
  tokensEstimated?: boolean;
  cost?: number;
  costReported?: boolean;
  millis: number;
  outcome: string;
};

export type Value = {
  kind: string;
  status: string;
  value?: unknown;
};

export type ValueSchema = {
  type: string;
  nullable?: boolean;
  discriminator?: string;
  variants?: Record<string, ValueSchema>;
  properties?: Record<string, ValueSchema>;
  required?: string[];
  items?: ValueSchema;
  enum?: string[];
  description?: string;
  maxLength?: number;
  maxItems?: number;
};

export type ValueType = {
  name: string;
  title: string;
  description: string;
  base: "text" | "longtext" | "integer" | "decimal" | "date" | "datetime";
  constraints?: InputConstraints;
  pattern?: string;
  choices?: string[];
};

export type Vendor = {
  id: string;
  name: string;
  baseUrl: string;
  wire: string;
};

export type Verdict = {
  allow: boolean;
  rule: string;
  reason: string;
  role?: string;
  policy?: string;
  layers: Layer[];
  withheld?: string[];
  definition?: string;
};

export type VersionPinBinding = {
  reference: string;
  version: string;
};

export type Versioning = {
  key: string;
  version: string;
  validFrom: string;
  validUntil?: string;
  pin: string;
};

export type View = {
  id: string;
  name: string;
  viewpoint: string;
  kind?: string;
  elements: string[];
  layout?: Record<string, number[]>;
  context?: string[];
  pins?: Pin[];
  asOf?: string;
};

export type Viewpoint = {
  id: string;
  title: string;
  note: string;
  elements: string[];
  context?: string[];
  relationships: string[];
};

export type WindowReference = {
  source: string;
  aggregate?: string;
};

export type WindowSummary = {
  since: string;
  until: string;
  count: number;
  firstAt?: string;
  lastAt?: string;
  first?: string;
  last?: string;
  min?: string;
  max?: string;
  sum?: string;
  mean?: string;
  unit: string;
};

export type Word = {
  text: string;
  translations: Record<string, string>;
  own: Record<string, string>;
};

export type WordsView = {
  app: string;
  apps: string[];
  languages: string[];
  words: Word[];
};

export type WorkTask = {
  id: string;
  revision: number;
  created: Stamp;
  changed: Stamp;
  archived?: boolean;
  versionPins?: RecordVersionPin[];
  title: string;
  body?: string;
  withheld?: boolean;
  ref?: string;
  app: string;
  key?: string;
  candidates: string[];
  assignee?: string;
  due?: string;
  state: "open" | "done" | "canceled";
  answers?: string[];
  answer?: string;
};

export const imageRegionLimits = {"maxIDBytes":128,"maxLabelBytes":256,"maxRegions":64} as const;
