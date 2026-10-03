import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import type {GraphNode,GraphEdge} from "../graph/Graph";
import type {EntityInfo,EntityRecord} from "./Records";
const captured=vi.hoisted(()=>({props:undefined as unknown}));
vi.mock("../graph/Graph",()=>({Graph:(props:{nodes:GraphNode[];edges:GraphEdge[];onOpen?:(node:GraphNode)=>void})=>{captured.props=props;return <div>{props.nodes.map(node=>props.onOpen?<button key={node.id} onClick={()=>props.onOpen?.(node)}>{node.label}</button>:<span key={node.id}>{node.label}</span>)}</div>;}}));
import {RecordNeighborhood,type RecordNeighborhoodGroup} from "./RecordNeighborhood";
afterEach(cleanup);
const info=(type:string)=>({type,title:type,fields:[{name:"name",title:"Name",type:"text"}]}) as EntityInfo;
const row=(id:string):EntityRecord=>({id,revision:1,created:{},changed:{},name:"<b>Same record</b>"});
const root={object:"sample.asset",record:row("SHARED"),info:info("sample.asset"),labelField:"name"};
const groups:RecordNeighborhoodGroup[]=[{id:"sensors:link-v1",records:[row("SHARED"),row("S2")],info:info("sample.sensor"),bindingTitle:"Original sensors",badge:"S",tone:"success",total:19,limit:4},{id:"alerts:link-v3",records:[row("SHARED")],info:info("sample.alert"),bindingTitle:"Original alerts",badge:"A",tone:"danger",total:11,limit:3}];
test("the neighborhood retains composite original identities, source groups and honest partial counts without business joins",()=>{
 const open=vi.fn(),view=render(<RecordNeighborhood root={root} groups={groups} onOpen={open}/>),props=captured.props as {nodes:GraphNode[];edges:GraphEdge[];positions:Record<string,{x:number;y:number}>};
 expect(props.nodes.map(node=>JSON.parse(decodeURIComponent(node.id)))).toEqual([["sample.asset","SHARED"],["sample.sensor","SHARED"],["sample.sensor","S2"],["sample.alert","SHARED"]]);expect(new Set(props.edges.map(edge=>edge.id)).size).toBe(3);expect(props.edges.every(edge=>edge.directed===false)).toBe(true);expect(props.nodes.slice(1).map(node=>node.circle?.badge)).toEqual(["S","S","A"]);expect(props.nodes[0]!.circle?.badge).toBe("SHARED");expect(Object.keys(props.positions).sort()).toEqual(props.nodes.map(node=>node.id).sort());expect(screen.getByText("Original sensors · Showing 2 of 19 neighbors.")).toBeTruthy();expect(screen.getByText("Original alerts · Showing 1 of 11 neighbors.")).toBeTruthy();expect(view.container.querySelector("b")).toBeNull();fireEvent.click(screen.getByRole("button",{name:"SHARED · sample.alert/SHARED"}));expect(open).toHaveBeenCalledWith("sample.alert",groups[1]!.records[0]);
 view.rerender(<RecordNeighborhood root={root} groups={groups} onOpen={open} enabled={false}/>);expect(screen.queryByRole("button")).toBeNull();view.rerender(<RecordNeighborhood root={root} groups={groups}/>);expect(screen.queryByRole("button")).toBeNull();
});
test("private labels, malformed identity, source group budgets and false totals refuse the graph instead of fabricating neighbors",()=>{
 const view=render(<RecordNeighborhood root={{...root,labelField:"private"}} groups={groups}/>);expect(screen.getByRole("alert")).toBeTruthy();
 for(const next of [[{...groups[0]!,limit:3},groups[1]!],[{...groups[0]!,records:[row("A"),row("A")]},groups[1]!],[{...groups[0]!,total:0},groups[1]!],[groups[0]!,{...groups[1]!,records:[row("1"),row("2"),row("3"),row("4")] }],[groups[0]!,{...groups[1]!,labelField:"private"}],[groups[0]!,{...groups[1]!,badge:"S"}]]){view.rerender(<RecordNeighborhood root={root} groups={next}/>);expect(screen.getByRole("alert")).toBeTruthy();}
 view.rerender(<RecordNeighborhood root={root} groups={groups.map(group=>({...group,records:[],total:0}))}/>);const props=captured.props as {nodes:GraphNode[];edges:GraphEdge[]};expect(props.nodes).toHaveLength(1);expect(props.edges).toHaveLength(0);expect(screen.getAllByText(/Showing 0 of 0 neighbors/)).toHaveLength(2);
});
test("permission-cropped groups retain their original badge side and total while hidden groups never become fake zero counts",()=>{
 const view=render(<RecordNeighborhood root={root} groups={[groups[1]!]}/>),rootID=encodeURIComponent(JSON.stringify([root.object,root.record.id])),alertID=encodeURIComponent(JSON.stringify([groups[1]!.info.type,groups[1]!.records[0]!.id]));let props=captured.props as {nodes:GraphNode[];edges:GraphEdge[];positions:Record<string,{x:number;y:number}>};expect(props.nodes).toHaveLength(2);expect(props.nodes[1]!.circle?.badge).toBe("A");expect(props.positions[alertID]!.y).toBeGreaterThan(props.positions[rootID]!.y);expect(screen.getByText("Original alerts · Showing 1 of 11 neighbors.")).toBeTruthy();expect(screen.queryByText(/Original sensors/)).toBeNull();
 view.rerender(<RecordNeighborhood root={root} groups={[groups[0]!]}/>);props=captured.props as typeof props;const sensorID=encodeURIComponent(JSON.stringify([groups[0]!.info.type,groups[0]!.records[0]!.id]));expect(props.positions[sensorID]!.y).toBeLessThan(props.positions[rootID]!.y);expect(screen.queryByText(/Original alerts/)).toBeNull();
 view.rerender(<RecordNeighborhood root={root} groups={[]}/>);props=captured.props as typeof props;expect(props.nodes).toHaveLength(1);expect(props.edges).toHaveLength(0);expect(screen.getByRole("status").textContent).toBe("No readable neighborhood relations.");expect(screen.queryByText(/Showing 0 of 0 neighbors/)).toBeNull();
});
test("quoted, bracketed and delimiter-containing original identities remain collision-free and usable by canvas CSS selectors",()=>{
 const shared='same"[id]\\/:,%',rootObject='asset"[root]\\/:,%',peerObject='sensor"[peer]\\/:,%';
 const originalRoot={object:rootObject,record:row(shared),info:info(rootObject),labelField:"id"},first=row(shared),second=row('same"[id]\\/:,%extra');
 const originals:RecordNeighborhoodGroup[]=[{id:'link"[S]\\/:,%',records:[first,second],info:info(peerObject),bindingTitle:"Original quoted sensors",badge:"S",tone:"success",total:2,limit:4},{id:'link"[A]\\/:,%',records:[row(shared)],info:info("alerts"),bindingTitle:"Original quoted alerts",badge:"A",tone:"danger",total:1,limit:3}],open=vi.fn();
 render(<RecordNeighborhood root={originalRoot} groups={originals} onOpen={open}/>);const props=captured.props as {nodes:GraphNode[];edges:GraphEdge[];onOpen:(node:GraphNode)=>void};
 expect(new Set(props.nodes.map(node=>node.id)).size).toBe(4);expect(props.nodes.map(node=>JSON.parse(decodeURIComponent(node.id)))).toEqual([[rootObject,shared],[peerObject,shared],[peerObject,second.id],["alerts",shared]]);
 const canvas=document.createElement("div");for(const item of [...props.nodes,...props.edges]){const element=document.createElement("div");element.setAttribute("data-id",item.id!);canvas.append(element);expect(canvas.querySelector(`[data-id="${item.id}"]`)).toBe(element);}
 expect(new Set(props.edges.map(edge=>edge.id)).size).toBe(3);props.edges.forEach((edge,index)=>expect(JSON.parse(decodeURIComponent(edge.id!))).toEqual([index<2?originals[0]!.id:originals[1]!.id,edge.from,edge.to]));
 props.onOpen(props.nodes[1]!);expect(open).toHaveBeenCalledWith(peerObject,first);props.onOpen(props.nodes[0]!);expect(open).toHaveBeenLastCalledWith(rootObject,originalRoot.record);
});
