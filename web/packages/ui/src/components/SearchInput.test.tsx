import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {SearchInput} from "./SearchInput";
afterEach(cleanup);
test("search text remains caller-owned and literal with declared scope, empty clearing and disabled protection",()=>{
 const raw="%_\\";const change=vi.fn(),props={"aria-label":"Assets",scope:["Assets","Orders"],onChange:change};const {rerender}=render(<SearchInput {...props} value={raw}/>);const input=screen.getByRole("searchbox",{name:"Assets"}) as HTMLInputElement;expect(input.value).toBe(raw);expect(screen.getByText("Search in: Assets, Orders")).toBeTruthy();fireEvent.change(input,{target:{value:""}});expect(change).toHaveBeenCalledWith("");rerender(<SearchInput {...props} value="original" disabled/>);fireEvent.change(input,{target:{value:"other"}});expect(change).toHaveBeenCalledTimes(1);rerender(<SearchInput {...props} value="" scope={[]}/>);expect(screen.queryByRole("searchbox")).toBeNull();expect(screen.getByRole("status").textContent).toContain("unavailable");
});
