import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { webcrypto } from "node:crypto";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({ host: undefined, query: undefined, open: vi.fn(), read: vi.fn() }));
vi.mock("@platform/app", () => ({ useHost: () => state.host, useReadQuery: (...args) => { state.read(...args); return state.query; } }));
vi.mock("@platform/ui", async load => ({ ...await load(), useWorkspace: () => ({ open: state.open }) }));
import { setLanguage } from "@platform/ui";
import "./i18n";
import { PackagePreparation, PackageRequirements } from "./package-preparation.tsx";

let root, container;
const button = text => [...container.querySelectorAll("button")].find(button => button.textContent === text);
const render = (Component = PackagePreparation) => act(() => root.render(createElement(Component, { id: "sample", version: "1.0.0" })));
const click = async text => { const target = button(text); expect(target).toBeDefined(); expect(target.disabled).toBe(false); await act(async () => target.click()); };
const reviewSetup = async () => { await act(async () => container.querySelector('input[type="checkbox"]').click()); };
beforeEach(() => {
  localStorage.clear(); setLanguage("en"); state.open.mockReset(); state.read.mockReset();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true); vi.stubGlobal("crypto", webcrypto);
  const outbox = [];
  state.host = { me: { principalId: "builder" }, role: () => "builder", can: () => true, decide: vi.fn().mockResolvedValue(true),
    outbox, catalog: [{ schema: "build.entry.create", schemaVersion: { family: 1, version: "1" } }],
    client: { connection: { server: "", tenant: "tenant", principal: "builder" }, authorities: { outbox }, call: vi.fn(), draft: vi.fn(), refreshDeclarations: vi.fn().mockResolvedValue(undefined) } };
  state.query = { isError: false, isLoading: false, refetch: vi.fn().mockResolvedValue({}), data: { package: "sample", version: "1.0.0", installed: true,
    applications: [{ ref: { app: "build", kind: "app", name: "desk" }, title: "Sample desk", draftId: "draft", published: false, state: "draft", pages: ["entry"] }], connections: [], ai: [], roles: [{ app: "build.sample", role: "operator", available: false, held: false }] } };
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(() => { act(() => root.unmount()); document.body.replaceChildren(); vi.unstubAllGlobals(); });

test("package preparation keeps publication before business role assignment and never grants an unavailable role", async () => {
  render();
  for (const title of ["Install and create drafts", "Publish applications", "Configure services when needed", "Assign business access", "Initial data and application access"]) expect(container.textContent).toContain(title);
  expect(container.textContent).toContain("Publish the application before assigning this role.");
  expect(button("Grant this role to me")).toBeUndefined(); expect(button("Assign business roles")).toBeUndefined();
  expect(state.host.decide).not.toHaveBeenCalled();
  await click("Publish application");
  expect(state.open).toHaveBeenCalledWith({ view: "release-review", params: { kind: "app", id: "draft", application: "draft", applicationName: "desk" } });
  state.query.data.roles[0].available = true; render();
  expect(state.host.decide).not.toHaveBeenCalled();
  await click("Grant this role to me");
  expect(state.host.decide).toHaveBeenCalledWith("platform.member.grant", { type: "platform.member", id: "builder" }, { app: "build.sample", role: "operator" }, expect.any(Object));
});

test("unconfigured services do not block publication or require sample initialization for application access", async () => {
  state.query.data.connections = [{ ref: { app: "build", kind: "connection", name: "external" }, title: "External service", draftId: "connection", kind: "http", credential: true, configured: false, ready: false }];
  state.query.data.ai = [{ ref: { app: "build", kind: "function", name: "summary" }, ready: false, problem: "No model configured" }];
  state.query.data.setup = { roles: [{ app: "build.sample", role: "operator" }], steps: [{ name: "Sample record", app: "build", schema: "build.entry.create", target: { type: "build.entry", id: "sample" }, payload: "{}" }] };
  render();
  expect(container.textContent).toContain("Needs configuration"); expect(container.textContent).toContain("No model configured");
  await click("Publish application");
  expect(state.open).toHaveBeenCalledWith({ view: "release-review", params: { kind: "app", id: "draft", application: "draft", applicationName: "desk" } });
  state.query.data.applications[0].published = true; render();
  expect(container.textContent).toContain("Optional initial data");
  expect(button("Create initial data").disabled).toBe(true);
  await click("Open application");
  expect(state.open).toHaveBeenLastCalledWith({ view: "page", params: { app: "build", kind: "page", name: "entry", application: "build:desk" } });
  expect(state.host.decide).not.toHaveBeenCalled(); expect(state.host.client.call).not.toHaveBeenCalled();
});

test("preparation reads expose their actual error and retry the original read", async () => {
  state.query = { isError: true, isLoading: false, error: Error("package version is unavailable"), refetch: vi.fn() };
  render(); expect(container.querySelector('[role="alert"]').textContent).toContain("package version is unavailable");
  await click("Retry"); expect(state.query.refetch).toHaveBeenCalledOnce();
  render(PackageRequirements); expect(container.querySelector('[role="alert"]').textContent).toContain("package version is unavailable");
  await click("Retry"); expect(state.query.refetch).toHaveBeenCalledTimes(2);
});

test("a builder whose primary role changes after a business grant keeps the authorized preparation task", () => {
  state.host.role = () => "accountant";
  state.host.me.profile = { roles: { build: "accountant" }, grants: [{ app: "build", role: "accountant" }, { app: "build", role: "builder" }] };
  render();
  expect(state.read).toHaveBeenLastCalledWith("/v1/packages/sample/preparation?version=1.0.0", undefined, true);
  expect(button("Publish application")).toBeDefined();
  expect(container.textContent).toContain("Assign business access");
  render(PackageRequirements);
  expect(state.read).toHaveBeenLastCalledWith("/v1/packages/sample/preparation?version=1.0.0", undefined, true);
  expect(container.textContent).toContain("Applications: Sample desk");
  expect(state.host.decide).not.toHaveBeenCalled();
});

test("creating application drafts preserves the request key after a concrete server refusal", async () => {
  state.query.data.applications[0].draftId = undefined;
  state.host.client.call.mockResolvedValueOnce({ ok: false, body: { error: { message: "app field points to a missing page" } } }).mockResolvedValueOnce({ ok: true, body: {} });
  render(); await click("Create application drafts");
  expect(container.querySelector('[role="alert"]').textContent).toBe("app field points to a missing page");
  const original = state.host.client.call.mock.calls[0][2];
  await click("Create application drafts");
  expect(state.host.client.call.mock.calls[1]).toEqual(["POST", "/v1/packages/sample/drafts", original]);
  expect(container.querySelector('[role="alert"]')).toBeNull();
  expect(localStorage.getItem("package-task::tenant:builder:sample@1.0.0:create")).toBeNull();
});

test("initialization resumes the original unanswered step after returning and skips confirmed requests", async () => {
  state.query.data.applications[0].published = true;
  state.query.data.roles = [{ app: "build.sample", role: "operator", available: true, held: true }];
  state.query.data.setup = { roles: [{ app: "build.sample", role: "operator" }], steps: ["first", "second"].map(name => ({ name, app: "build", schema: "build.entry.create", target: { type: "build.entry", id: name }, payload: JSON.stringify({ note: name }) })) };
  const outbox = state.host.outbox;
  state.host.client.draft.mockImplementation((schema, target, payload, _evidence, _revision, _version, _correction, key) => outbox.push({ state: "SUBMISSION_STATE_PENDING", submission: { tenantId: "tenant", principalId: "builder", schema: { name: schema }, target, payload, idempotencyKey: key } }));
  let interrupted = false;
  state.host.decide.mockImplementation(async (_schema, target, _payload, options) => {
    const entry = outbox.find(entry => entry.submission.idempotencyKey === options.originalKey);
    if (target.id === "second" && !interrupted) { interrupted = true; entry.state = "SUBMISSION_STATE_UNKNOWN"; entry.reason = "Receipt unavailable"; options.onRefused(entry.reason); return false; }
    entry.state = "SUBMISSION_STATE_CONFIRMED"; return true;
  });
  render(); expect(state.host.decide).not.toHaveBeenCalled(); expect(button("Create initial data").disabled).toBe(true);
  await reviewSetup(); await click("Create initial data");
  expect(container.textContent).toContain("Initialization step second: Receipt unavailable"); expect(container.textContent).toContain("Awaiting confirmation");
  const keys = outbox.map(entry => entry.submission.idempotencyKey); expect(keys).toHaveLength(2);
  act(() => root.unmount()); root = createRoot(container); render();
  await reviewSetup(); await click("Continue initialization");
  expect(container.textContent).toContain("Initial data is ready.");
  expect(state.host.client.draft).toHaveBeenCalledTimes(2);
  expect(state.host.decide.mock.calls.map(call => call[3].originalKey)).toEqual([keys[0], keys[1], keys[1]]);
  expect(outbox.map(entry => entry.submission.payload)).toEqual([{ note: "first" }, { note: "second" }]);
});
