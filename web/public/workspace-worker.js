const cacheName="platform-workspace-shell-v1";
const staticPath=path=>path.startsWith("/assets/")||path==="/rules/wasm_exec.js"||path==="/rules/engine.wasm";
self.addEventListener("install",()=>self.skipWaiting());
self.addEventListener("activate",event=>event.waitUntil(self.clients.claim()));
self.addEventListener("message",event=>{
 if(event.data?.type!=="prepare-workspace-shell"||!event.ports[0])return;
 const port=event.ports[0];
 event.waitUntil((async()=>{
  try{
   const paths=event.data.paths,entry=event.data.entry;
   if(!Array.isArray(paths)||paths.length>200||typeof entry!=="string"||!entry.startsWith("/assets/")||!paths.includes(entry)||paths.some(path=>typeof path!=="string"||!staticPath(path)||path.includes("..")||path.includes("?")||path.includes("#")))throw Error("Open the built workspace online before preparing it.");
   const root=await fetch("/",{cache:"reload"});
   if(!root.ok||!(await root.clone().text()).includes(entry))throw Error("The workspace software changed. Reload before preparing it.");
   const responses=[];let bytes=0;
   for(const path of [...new Set(paths)]){
    const response=await fetch(path,{cache:"reload"});
    if(!response.ok)throw Error("A workspace resource could not be prepared. Connect and retry.");
    bytes+=(await response.clone().arrayBuffer()).byteLength;
    if(bytes>32*1024*1024)throw Error("Workspace preparation exceeds its 32 MiB resource limit.");
    responses.push([path,response]);
   }
   const cache=await caches.open(cacheName);
   for(const [path,response] of responses)await cache.put(path,response);
   await cache.put("/",root);
   const retained=new Set(["/",...paths]);
   for(const request of await cache.keys())if(!retained.has(new URL(request.url).pathname))await cache.delete(request);
   port.postMessage({ok:true});
  }catch(error){port.postMessage({ok:false,error:error instanceof Error?error.message:"Workspace resources could not be saved."});}
 })());
});
self.addEventListener("fetch",event=>{
 const request=event.request,url=new URL(request.url);
 if(request.method!=="GET"||url.origin!==self.location.origin||!((request.mode==="navigate"&&(url.pathname==="/"||url.pathname==="/index.html"))||staticPath(url.pathname)))return;
 event.respondWith(fetch(request).catch(async()=>{
  const cached=await(await caches.open(cacheName)).match(request.mode==="navigate"?"/":url.pathname);
  if(cached)return cached;
  return new Response("This workspace task was not prepared. Connect to continue.",{status:503,headers:{"Content-Type":"text/plain; charset=utf-8"}});
 }));
});
