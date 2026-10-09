// BPMN notation for the flow family (ADR-0086 D4): one vocabulary of shapes and
// markers, so a process reads the same way in every view that draws it. A notation
// says which shape an activity is drawn as and which marker sits inside it; the
// owner's own icon still says what the activity does.
//
// Node classes (ADR-0089) sit one level above: what a node *is*. A class is a
// declaration, not a drawing — it hands a node its glyph, its palette group and
// its default notation, so a new scenario picks a class instead of commissioning
// a renderer.
import { ArrowRightLeft, Blocks, Brain, Braces, Circle, CircleSlash, Clock, Code2, Database, FileText, Flag, GitBranch, Mail, Play, Plus, User, Workflow, X, Zap } from "lucide-react";
import type { ReactNode } from "react";
import { t } from "../../i18n";
import type { Tone } from "../../components/StatusTag";

/** The outline a notation is drawn as. */
export type FlowShape = "task" | "gateway" | "event" | "subprocess";

export type FlowNotation =
  | "task" | "service-task" | "user-task" | "script-task" | "call-activity" | "subprocess" | "data-object"
  | "gateway-exclusive" | "gateway-parallel" | "gateway-inclusive" | "gateway-event"
  | "event-start" | "event-intermediate" | "event-end" | "event-timer" | "event-message" | "event-error" | "event-terminate";

export const flowNotations: FlowNotation[] = ["task", "service-task", "user-task", "script-task", "call-activity", "subprocess", "data-object",
  "gateway-exclusive", "gateway-parallel", "gateway-inclusive", "gateway-event",
  "event-start", "event-intermediate", "event-end", "event-timer", "event-message", "event-error", "event-terminate"];

/** An event attached to an activity's border: the timeout it may hit, the error it
 * may escape through. It is a marker on the shape, not a step of its own. */
export type FlowBoundary = { id: string; notation: "event-timer" | "event-error" | "event-message"; label?: string; tone?: Tone };

export function flowShape(notation?: FlowNotation, kind?: string): FlowShape {
  const resolved = notation ?? notationOf(kind ?? "");
  if (resolved.startsWith("gateway")) return "gateway";
  if (resolved.startsWith("event")) return "event";
  if (resolved === "subprocess" || resolved === "call-activity") return "subprocess";
  return "task";
}

/** The step kinds the host's Process and Flow declarations answer with, read as
 * BPMN. An owner may name a notation instead; this is only what a kind says by itself. */
export function notationOf(kind: string): FlowNotation {
  switch (kind) {
    case "payload": return "event-start";
    case "ask": return "user-task";
    case "wait": return "event-timer";
    case "branch": case "switch": return "gateway-exclusive";
    case "fork": case "all": case "any": case "join": return "gateway-parallel";
    case "subflow": case "call": return "call-activity";
    case "end": return "event-end";
    case "fail": return "event-terminate";
    case "break": case "continue": return "event-intermediate";
    case "query": case "action": case "act": case "transform": case "ai": case "compute": case "agent": return "service-task";
    default: return "task";
  }
}

/** A step kind that repeats carries BPMN's loop marker. */
export const loops = new Set(["foreach", "while"]);

/** The marker drawn inside a shape. A task keeps its owner's icon instead. */
export function notationGlyph(notation: FlowNotation): ReactNode | undefined {
  switch (notation) {
    case "user-task": return <User />;
    case "call-activity": case "subprocess": return <Plus />;
    case "gateway-exclusive": return <X />;
    case "gateway-parallel": return <Plus />;
    case "gateway-inclusive": return <Circle />;
    case "gateway-event": return <Zap />;
    case "event-timer": return <Clock />;
    case "event-message": return <Mail />;
    case "event-error": return <Zap />;
    case "event-terminate": return <CircleSlash />;
    case "data-object": return <FileText />;
    default: return undefined;
  }
}

/** What the shape is called, for its tooltip and accessible name. */
export function notationTitle(notation: FlowNotation): string {
  return t(({
    task: "Task", "service-task": "Service task", "user-task": "User task", "script-task": "Script task",
    "call-activity": "Call activity", subprocess: "Sub-process", "data-object": "Data object",
    "gateway-exclusive": "Exclusive gateway", "gateway-parallel": "Parallel gateway",
    "gateway-inclusive": "Inclusive gateway", "gateway-event": "Event gateway",
    "event-start": "Start event", "event-intermediate": "Intermediate event", "event-end": "End event",
    "event-timer": "Timer event", "event-message": "Message event", "event-error": "Error event",
    "event-terminate": "Terminate event",
  } as Record<FlowNotation, string>)[notation]);
}

