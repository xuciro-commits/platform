import type { Node, NodeProps } from "@xyflow/react";
import type { CSSProperties } from "react";
import { cn } from "../../lib/cn";
import type { FlowLane } from "./model";

type FlowLaneData = { lane: FlowLane; vertical: boolean };
export type FlowLaneShapeNode = Node<FlowLaneData, "lane">;

export function FlowLaneView({ data }: NodeProps<FlowLaneShapeNode>) {
  const { lane, vertical } = data;
  return <div className={cn("platform-flow-lane", vertical && "platform-flow-lane-vertical", lane.tone && `platform-flow-lane-${lane.tone}`)}
    style={{ "--lane-color": lane.tone ? `var(--tone-${lane.tone})` : undefined } as CSSProperties}>
    <span className="platform-flow-lane-title">{lane.title ?? lane.id}</span>
  </div>;
}
