import {afterEach,expect,test} from "vitest";
import {cleanup,render,screen} from "@testing-library/react";
import {DerivedMean} from "./DerivedMean";
afterEach(cleanup);
test("original count and mean remain separate; no numeric answer is not rendered as a fabricated zero",()=>{const view=render(<DerivedMean count="9007199254740993" mean={2.375} field="pressure" unit="bar"/>);expect(screen.getByText("Original source set: 9007199254740993 records")).toBeTruthy();expect(screen.getByText("2.38 bar")).toBeTruthy();expect(screen.getByText("mean(pressure)")).toBeTruthy();view.rerender(<DerivedMean count="3" field="pressure" unit=""/>);expect(screen.getByText("No numeric values")).toBeTruthy();expect(screen.queryByText("0.00")).toBeNull();view.rerender(<DerivedMean count="2" mean={NaN} field="pressure" unit="bar"/>);expect(screen.getByRole("alert")).toBeTruthy();});
