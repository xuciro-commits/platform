import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, focusManager, onlineManager } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const state=vi.hoisted(()=>({host:undefined,files:undefined,reads:undefined,error:undefined,refetch:vi.fn()}));
vi.mock("@platform/app",()=>({useHost:()=>state.host,useReadQuery:path=>({data:path.startsWith("/v1/system-files?")?Object.values(state.files):state.reads[path],error:state.error,isLoading:false,refetch:state.refetch})}));
import { setLanguage } from "@platform/ui";
import "./i18n.ts";
import { SystemConsole } from "./system.tsx";

let root,container,queries;
const click=async text=>{let button;await waitFor(()=>{button=[...document.querySelectorAll("button")].find(button=>button.textContent===text);expect(button).toBeDefined();});await act(async()=>button.click());};
const waitFor=async assertion=>{let error;for(let step=0;step<100;step++){await act(async()=>{await vi.runOnlyPendingTimersAsync();});try{assertion();return;}catch(problem){error=problem;}}throw error;};
const render=()=>act(()=>root.render(createElement(QueryClientProvider,{client:queries},createElement(SystemConsole))));
beforeEach(()=>{
 vi.useFakeTimers();localStorage.clear();history.replaceState(null,"","/#/self-hosting");setLanguage("en");
 vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT",true);
 vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});
 vi.spyOn(HTMLElement.prototype,"offsetHeight","get").mockReturnValue(480);vi.spyOn(HTMLElement.prototype,"offsetWidth","get").mockReturnValue(1200);
 vi.stubGlobal("matchMedia",query=>({matches:false,media:query,addEventListener(){},removeEventListener(){}}));
 state.files={"a.ts":{path:"a.ts",content:"retained source",revision:4,digest:"source-digest",size:15,editable:true}};
 state.reads={"/v1/system-changes":[{id:"original-change",title:"Retained change",state:"draft",rationale:"Original reason",release:2,files:[{path:"a.ts",digest:"frozen-digest",content:"frozen source",before:{digest:"prior-digest",content:"prior source"}}]}],"/v1/system-releases":[{id:"2",version:2,state:"published",change:"original-change"}],"/v1/system-activations":[{id:"original-activation",release:2,result:"requested",at:"2026-10-27T10:00:00Z",window:600}]};
 state.error=undefined;state.refetch.mockClear();
 state.host={catalog:["system.file.save","system.change.propose","system.change.approve","system.release.activate","system.release.cancel","agent.run.start"].map(schema=>({schema,target:"system.file"})),source:{scope:"test"},client:{connection:{token:"manager",tenant:"system"},authorities:{outbox:[]},enableLiveReads:()=>()=>{},get:vi.fn(async path=>state.files[new URL(path,"http://localhost").searchParams.get("path")]),send:vi.fn()},refresh:vi.fn(),decide:vi.fn()};
 queries=new QueryClient({defaultOptions:{queries:{retry:false}}});container=document.createElement("div");document.body.append(container);root=createRoot(container);
 render();
});
afterEach(async()=>{await act(async()=>{root?.unmount();queries?.clear();focusManager.setFocused(undefined);onlineManager.setOnline(true);await vi.runOnlyPendingTimersAsync();});document.body.replaceChildren();vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals();});

test("even an old writable catalog cannot expose source, proposal or activation controls",async()=>{
 await waitFor(()=>expect(container.textContent).toContain("System source editing, proposals, approvals, builds and new activations are frozen."));
 for(const label of ["Save file","Open or create file","Propose selected changes","Approve","Activate","Cancel activation","Run the architect"]){expect([...container.querySelectorAll("button")].some(button=>button.textContent===label)).toBe(false);}
 expect(container.querySelector("textarea")).toBeNull();
 expect(container.querySelector('a[href="#/control-panel"]')).not.toBeNull();expect(container.querySelector('a[href="#/host-overview"]')).not.toBeNull();
 expect(state.host.decide).not.toHaveBeenCalled();
});

test("reviewing original frozen files and source retains their content without creating work",async()=>{
 await click("Review");await act(async()=>[...container.querySelectorAll("button")].find(summary=>summary.textContent.startsWith("a.ts")).click());expect(container.textContent).toContain("frozen source");expect(container.textContent).toContain("prior source");expect(container.textContent).toContain("original-change");
 await click("Open");await waitFor(()=>expect(container.textContent).toContain("retained source"));
 expect(container.textContent).toContain("source-digest");expect(container.querySelector("textarea")).toBeNull();
 expect(state.host.client.get).toHaveBeenCalledWith("/v1/system-file?path=a.ts",true);
 expect(state.host.decide).not.toHaveBeenCalled();expect(state.host.client.send).not.toHaveBeenCalled();
});

test("missing retained source stays an error instead of becoming a new editable file",async()=>{
 state.host.client.get.mockRejectedValue(Error("HTTP 404: retained file unavailable"));
 await click("Open");await waitFor(()=>expect([...container.querySelectorAll('[role="alert"]')].map(alert=>alert.textContent).join(" ")).toContain("HTTP 404"));
 expect(container.querySelector("textarea")).toBeNull();expect(state.host.decide).not.toHaveBeenCalled();
});

test("checking an unknown activation sends only its original key and does not mint a new decision",async()=>{
 const entry={state:"SUBMISSION_STATE_UNKNOWN",submission:{idempotencyKey:"original-request",schema:{name:"system.release.activate"},target:{id:"2"},values:{window:600}}};
 state.host.client.authorities.outbox.push(entry);state.host.client.send.mockImplementation(async key=>{expect(key).toBe("original-request");entry.state="SUBMISSION_STATE_CONFIRMED";});
 render();await click("Check the original request");
 expect(state.host.client.send).toHaveBeenCalledExactlyOnceWith("original-request");expect(state.host.client.authorities.outbox).toEqual([entry]);
 expect(state.host.decide).not.toHaveBeenCalled();expect(state.refetch).toHaveBeenCalled();
});

test("freeze explanation is bilingual while retained source content remains unchanged",async()=>{
 await act(async()=>setLanguage("zh-CN"));render();
 expect(container.textContent).toContain("系统历史");expect(container.textContent).toContain("系统源码编辑、提案、批准、构建和新激活已冻结");
 expect(container.textContent).toContain("Retained change");
});
