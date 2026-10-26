import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { Authorities, KernelError, type State } from "./outbox";

// The contract's K5 vectors, shared with the Go, Swift and Rust implementations.
const file = JSON.parse(readFileSync(new URL("../../../../contract/vectors/k5-authority.json", import.meta.url), "utf8"));

for (const vector of file.vectors) test(`K5 ${vector.id}`, () => {
  const a = new Authorities(vector.given.edge);
  for (const d of vector.given.declarations) a.declare(d);
  vector.steps.forEach((step: any, i: number) => {
    let got: object;
    try {
      let state: State | undefined;
      if (step.declare) a.declare(step.declare);
      else if (step.authorize) a.authorize(step.authorize);
      else if (step.enqueue) state = a.enqueue(step.enqueue);
      else if (step.answer) state = a.answer(step.answer.tenantId, step.answer.idempotencyKey, step.answer.code);
      else state = a.transition(step.transition.tenantId, step.transition.idempotencyKey, step.transition.event);
      got = state ? { state } : { ok: true };
    } catch (e) {
      got = { error: (e as KernelError).code };
    }
    expect(got, `${vector.id} step ${i}`).toEqual(step.expect);
  });
});
