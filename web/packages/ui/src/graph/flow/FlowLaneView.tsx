// A BPMN lane band (ADR-0087 D1): the responsibility a set of steps sits inside.
// It is scenery — never selected, never dragged, never connected, always behind the
// steps — and its title reads along the band's leading edge the way paper BPMN does.
import type { Node, NodeProps } from "@xyflow/react";
import type { CSSProperties } from "react";
import { cn } from "../../lib/cn";
import type { FlowLane } from "./model";

export type FlowLaneData = { lane: FlowLane; vertical: boolean };
export type FlowLaneShapeNode = Node<FlowLaneData, "lane">;

export function FlowLaneView({ data }: NodeProps<FlowLaneShapeNode>) {
  const { lane, vertical } = data;
  return <div className={cn("platform-flow-lane", vertical && "platform-flow-lane-vertical", lane.tone && `platform-flow-lane-${lane.tone}`)}
    style={{ "--lane-color": lane.tone ? `var(--tone-${lane.tone})` : undefined } as CSSProperties}>
    <span className="platform-flow-lane-title">{lane.title ?? lane.id}</span>
  </div>;
}
