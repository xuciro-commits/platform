import { expect, test } from "vitest";
import { ViewTransfers } from "./shell/ViewTransfers";

test("ephemeral view payloads are bound to one callee and return once", () => {
  const calls = new ViewTransfers(), results: unknown[] = [], input = { record: { object: "sample.record", id: "A" } };
  const id = calls.start("source", input, (result) => results.push(result)); calls.bind(id, "target");
  expect(calls.read(id,"other")).toBeUndefined(); expect(calls.read(id,"target")?.input).toBe(input);
  expect(calls.finish(id,"other",true)).toBeUndefined();expect(results).toEqual([]);
  expect(calls.finish(id,"target",true)).toBe("source");expect(results).toEqual([true]);
  expect(calls.finish(id,"target",false)).toBeUndefined();expect(new ViewTransfers().read(id,"target")).toBeUndefined();
});
test("closing a caller or callee clears payloads and callbacks", () => {
  for (const panel of ["source","target"]) {
    const calls=new ViewTransfers();let returned=false;
    const id=calls.start("source",{},()=>{returned=true;});calls.bind(id,"target");calls.remove(panel);
    expect(calls.read(id,"target")).toBeUndefined();calls.finish(id,"target",true);expect(returned).toBe(false);
  }
});
