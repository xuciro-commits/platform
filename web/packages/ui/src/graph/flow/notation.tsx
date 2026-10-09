// BPMN notation for the flow family (ADR-0086 D4): one vocabulary of shapes and
// markers, so a process reads the same way in every view that draws it. A notation
// says which shape an activity is drawn as and which marker sits inside it; the
// owner's own icon still says what the activity does.
import { Circle, CircleSlash, Clock, FileText, Mail, Plus, User, X, Zap } from "lucide-react";
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
