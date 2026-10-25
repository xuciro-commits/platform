import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import type { Entry } from "@platform/kernel";
import { CatalogFixture, FixtureClient } from "../catalog.fixtures";
import { ActionRefusal } from "./refusal";

const entry = { state: "SUBMISSION_STATE_REJECTED", outcome: "ERROR_CODE_POLICY_DENIED", submission: {
  authority: "build", principalId: "caller", schema: { name: "sample.note.edit" }, target: { type: "sample.note", id: "N1" }, idempotencyKey: "original",
} } as Entry;
class ExplainClient extends FixtureClient {
  status = 503;
  asked: unknown[] = [];
  constructor() { super({ "/v1/business-refusals?type=sample.note&id=N1&key=original&limit=1": { records: [], total: 0 } }); }
  override async call<T>(...args: unknown[]): Promise<{ ok: boolean; status: number; body: T }> {
    expect(args[1]).toBe("/v1/authz/explain");
    this.asked.push(args[2]);
    return { ok: this.status === 200, status: this.status, body: (this.status === 200
      ? { allow: false, reason: "Current action policy", rule: "original-rule", layers: [] }
      : { error: "Explanation service unavailable" }) as T };
  }
}
afterEach(cleanup);

test("an explanation service failure stays retryable and never claims the member lacks permission", async () => {
  const client = new ExplainClient();
  render(<CatalogFixture client={client}><ActionRefusal entry={entry} message="Original action refused" /></CatalogFixture>);
  fireEvent.click(screen.getByRole("button", { name: "Explain the authorization" }));
  await screen.findByText("Explanation service unavailable");
  expect(screen.queryByText("Only administrators and auditors may read the authorization explain.")).toBeNull();
  client.status = 200;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("Current action policy");
  expect(client.asked).toHaveLength(2);
  expect(client.asked[0]).toMatchObject({ action: "sample.note.edit", principal: "caller", type: "sample.note", record: "N1" });
});

test("an explicit authorization refusal is distinguished from a service failure", async () => {
  const client = new ExplainClient(); client.status = 403;
  render(<CatalogFixture client={client}><ActionRefusal entry={entry} message="Original action refused" /></CatalogFixture>);
  fireEvent.click(screen.getByRole("button", { name: "Explain the authorization" }));
  await screen.findByText("Only administrators and auditors may read the authorization explain.");
  await waitFor(() => expect(client.asked).toHaveLength(1));
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
});
