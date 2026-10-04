import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {AIResult} from "./AIResult";
afterEach(cleanup);
test("typed AI replies remain literal text and the caller owns question and permission state",()=>{
 const onQuestion=vi.fn(),onRun=vi.fn(),onReset=vi.fn(),props={kind:"chatbot",turns:[{id:"CALL",state:"ready",question:"Original question",fields:[{name:"reply",value:"<script>execute()</script>"}]}],question:"Draft",suggestions:["Summarise"],disabled:false,busy:false,onQuestion,onRun,onReset},view=render(<AIResult {...props}/>);
 expect(screen.getByText("<script>execute()</script>")).toBeTruthy();expect(view.container.querySelector("script")).toBeNull();fireEvent.change(screen.getByRole("textbox",{name:"AI question"}),{target:{value:"New draft"}});expect(onQuestion).toHaveBeenCalledWith("New draft");expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("Draft");fireEvent.click(screen.getByRole("button",{name:"Summarise"}));expect(onRun).toHaveBeenCalledWith("Summarise");onRun.mockClear();view.rerender(<AIResult {...props} disabled/>);fireEvent.click(screen.getByRole("button",{name:"Send AI question"}));fireEvent.keyDown(screen.getByRole("textbox"),{key:"Enter"});expect(onRun).not.toHaveBeenCalled();fireEvent.click(screen.getByRole("button",{name:"Reset AI view"}));expect(onReset).toHaveBeenCalledOnce();
});
