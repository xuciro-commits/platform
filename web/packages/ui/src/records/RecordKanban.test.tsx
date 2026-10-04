import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {RecordKanban,kanbanMove} from "./RecordKanban";
import type {EntityRecord} from "./Records";
afterEach(cleanup);
const record:EntityRecord={id:"R",revision:1,created:{at:"2026-10-02",by:"test"},changed:{at:"2026-10-02",by:"test"},title:"Task",state:"open"};
const lanes=[{name:"open",title:"Open"},{name:"done",title:"Done"}],move={schema:"sample.task.complete",title:"Complete",from:["open"],to:"done"};
test("a move emits an original command without optimistically changing the record",()=>{
 const select=vi.fn(),act=vi.fn();const view=render(<RecordKanban records={[record]} lanes={lanes} stateField="state" labelField="title" label="Board" moves={[move]} onMove={act} onSelect={select}/>);
 const open=screen.getByRole("region",{name:"Open"}),done=screen.getByRole("region",{name:"Done"});fireEvent.click(within(open).getByRole("button",{name:"Task"}));expect(select).toHaveBeenLastCalledWith(record);
 fireEvent.change(screen.getByRole("combobox",{name:"Move Task with action"}),{target:{value:move.schema}});expect(act).toHaveBeenLastCalledWith(record,move.schema);expect(within(open).getByRole("button",{name:"Task"})).toBeTruthy();expect(within(done).queryByRole("button",{name:"Task"})).toBeNull();
 view.rerender(<RecordKanban records={[{...record,state:"done",revision:2}]} lanes={lanes} stateField="state" labelField="title" label="Board" moves={[move]} onMove={act} onSelect={select}/>);expect(within(open).queryByRole("button",{name:"Task"})).toBeNull();expect(within(done).getByRole("button",{name:"Task"})).toBeTruthy();
});
test("ambiguous destinations have no drag default, preview does not offer moves and unknown states create no lane",()=>{
 expect(kanbanMove(record,"state","done",[move,{...move,schema:"sample.task.other"}])).toBeUndefined();expect(kanbanMove(record,"state","done",[move])?.schema).toBe(move.schema);
 render(<RecordKanban records={[record,{...record,id:"UNKNOWN",state:"private"}]} lanes={lanes} stateField="state" labelField="title" label="Board" moves={[move]} onSelect={()=>{}}/>);expect(screen.queryByRole("combobox")).toBeNull();expect(screen.queryByRole("region",{name:"private"})).toBeNull();expect(screen.getByRole("status").textContent).toContain("1 records");
});

test("one parameterized action keeps separate destination options and emits the declared target without a state write",()=>{
 const act=vi.fn(),dynamic={...move,input:"status"},pause={...dynamic,to:"pause"};render(<RecordKanban records={[record]} lanes={[...lanes,{name:"pause",title:"Pause"}]} stateField="state" labelField="title" label="Board" moves={[dynamic,pause]} onMove={act} onSelect={()=>{}}/>);
 fireEvent.change(screen.getByRole("combobox",{name:"Move Task with action"}),{target:{value:`${move.schema}/pause`}});expect(act).toHaveBeenLastCalledWith(record,move.schema,"pause");expect(within(screen.getByRole("region",{name:"Open"})).getByRole("button",{name:"Task"})).toBeTruthy();expect(kanbanMove(record,"state","done",[dynamic,pause])?.input).toBe("status");
});
