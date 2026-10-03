import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {SummaryStatistics} from "./SummaryStatistics";
afterEach(cleanup);
test("summary displays the original five statistics and precision without computing from records",()=>{render(<SummaryStatistics fieldTitle="Availability" value={{kind:"statistics",count:"9007199254740991",min:20,mean:63.333,max:100,sum:380}}/>);expect(screen.getByText("9007199254740991")).toBeTruthy();expect(screen.getByText("63.3")).toBeTruthy();expect(screen.getAllByRole("term")).toHaveLength(5);expect(screen.getAllByRole("definition").map(d=>d.textContent)).toEqual(["9007199254740991","20","63.3","100","380"]);});
test("empty numeric measures stay absent while an original zero remains zero",()=>{const {rerender}=render(<SummaryStatistics fieldTitle="Pressure" value={{kind:"statistics",count:"0"}}/>);expect(screen.getAllByText("No value")).toHaveLength(4);rerender(<SummaryStatistics fieldTitle="Pressure" value={{kind:"statistics",count:"1",min:0,mean:0,max:0,sum:0}}/>);expect(screen.queryByText("No value")).toBeNull();expect(screen.getAllByText("0")).toHaveLength(4);});
