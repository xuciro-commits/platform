import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
vi.mock("@xyflow/react",()=>({Handle:(props:{"aria-label"?:string})=><span aria-label={props["aria-label"]}/>,Position:{Top:"top",Bottom:"bottom",Left:"left",Right:"right"},useUpdateNodeInternals:()=>()=>{}}));
import {BlockNode,type FlowBlockNode} from "./BlockNode";
import type {NodeProps} from "@xyflow/react";
afterEach(cleanup);
test("circle view badges remain decorative while original record label and detail remain accessible, and ordinary blocks keep labels",()=>{
 const props={id:"record",selected:false,dragging:false,draggable:false,selectable:false,deletable:false,type:"block",zIndex:0,isConnectable:false,positionAbsoluteX:0,positionAbsoluteY:0,data:{id:"record",kind:"step",label:"<b>Original peer</b>",detail:"sample.sensor/A",position:{x:0,y:0},compact:true,editable:false,collapsedView:false,direction:"right",tone:"success",circle:{badge:"S",size:24},definition:{id:"step",title:"Step",category:"flow",inputs:[],outputs:[]}}} as NodeProps<FlowBlockNode>,view=render(<BlockNode {...props}/>);expect(screen.getByText("S").getAttribute("aria-hidden")).toBe("true");expect(screen.getByLabelText("<b>Original peer</b>").title).toContain("sample.sensor/A");expect(view.container.querySelector("b")).toBeNull();
 view.rerender(<BlockNode {...props} data={{...props.data,circle:undefined}}/>);expect(screen.getByText("<b>Original peer</b>")).toBeTruthy();expect(screen.getByText("sample.sensor/A")).toBeTruthy();expect(screen.queryByText("S")).toBeNull();
});
