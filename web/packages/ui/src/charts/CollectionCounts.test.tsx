import {afterEach,expect,test} from "vitest";
import {cleanup,render,screen} from "@testing-library/react";
import {CollectionCounts,signedCounts} from "./CollectionCounts";
afterEach(cleanup);
const steps=[{value:"ready",label:"Active",positive:true},{value:"warn",label:"Warning",positive:false},{value:"maint",label:"Maint.",positive:false},{value:"off",label:"Offline",positive:false}];
test("signed counts retain original business mappings, order, signs and exact cumulative totals outside the loaded record window",()=>{
 const terms=[{value:"ready",count:2},{value:"warn",count:7},{value:"maint",count:1},{value:"off",count:1},{value:null,count:3},{value:"other",count:4}],m=signedCounts(terms,steps)!;
 expect(m.total).toBe(18n);expect(m.unmapped).toBe(7n);expect(m.rows.map(r=>[r.value,r.delta,r.cumulative])).toEqual([["ready",2n,2n],["warn",-7n,-5n],["maint",-1n,-6n],["off",-1n,-7n]]);expect(m.rows[1]?.share).toBe(7/18);
 render(<CollectionCounts terms={terms} steps={steps} label="Original steps"/>);expect(screen.getByText("18 matching records · 7 records outside the mapped steps")).toBeTruthy();expect(screen.getByLabelText("Warning: -7; Σ -5")).toBeTruthy();expect(screen.queryByRole("button")).toBeNull();
 const large=signedCounts([{value:"ready",count:Number.MAX_SAFE_INTEGER},{value:"warn",count:Number.MAX_SAFE_INTEGER}],steps)!;expect(large.total).toBe(18014398509481982n);expect(large.rows[1]?.cumulative).toBe(0n);expect(signedCounts([],steps)?.rows.map(r=>r.delta)).toEqual([0n,0n,0n,0n]);
});
test("horizontal counts preserve separate missing, empty and prototype-like original values and refuse lossy declarations",()=>{
 render(<CollectionCounts terms={[{value:"",count:1},{value:null,count:2},{value:"__proto__",count:3}]} label="Original bars"/>);expect(screen.getByLabelText("Empty text: 1")).toBeTruthy();expect(screen.getByLabelText("No value (missing): 2")).toBeTruthy();expect(screen.getByLabelText("__proto__: 3")).toBeTruthy();
 for(const terms of [[{value:"ready",count:1},{value:"ready",count:2}],[{value:"ready",count:1.5}],[{value:"ready",count:0}]])expect(signedCounts(terms,steps)).toBeUndefined();expect(signedCounts([],[{...steps[0]!,positive:false},...steps.slice(1)])).toBeUndefined();expect(signedCounts([],[steps[0]!,steps[0]!,steps[2]!,steps[3]!])).toBeUndefined();
});
