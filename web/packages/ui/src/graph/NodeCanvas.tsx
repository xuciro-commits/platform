import { BlockCanvas, type BlockCanvasProps } from "./BlockCanvas";

export { canvasNodeHeight, canvasNodeWidth, canvasPlacement, validateCanvasConnection } from "./model";
export type { BlockDiagnostic, BlockStatus, CanvasAddContext, CanvasEdge, CanvasHistory, CanvasNode, CanvasPosition, NodeCatalog, NodeKind, NodePort } from "./model";

/** Semantic owner adapter; shared block rendering, viewport and interaction core with Graph. */
export function NodeCanvas(props: BlockCanvasProps) {
  return <BlockCanvas {...props} />;
}
