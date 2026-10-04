type Point={x:number;y:number};
const W=1000,H=500;
/** Unwrap and clip at the antimeridian instead of drawing a border across the ocean. */
export function landRingPath(ring:number[][]){
 if(!ring.length)return "";const points:Point[]=[];
 for(const [longitude,latitude]of ring){let x=(longitude!+180)/360*W;const previous=points.at(-1)?.x;if(previous!==undefined){while(x-previous>W/2)x-=W;while(x-previous < -W/2)x+=W;}points.push({x,y:(90-latitude!)/180*H});}
 const first=points[0]!,last=points.at(-1)!;
 if(Math.abs(last.x-first.x)>W/2){const pole=points.reduce((n,p)=>n+p.y,0)/points.length>H/2?H:0;points.push({x:last.x,y:pole},{x:first.x,y:pole},first);}
 const clip=(input:Point[],edge:number,right:boolean)=>{const output:Point[]=[];for(let i=0;i<input.length;i++){const a=input[i]!,b=input[(i+1)%input.length]!,inside=(p:Point)=>right?p.x<=edge:p.x>=edge,ai=inside(a),bi=inside(b);if(ai)output.push(a);if(ai!==bi)output.push({x:edge,y:a.y+(b.y-a.y)*(edge-a.x)/(b.x-a.x)});}return output;};
 const min=Math.min(...points.map(p=>p.x)),max=Math.max(...points.map(p=>p.x));let path="";
 for(let offset=Math.ceil(-max/W);offset<=Math.floor((W-min)/W);offset++){const part=clip(clip(points.map(p=>({...p,x:p.x+offset*W})),0,false),W,true);if(part.length>=3)path+=part.map((p,i)=>`${i?"L":"M"}${p.x.toFixed(1)} ${p.y.toFixed(1)}`).join("")+"Z";}
 return path;
}
