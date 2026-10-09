import type { CanvasBox, CanvasPosition } from "../types";
export type LayoutAlgorithm = "layered" | "mrtree" | "radial" | "stress" | "force" | "rectpacking";
export type LayoutDirection = "right" | "down" | "left" | "up";
export type LayoutPort = { id: string; side: "NORTH" | "SOUTH" | "EAST" | "WEST"; x: number; y: number };
export type LayoutNode = CanvasBox & { id: string; ports?: LayoutPort[]; lane?: string };
export type LayoutEdge = { id: string; source: string; target: string; sourcePort?: string; targetPort?: string; label?: string; reverse?: boolean };
export type LayoutRequest = { nodes: LayoutNode[]; edges: LayoutEdge[]; algorithm?: LayoutAlgorithm; direction?: LayoutDirection; gap?: number; layerGap?: number; lanes?: string[] };
export type LayoutRoute = { points: CanvasPosition[]; label?: CanvasPosition; sourceSide?: LayoutPort["side"]; targetSide?: LayoutPort["side"] };
export type LayoutResult = { positions: Record<string, CanvasPosition>; routes: Record<string, LayoutRoute>; boxes: Record<string, CanvasBox> };
