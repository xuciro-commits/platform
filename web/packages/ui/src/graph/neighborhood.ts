import type {CanvasPosition} from "./model";

export type NeighborhoodLayoutGroup={side:"right"|"left";nodes:readonly string[]};
/** The original two half-ellipses are presentation, independent of relationship queries. */
export function neighborhoodPositions(root:string,groups:readonly NeighborhoodLayoutGroup[]):Record<string,CanvasPosition> {
 const positions=Object.create(null) as Record<string,CanvasPosition>;positions[root]={x:158,y:68};
 for(const group of groups)group.nodes.forEach((id,index)=>{if(Object.hasOwn(positions,id))return;const angle=(group.side==="right"?-Math.PI/2:Math.PI/2)+index/Math.max(1,group.nodes.length)*Math.PI;positions[id]={x:180+Math.cos(angle)*90-12,y:90+Math.sin(angle)*55-12};});
 return positions;
}
