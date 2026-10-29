import type {Api} from "@platform/kernel";
import {savePreparedSignIn} from "./signin";
let registration:Promise<ServiceWorkerRegistration>|undefined;
function register(){
 if(!("serviceWorker" in navigator))throw Error("Offline preparation is unavailable in this browser.");
 return registration??=navigator.serviceWorker.register("/workspace-worker.js",{scope:"/"}).then(()=>navigator.serviceWorker.ready);
}
export async function prepareShell(){
 const entry=document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.src;
 if(!entry||!new URL(entry).pathname.startsWith("/assets/"))throw Error("Open the built workspace online before preparing it.");
 const paths=[...new Set([new URL(entry).pathname,"/rules/wasm_exec.js","/rules/engine.wasm",...performance.getEntriesByType("resource").flatMap(item=>{
  const url=new URL(item.name);return url.origin===location.origin&&url.pathname.startsWith("/assets/")&&!url.search?[url.pathname]:[];
 })])];
 const signIn=await fetch("/v1/sign-in").then(response=>{if(!response.ok)throw Error("Connect to prepare current application content.");return response.json() as Promise<Api.SignIn>;});
 const ready=await register(),worker=ready.active;
 if(!worker)throw Error("Offline preparation is unavailable in this browser.");
 await new Promise<void>((resolve,reject)=>{
  const channel=new MessageChannel(),timer=setTimeout(()=>{channel.port1.close();reject(Error("Workspace preparation timed out. Connect and retry."));},60_000);
  channel.port1.onmessage=event=>{clearTimeout(timer);channel.port1.close();event.data?.ok?resolve():reject(Error(event.data?.error??"Workspace resources could not be saved."));};
  worker.postMessage({type:"prepare-workspace-shell",entry:new URL(entry).pathname,paths},[channel.port2]);
 });
 savePreparedSignIn(signIn);
}
