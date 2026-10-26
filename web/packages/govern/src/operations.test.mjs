import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({ reads: {} }));
vi.mock("@platform/app", () => ({
  useHost: () => ({ decide: vi.fn() }),
  useReadQuery: path => state.reads[path],
  DeclaredActionForm: () => null,
}));

import { Automation } from "./operations.tsx";
let root;
afterEach(() => { act(() => root?.unmount()); document.body.replaceChildren(); vi.unstubAllGlobals(); });

test("subscription discovery distinguishes loading and failure from an empty successful answer and retries its own read", () => {
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div"); document.body.append(container); root = createRoot(container);
  const retry = vi.fn();
  state.reads["/v1/work"] = { data: [], isPending: false, isError: false };
  state.reads["/v1/apps"] = { isPending: true, isError: false, refetch: retry };
  act(() => root.render(createElement(Automation)));
  expect(container.querySelector('[role="status"]')?.textContent).toBe("Loading…");
  expect(container.textContent).not.toContain("No subscriptions.");

  state.reads["/v1/apps"] = { isPending: false, isError: true, error: Error("Original subscriptions read failed"), refetch: retry };
  act(() => root.render(createElement(Automation)));
  expect(container.querySelector('[role="alert"]')?.textContent).toContain("Original subscriptions read failed");
  expect(container.textContent).not.toContain("No subscriptions.");
  act(() => [...container.querySelectorAll("button")].find(button => button.textContent === "Retry").click());
  expect(retry).toHaveBeenCalledOnce();

  state.reads["/v1/apps"] = { data: [], isPending: false, isError: false, refetch: retry };
  act(() => root.render(createElement(Automation)));
  expect(container.textContent).toContain("No subscriptions.");
  expect(container.querySelector('[role="alert"]')).toBeNull();
});
