import {expect,test} from "vitest";
import {neighborhoodPositions} from "./neighborhood";
test("radial presentation keeps stable original keys, assigns each original node once and separates right and left neighborhoods",()=>{
 const root=JSON.stringify(["asset","same"]),sensor=JSON.stringify(["sensor","same"]),alert=JSON.stringify(["alert","same"]),positions=neighborhoodPositions(root,[{side:"right",nodes:[sensor,"S2"]},{side:"left",nodes:[alert,"A2"]}]);expect(Object.keys(positions)).toHaveLength(5);expect(positions.S2!.x).toBeGreaterThan(positions.A2!.x);expect(positions[sensor]!.y).toBeLessThan(positions[alert]!.y);expect(Object.getPrototypeOf(positions)).toBeNull();const duplicate=neighborhoodPositions("__proto__",[{side:"right",nodes:["constructor","constructor"]}]);expect(Object.hasOwn(duplicate,"__proto__")).toBe(true);expect(Object.hasOwn(duplicate,"constructor")).toBe(true);expect(Object.keys(duplicate)).toHaveLength(2);
});
