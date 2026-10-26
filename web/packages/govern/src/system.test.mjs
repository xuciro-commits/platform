import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, focusManager, onlineManager } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const state=vi.hoisted(()=>({host:undefined,files:undefined}));
vi.mock("@platform/app",()=>({useHost:()=>state.host,useReadQuery:path=>({data:path.startsWith("/v1/system-files?")?Object.values(state.files):[],isLoading:false,refetch:vi.fn()})}));
import { Workspace, setLanguage } from "@platform/ui";
import { SystemConsole } from "./system.tsx";

let root,container,queries;
const click=async text=>{const button=[...document.querySelectorAll("button")].find(button=>button.textContent===text);expect(button).toBeDefined();await act(async()=>button.click());};
const waitFor=async assertion=>{let error;for(let step=0;step<100;step++){await act(async()=>{await vi.runOnlyPendingTimersAsync();});try{assertion();return;}catch(problem){error=problem;}}throw error;};
const content=()=>container.querySelector("textarea[rows='16']");
const expectContent=value=>waitFor(()=>expect(content()?.value).toBe(value));
const input=async value=>{await act(async()=>{const editor=content();Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,"value").set.call(editor,value);editor.dispatchEvent(new Event("input",{bubbles:true}));});};
const open=async path=>{const input=[...container.querySelectorAll("label")].find(label=>label.textContent==="Path").querySelector("input");await act(async()=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(input,path);input.dispatchEvent(new Event("input",{bubbles:true}));});await click("Open or create file");};
const start=()=>waitFor(()=>expect([...container.querySelectorAll("label")].some(label=>label.textContent==="Path")).toBe(true));
beforeEach(()=>{
 vi.useFakeTimers();localStorage.clear();history.replaceState(null,"","/#/self-hosting");setLanguage("en");
 vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT",true);
 vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});
 vi.stubGlobal("matchMedia",query=>({matches:false,media:query,addEventListener(){},removeEventListener(){}}));
 state.files=Object.fromEntries(["a.ts","b.ts"].map(path=>[path,{path,content:`saved ${path}`,revision:1,digest:path,size:10,editable:true}]));
 state.host={catalog:[{schema:"system.file.save",target:"system.file"}],source:{scope:"test"},client:{connection:{token:"manager",tenant:"system"},authorities:{outbox:[]},enableLiveReads:()=>()=>{},get:vi.fn(async path=>state.files[new URL(path,"http://localhost").searchParams.get("path")])},refresh:vi.fn(),decide:vi.fn()};
 queries=new QueryClient({defaultOptions:{queries:{retry:false}}});container=document.createElement("div");document.body.append(container);root=createRoot(container);
 act(()=>root.render(createElement(QueryClientProvider,{client:queries},createElement(Workspace,{product:"System",storageKey:"system:test",views:[{id:"self-hosting",title:()=>"Self-hosting",render:()=>createElement(SystemConsole)}],home:{view:"self-hosting"},nav:[]}))));
});
afterEach(async()=>{await act(async()=>{root?.unmount();queries?.clear();focusManager.setFocused(undefined);onlineManager.setOnline(true);await vi.runOnlyPendingTimersAsync();});document.body.replaceChildren();vi.useRealTimers();vi.unstubAllGlobals();});

test("source switching preserves edits until the original workspace discard is confirmed",async()=>{
 await start();await open("a.ts");await expectContent("saved a.ts");await input("unsaved source");expect(content().value).toBe("unsaved source");
 await open("b.ts");expect(document.querySelector('[role="dialog"]')?.textContent).toContain("Unsaved changes");
 await click("Keep editing");expect(content().value).toBe("unsaved source");
 await open("b.ts");await click("Discard changes");await expectContent("saved b.ts");
 await open("a.ts");await expectContent("saved a.ts");
});

test("focus, reconnection and host refresh preserve edits until the original reload is confirmed",async()=>{
 await start();await open("a.ts");await expectContent("saved a.ts");await input("keep source");state.files["a.ts"]={...state.files["a.ts"],content:"new saved source",revision:2,digest:"new"};
 await act(async()=>{focusManager.setFocused(false);focusManager.setFocused(true);onlineManager.setOnline(false);onlineManager.setOnline(true);await queries.invalidateQueries();await vi.runOnlyPendingTimersAsync();});
 expect(content().value).toBe("keep source");await click("Reload file and discard edits");await click("Keep editing");expect(content().value).toBe("keep source");
 await click("Reload file and discard edits");await click("Discard changes");await expectContent("new saved source");
});

test("an unanswered source save blocks switching without losing its input or request",async()=>{
 await start();await open("a.ts");await expectContent("saved a.ts");await input("pending source");
 const entry={state:"SUBMISSION_STATE_UNKNOWN",submission:{idempotencyKey:"original",schema:{name:"system.file.save"},target:{id:"a.ts"}}};
 state.host.decide.mockImplementation(async(_schema,_target,_values,options)=>{state.host.client.authorities.outbox.push(entry);options.onOutcome(entry);return false;});
 await click("Save file");await waitFor(()=>expect(container.textContent).toContain("Unconfirmed requests"));await open("b.ts");
 expect(content().value).toBe("pending source");expect(document.querySelector('[role="dialog"]')).toBeNull();
 expect(state.host.client.authorities.outbox).toEqual([entry]);expect(state.host.decide).toHaveBeenCalledOnce();
});
