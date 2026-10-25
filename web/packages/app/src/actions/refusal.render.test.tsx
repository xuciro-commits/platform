import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { Entry } from "@platform/kernel";
import { PreviewWorkspace } from "@platform/ui/catalog/examples";
import type { ReactNode } from "react";
import { CatalogFixture, FixtureClient, sampleActions } from "../catalog.fixtures";
import { HostContext, useHost, type Host } from "../index";
import { ActionRefusal } from "./refusal";
import { DeclaredActionDialog } from "./actions";

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

function OutboxOwner({client,decide,children}:{client:FixtureClient;decide:Host["decide"];children:ReactNode}) {
  const host=useHost();
  return <HostContext.Provider value={{...host,client,decide,outbox:[...client.authorities.outbox]}}>{children}</HostContext.Provider>;
}

const pendingEntry=():Entry=>({state:"SUBMISSION_STATE_UNKNOWN",schemaVersion:{family:1,version:"original-version"},submission:{tenantId:"catalog-fixture",principalId:"catalog-fixture",authority:"catalog",schema:{name:sampleActions[0]!.schema},target:{type:"catalog.sample",id:"SAMPLE-001"},idempotencyKey:"review-original",expectedRevision:7,payload:btoa(JSON.stringify({note:"Original note"}))}});

test("unknown submission status opens the original outbox rather than the approval list",()=>{
  const open=vi.fn();
  render(<CatalogFixture><PreviewWorkspace onOpen={open}><ActionRefusal entry={pendingEntry()} message="Waiting for the server"/></PreviewWorkspace></CatalogFixture>);
  fireEvent.click(screen.getByRole("button",{name:"View outbox"}));
  expect(open).toHaveBeenCalledWith({view:"outbox"});
  expect(screen.queryByRole("button",{name:"View request status"})).toBeNull();
});

test.each(["SUBMISSION_STATE_REJECTED","SUBMISSION_STATE_CONFLICT"] as const)("an outbox %s releases the original form and retries the same saved intent",async(state)=>{
  const original=pendingEntry(),client=new FixtureClient({}),decide=vi.fn<Host["decide"]>(async(_schema,_target,_payload,options)=>{options?.onOutcome?.(client.authorities.outbox[0]!);return false;}),closed=vi.fn();
  client.authorities.outbox=[original];
  const tree=()=> <CatalogFixture client={client}><OutboxOwner client={client} decide={decide}><DeclaredActionDialog declared={sampleActions[0]!} type="catalog.sample" record={{id:"SAMPLE-001",revision:7}} initial={{note:"Original note"}} onClose={closed} originalOutcome={original}/></OutboxOwner></CatalogFixture>;
  const {rerender}=render(tree());
  const note=screen.getByRole("textbox",{name:"Review note *"});
  expect(note.matches(":disabled")).toBe(true);
  expect(screen.getByText("Waiting for the server response. Retry sends the same request.")).toBeTruthy();
  expect(screen.queryByText("refused",{exact:true})).toBeNull();
  const answered={...original,state,outcome:state==="SUBMISSION_STATE_REJECTED"?"ERROR_CODE_INVALID_ARGUMENT":"ERROR_CODE_CONFLICT",reason:"The original owner refused this intent."};
  client.authorities.outbox=[answered];rerender(tree());
  await screen.findByText(answered.reason);
  expect(screen.queryByText("The server response is unknown. Retry the original request or check Outbox.")).toBeNull();
  expect(note.matches(":disabled")).toBe(false);expect((note as HTMLInputElement).value).toBe("Original note");
  expect(decide).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"Retry original inputs"}));
  await waitFor(()=>expect(decide).toHaveBeenCalledTimes(1));
  expect(decide.mock.calls[0]).toMatchObject([sampleActions[0]!.schema,{type:"catalog.sample",id:"SAMPLE-001"},{note:"Original note"},{originalKey:"review-original",expectedRevision:7,schemaVersion:original.schemaVersion}]);
  expect(closed).not.toHaveBeenCalled();
});

test("a late outbox confirmation completes the original form once without creating another request",async()=>{
  const original=pendingEntry(),client=new FixtureClient({}),decide=vi.fn<Host["decide"]>(),closed=vi.fn(),completed=vi.fn();client.authorities.outbox=[original];
  const tree=()=> <CatalogFixture client={client}><OutboxOwner client={client} decide={decide}><DeclaredActionDialog declared={sampleActions[0]!} type="catalog.sample" record={{id:"SAMPLE-001",revision:7}} initial={{note:"Original note"}} onClose={closed} onCompleted={completed} originalOutcome={original}/></OutboxOwner></CatalogFixture>;
  const {rerender}=render(tree());
  client.authorities.outbox.unshift({...original,state:"SUBMISSION_STATE_CONFIRMED",submission:{...original.submission,principalId:"another-member"}});
  rerender(tree());expect(completed).not.toHaveBeenCalled();expect(screen.getByRole("textbox",{name:"Review note *"}).matches(":disabled")).toBe(true);
  await act(async()=>{await Promise.resolve();original.state="SUBMISSION_STATE_CONFIRMED";original.outcome="original-change";rerender(tree());});
  await waitFor(()=>expect(completed).toHaveBeenCalledTimes(1));expect(completed).toHaveBeenCalledWith({type:"catalog.sample",id:"SAMPLE-001"});
  rerender(tree());expect(closed).toHaveBeenCalledTimes(1);expect(decide).not.toHaveBeenCalled();
  expect(screen.queryByText("The server response is unknown. Retry the original request or check Outbox.")).toBeNull();
});
