import {cleanup,render} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import type {BlockCanvasProps} from "./BlockCanvas";
const captured=vi.hoisted(()=>({props:undefined as unknown}));
vi.mock("./BlockCanvas",()=>({BlockCanvas:(props:BlockCanvasProps)=>{captured.props=props;return <div/>;}}));
import {Graph} from "./Graph";
afterEach(cleanup);
test("graph adapters preserve optional original edge identities, supplied positions and circle badges while ordinary layouts stay compatible",()=>{
 const open=vi.fn(),nodes=[{id:"root",label:"Original root",circle:{size:44,badge:"ROOT"}},{id:"peer",label:"Original peer",circle:{size:24,badge:"S"}}],position={root:{x:50,y:50},peer:{x:100,y:25}},view=render(<Graph nodes={nodes} edges={[{id:"original-link",from:"root",to:"peer",directed:false}]} positions={position} onOpen={open}/>),props=captured.props as BlockCanvasProps;
 expect(props.nodes.map(node=>node.circle?.badge)).toEqual(["ROOT","S"]);expect(props.nodes[0]!.position).toEqual(position.root);expect(props.edges[0]!.id).toBe("original-link");expect(props.edges[0]!.directed).toBe(false);props.onSelect?.("peer");expect(open).toHaveBeenCalledWith(nodes[1]);
 view.rerender(<Graph nodes={[{id:"A",label:"A"},{id:"B",label:"B"}]} edges={[{from:"A",to:"B"}]}/>);const original=captured.props as BlockCanvasProps;expect(original.nodes[1]!.position.x).toBeGreaterThan(original.nodes[0]!.position.x);expect(original.nodes[0]!.circle).toBeUndefined();expect(original.edges[0]!.id).toBe("0:A>B");expect(original.edges[0]!.directed).toBeUndefined();expect(original.onSelect).toBeUndefined();
});
