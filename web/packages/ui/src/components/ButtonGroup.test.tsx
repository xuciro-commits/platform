import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {ButtonGroup} from "./ButtonGroup";
afterEach(cleanup);
test("button controls activate by stable identity and keyboard movement skips unbound controls",()=>{
 const activate=vi.fn(),buttons=[{id:"open",title:"Open",variant:"primary",icon:"arrow"},{id:"unbound",title:"Unbound"},{id:"close",title:"Close",variant:"danger",icon:"trash"}];const {rerender}=render(<ButtonGroup buttons={buttons} label="Commands" isBound={id=>id!=="unbound"} onActivate={activate}/>);
 fireEvent.click(screen.getByRole("button",{name:"Open"}));expect(activate).toHaveBeenCalledWith("open");expect((screen.getByRole("button",{name:"Unbound"}) as HTMLButtonElement).disabled).toBe(true);const open=screen.getByRole("button",{name:"Open"});open.focus();fireEvent.keyDown(open,{key:"ArrowRight"});expect(document.activeElement).toBe(screen.getByRole("button",{name:"Close"}));
 rerender(<ButtonGroup buttons={[buttons[2]!,buttons[0]!]} label="Commands" isBound={()=>true} onActivate={activate}/>);fireEvent.click(screen.getByRole("button",{name:"Close"}));expect(activate).toHaveBeenLastCalledWith("close");
});
