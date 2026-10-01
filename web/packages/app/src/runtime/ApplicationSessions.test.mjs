import assert from "node:assert/strict";
import test from "node:test";
import { ApplicationSessionHub } from "./ApplicationSessions.ts";

test("application state follows one identity and instance, and retired handles cannot write a reopened instance", () => {
  const variables = { condition: { scope: "application", type: "boolean", mode: "state", initial: false }, label: { scope: "application", type: "string", mode: "constant", initial: "read only" } };
  const hub = new ApplicationSessionHub(), first = hub.get("member:app:v1:one", variables), second = hub.get("member:app:v1:two", variables);
  const a = Symbol(), b = Symbol(); let closed = 0;
  first.attach(a, () => closed++); first.attach(b, () => closed++); second.attach(Symbol(), () => {});
  first.set("condition", true);
  assert.equal(hub.get("member:app:v1:one", variables), first);
  assert.deepEqual(second.snapshot(), {});
  assert.deepEqual(hub.get("member:app:v2:one", variables).snapshot(), {});
  assert.deepEqual(hub.get("other:app:v1:one", variables).snapshot(), {});
  first.set("condition", "wrong type"); first.set("label", "write constant");
  assert.deepEqual(first.snapshot(), { condition: true });
  first.detach(a); assert.equal(first.snapshot().condition, true);
  first.close(); assert.equal(closed, 1); assert.deepEqual(first.snapshot(), {});
  const reopened = hub.get("member:app:v1:one", variables); reopened.attach(Symbol(), () => {});
  first.set("condition", true); assert.deepEqual(reopened.snapshot(), {});
  second.detach(Symbol()); // Removing an unrelated owner does not retire a live instance.
  assert.equal(second.retired, false);
});
