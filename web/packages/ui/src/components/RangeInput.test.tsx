import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {RangeInput,rangeGrid,rangeDrafts} from "./RangeInput";
afterEach(cleanup);
test("tick grid emits exact signed decimals, bounds cost, and refuses unrepresentable endpoints",()=>{
 const grid=rangeGrid("-0.3","0.3","0.1")!;expect(grid.ticks).toBe(6);expect(grid.text(4)).toBe("0.1");expect(grid.index("0.10")).toBe(4);expect(grid.text(3)).toBe("0");
 expect(rangeGrid("9007199254740993","9007199254740994","0.1")?.text(3)).toBe("9007199254740993.3");
 expect(rangeGrid("1"+"0".repeat(126),"1"+"0".repeat(125)+"1","0.01")).toBeUndefined();
 for(const values of [["0","1","0.3"],["0","10001","1"],["0","1","0"],["-0","1","1"],["0","1e3","1"],["1","0","1"]])expect(rangeGrid(values[0]!,values[1]!,values[2]!)).toBeUndefined();
 for(const [a,b] of [["no",""],["-0.4",""],["0.2","0.1"],["0.05","0.3"]])expect(rangeDrafts(grid,a!,b!)).toBeUndefined();
 expect(rangeDrafts(grid,"","")).toEqual({a:0,b:6});
});
test("empty bounds stay empty until explicit movement, clamps preserve the other draft, and Clear emits one pair",()=>{
 const writes:string[][]=[],props={min:"0",max:"45",step:"1",label:"Pressure",unit:"bar",onChange:(a:string,b:string)=>writes.push([a,b])};
 const {rerender}=render(<RangeInput {...props} lower="" upper=""/>);expect(writes).toEqual([]);expect(screen.getByText("Any bound – Any bound bar")).toBeTruthy();fireEvent.change(screen.getByRole("slider",{name:"Pressure Minimum"}),{target:{value:"10"}});expect(writes).toEqual([["10",""]]);
 rerender(<RangeInput {...props} lower="10" upper="20"/>);fireEvent.change(screen.getByRole("slider",{name:"Pressure Minimum"}),{target:{value:"30"}});expect(writes.at(-1)).toEqual(["20","20"]);fireEvent.change(screen.getByRole("slider",{name:"Pressure Maximum"}),{target:{value:"5"}});expect(writes.at(-1)).toEqual(["10","10"]);
 rerender(<RangeInput {...props} lower="broken" upper="20"/>);expect(screen.getByRole("slider",{name:"Pressure Minimum"}).hasAttribute("disabled")).toBe(true);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.getByText("broken – 20 bar")).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"Clear range"}));expect(writes.at(-1)).toEqual(["",""]);
});
