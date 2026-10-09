// Read-only steps join the one class vocabulary (ADR-0092): declared class first,
// BPMN reading second, the plain step when neither is given.
import { expect, test } from "vitest";
import { flowStepKind } from "./FlowSteps";

test("a step's class decides the entry it draws under", () => {
  expect(flowStepKind({ id: "x", class: "code" })).toBe("step:code");
  expect(flowStepKind({ id: "x", notation: "data-object" })).toBe("step:document");
  expect(flowStepKind({ id: "x", notation: "service-task" })).toBe("step:action");
  expect(flowStepKind({ id: "x", notation: "user-task" })).toBe("step:human");
});

test("a step that declares nothing stays the plain step, its kind name intact", () => {
  expect(flowStepKind({ id: "x" })).toBe("step");
  expect(flowStepKind({ id: "x", kind: "custom" })).toBe("custom");
});