/** How thick the outline of an event is: thin to start, double in the middle, thick to end. */
export const eventRing = (notation: FlowNotation): number => notation === "event-start" ? 1.5 : notation === "event-end" || notation === "event-terminate" ? 4 : 2.5;

/* -------------------------------------------------------------------------- */
/* Node classes (ADR-0089): what a node is, decided by declaration.            */
/* -------------------------------------------------------------------------- */

/** What a node is, for drawing and grouping. The standing scenarios are named
 * here; an owner may still name their own class id, which draws as a plain task
 * under its own group. Whatever the id, there is one table to look it up in and
 * one place to add to. */
export type FlowNodeClass =
  | "code"       // a typed code or script step
  | "document"   // a document, dataset or knowledge item
  | "function"   // a callable function or model
  | "action"     // an action, API call or service task
  | "query"      // a query
  | "transform"  // a transform
  | "ai"         // an AI step
  | "human"      // a user task
  | "trigger"    // a trigger, entry point or wait
  | "control"    // branching, gateways, joins
  | "flow"       // a flow or workflow step
  | "lifecycle"  // lifecycle states and actions
  | "end"        // a terminal
  | "task"       // the plain default
  | (string & {});

/** The class table: the only place a class's glyph, palette group and default
 * BPMN notation are written down. Titles are English source text; the palette
 * translates them when it groups by class. */
export const flowNodeClasses: Record<string, { title: string; icon: ReactNode; notation?: FlowNotation }> = {
  code: { title: "Code", icon: <Code2 />, notation: "script-task" },
  document: { title: "Document", icon: <FileText />, notation: "data-object" },
  function: { title: "Function", icon: <Braces />, notation: "service-task" },
  action: { title: "Action", icon: <Zap />, notation: "service-task" },
  query: { title: "Query", icon: <Database />, notation: "service-task" },
  transform: { title: "Transform", icon: <ArrowRightLeft />, notation: "service-task" },
  ai: { title: "AI", icon: <Brain />, notation: "service-task" },
  human: { title: "Human task", icon: <User />, notation: "user-task" },
  trigger: { title: "Trigger", icon: <Play />, notation: "event-start" },
  control: { title: "Branch", icon: <GitBranch />, notation: "gateway-exclusive" },
  flow: { title: "Flow", icon: <Workflow /> },
  lifecycle: { title: "Lifecycle", icon: <Workflow /> },
  end: { title: "End", icon: <Flag /> },
  task: { title: "Task", icon: <Blocks /> },
};

/** The class a BPMN notation stands for, when the owner named no class. This is
 * the fallback, not the source: an owner who knows what its node is says so. */
function classFromNotation(notation: FlowNotation): FlowNodeClass {
  if (notation.startsWith("gateway")) return "control";
  if (notation.startsWith("event")) return "trigger";
  if (notation === "user-task") return "human";
  if (notation === "script-task") return "code";
  if (notation === "data-object") return "document";
  if (notation === "call-activity" || notation === "subprocess") return "flow";
  if (notation === "service-task") return "action";
  return "task";
}

/** The class a node draws under: the class it declares, else the class its BPMN
 * reading stands for. Accepts a catalog kind or a canvas node. */
export function flowNodeClassOf(node: { class?: string; notation?: FlowNotation; id?: string }): FlowNodeClass {
  return node.class ?? classFromNotation(node.notation ?? notationOf(node.id ?? ""));
}

/** The glyph a node carries when its owner gives it none — the one lookup every
 * view shares, so the palette, the block and the structure tree agree. */
export function flowNodeIcon(node: { icon?: ReactNode; class?: string; notation?: FlowNotation; id?: string }): ReactNode {
  return node.icon ?? flowNodeClasses[flowNodeClassOf(node)]?.icon ?? <Blocks />;
}

/** The palette group a node lands in: the group it names, else its class title,
 * translated. A class id the table does not know still groups under itself. */
export function flowNodeGroup(node: { group?: string; class?: string; notation?: FlowNotation; id?: string }): string {
  return node.group ?? t(flowNodeClasses[flowNodeClassOf(node)]?.title ?? node.class ?? "Task");
}
