import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Progress,progressRatio} from "./Progress";
afterEach(cleanup);
test("progress computes exact bounded ratios and tones without floating coercion",()=>{
 expect(progressRatio("0.1","0.3")?.percentage).toBe("33");expect(progressRatio("9007199254740993","9007199254740992")).toMatchObject({width:100,over:true});expect(progressRatio("80.000000000000000001","100")?.tone).toBe("success");expect(progressRatio("80","100")?.tone).toBe("info");expect(progressRatio("40","100")?.tone).toBe("warning");expect(progressRatio("1","8")?.percentage).toBe("13");
 for(const [value,total] of [["","100"],["1","0"],["1","-1"],["-1","100"],["1e3","100"],["1","NaN"],["9".repeat(129),"100"]])expect(progressRatio(value!,total!)).toBeUndefined();
});
test("progress retains original values, reports excess and removes old bars on invalid denominators",()=>{
 const {rerender}=render(<Progress value="120" total="100" label="Completed work"/>);expect(screen.getByRole("progressbar").getAttribute("aria-valuetext")).toBe("120 / 100 · 120%");expect(screen.getByRole("status").textContent).toContain("exceeds total");rerender(<Progress value="120" total="0" label="Completed work"/>);expect(screen.queryByRole("progressbar")).toBeNull();expect(screen.getByRole("alert")).toBeTruthy();rerender(<Progress value="0" total="100" label="Completed work"/>);expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("0");
});
