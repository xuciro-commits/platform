import type {Action,AssetBinding,SchemaVersion} from "./gen/host";
import type {Connection} from "./client";

export type PreparedApplication={id:string;title:string;binding:AssetBinding;release:string;at:string;route:string;reads:Record<string,unknown>};
export type LocalFormDraft={key:string;application:string;binding:AssetBinding;schema:string;schemaVersion?:SchemaVersion;target:{type:string;id:string};expectedRevision?:number;values:Record<string,unknown>;at:string};
type Scope={server:string;tenant:string;principal:string};
type State={version:1;scope:Scope;active?:string;applications:PreparedApplication[];forms:LocalFormDraft[]};
const maxBytes=2*1024*1024;
export const preparedMetadataPaths=["/v1/me","/v1/declarations","/v1/apps","/v1/actions","/v1/entities","/v1/definitions","/v1/protocols","/v1/views","/v1/releases/active"] as const;
const metadata=new Set<string>(preparedMetadataPaths);
const object=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==="object"&&!Array.isArray(value);
const serverOf=(connection:Connection)=>{const server=connection.server||globalThis.location?.origin;if(!server)throw Error("The workspace host is not ready.");return server.replace(/\/$/,"");};
const scopeOf=(connection:Connection):Scope=>({server:serverOf(connection),tenant:connection.tenant,principal:connection.principal});
const sameScope=(a:Scope,b:Scope)=>a.server===b.server&&a.tenant===b.tenant&&a.principal===b.principal;
const keyOf=(scope:Scope)=>`prepared:${JSON.stringify([scope.server,scope.tenant,scope.principal])}`;
const binding=(value:unknown):value is AssetBinding=>object(value)&&object(value.ref)&&value.ref.kind==="app"&&typeof value.ref.app==="string"&&typeof value.ref.name==="string"&&typeof value.sourceVersion==="string"&&!!value.sourceVersion;
const sameBinding=(a:AssetBinding,b:AssetBinding)=>a.ref.app===b.ref.app&&a.ref.kind===b.ref.kind&&a.ref.name===b.ref.name&&a.sourceVersion===b.sourceVersion;
const time=(value:unknown)=>typeof value==="string"&&Number.isFinite(Date.parse(value));

export class PreparedWorkspace {
 readonly scope:Scope;
 private active?:string;
 private raw?:string;
 private parsed?:State;
 constructor(connection:Connection){
  this.scope=scopeOf(connection);
  if(!this.scope.tenant||!this.scope.principal)throw Error("The workspace identity is not ready.");
 }
 private load():State{
  const raw=localStorage.getItem(keyOf(this.scope));
  if(raw===null)return {version:1,scope:this.scope,applications:[],forms:[]};
  if(this.raw===raw&&this.parsed)return this.parsed;
  const state=this.decode(raw);this.raw=raw;this.parsed=state;return state;
 }
 private decode(raw:string):State{
  try{
   const value:unknown=JSON.parse(raw);
   if(new TextEncoder().encode(raw).length>maxBytes||!object(value)||value.version!==1||!object(value.scope)||!sameScope(value.scope as Scope,this.scope)||!Array.isArray(value.applications)||value.applications.length>8||!Array.isArray(value.forms)||value.forms.length>32)throw Error();
   const ids=new Set<string>(),forms=new Set<string>();
   for(const app of value.applications){
    if(!object(app)||typeof app.id!=="string"||!app.id||ids.has(app.id)||typeof app.title!=="string"||!binding(app.binding)||typeof app.release!=="string"||!app.release||!time(app.at)||typeof app.route!=="string"||!object(app.reads)||Object.keys(app.reads).length>128)throw Error();
    const me=app.reads["/v1/me"];
    if(!object(me)||me.tenantId!==this.scope.tenant||me.principalId!==this.scope.principal)throw Error();
    for(const path of Object.keys(app.reads))if(!readPath(path))throw Error();
    ids.add(app.id);
   }
   for(const draft of value.forms){
    if(!object(draft)||typeof draft.key!=="string"||!draft.key||forms.has(draft.key)||typeof draft.application!=="string"||!binding(draft.binding)||typeof draft.schema!=="string"||!object(draft.target)||typeof draft.target.type!=="string"||typeof draft.target.id!=="string"||!draft.target.id||!object(draft.values)||!time(draft.at))throw Error();
    if(draft.schemaVersion!==undefined&&(!object(draft.schemaVersion)||!Number.isInteger(draft.schemaVersion.family)||typeof draft.schemaVersion.version!=="string"||!draft.schemaVersion.version))throw Error();
    if(draft.expectedRevision!==undefined&&(!Number.isInteger(draft.expectedRevision)||Number(draft.expectedRevision)<0))throw Error();
    forms.add(draft.key);
   }
   if(value.active!==undefined&&(typeof value.active!=="string"||!ids.has(value.active)))throw Error();
   return value as State;
  }catch{throw Error("Prepared content could not be opened. Local storage was not replaced.");}
 }
 private save(next:State){
  const raw=JSON.stringify(next);
  if(new TextEncoder().encode(raw).length>maxBytes)throw Error("Prepared content exceeds 2 MiB. Remove a preparation or saved draft first.");
  const state=this.decode(raw);
  localStorage.setItem(keyOf(this.scope),raw);this.raw=raw;this.parsed=state;
 }
 applications(){return this.load().applications.map(({reads:_,...application})=>structuredClone(application));}
 activate(application?:string){this.active=application;}
 current(){const state=this.load(),application=state.applications.find(app=>app.id===(this.active??state.active));if(!application)return;const {reads:_,...summary}=application;return structuredClone(summary);}
 read<T>(path:string):T{
  const state=this.load(),app=state.applications.find(app=>app.id===(this.active??state.active));
  const context=app??(metadata.has(path)?state.applications.find(app=>app.id===state.active):undefined);
  if(!context||!Object.hasOwn(context.reads,path))throw Error("This task was not prepared for offline use. Connect to continue.");
  return structuredClone(context.reads[path]) as T;
 }
 prepare(application:PreparedApplication){
  if(!application.id||!binding(application.binding)||!application.release||!time(application.at)||Object.keys(application.reads).length>128||Object.keys(application.reads).some(path=>!readPath(path)))throw Error("Choose a bounded published application and its readable task context.");
  const me=application.reads["/v1/me"];
  if(!object(me)||me.tenantId!==this.scope.tenant||me.principalId!==this.scope.principal)throw Error("Prepared content belongs to another workspace identity.");
  const state=this.load(),applications=[...state.applications.filter(app=>app.id!==application.id),structuredClone(application)];
  if(applications.length>8)throw Error("At most eight applications can be prepared. Remove a preparation first.");
  this.save({...state,active:application.id,applications});this.active=application.id;
 }
 remove(application:string){const state=this.load(),applications=state.applications.filter(app=>app.id!==application);this.save({...state,applications,active:state.active===application?applications.at(-1)?.id:state.active});if(this.active===application)this.active=undefined;}
 forms(){return structuredClone(this.load().forms);}
 form(key:string){return structuredClone(this.load().forms.find(form=>form.key===key));}
 saveForm(draft:LocalFormDraft){
  const state=this.load(),app=state.applications.find(app=>app.id===draft.application);
  if(!app||!sameBinding(app.binding,draft.binding)||!draft.key||!draft.schema||!draft.target.type||!draft.target.id||!object(draft.values)||!time(draft.at))throw Error("Prepare this exact application version before saving a local draft.");
  const actions=app.reads["/v1/actions"] as Action[]|undefined,action=actions?.find(action=>action.schema===draft.schema&&action.target===draft.target.type);
  if(!action||action.schemaVersion?.version!==draft.schemaVersion?.version||action.schemaVersion?.family!==draft.schemaVersion?.family)throw Error("Prepare this exact application version before saving a local draft.");
  const forms=[...state.forms.filter(form=>form.key!==draft.key),structuredClone(draft)];
  if(forms.length>32)throw Error("At most 32 local drafts can be saved. Remove a draft first.");
  this.save({...state,forms});
 }
 removeForm(key:string){const state=this.load();this.save({...state,forms:state.forms.filter(form=>form.key!==key)});}
}

export function readPath(path:string):boolean{
 if(metadata.has(path))return true;
 const [url,body,...extra]=path.split("\n");
 if(extra.length||!url?.startsWith("/v1/")||url.startsWith("/v1/sign-in")||url.includes("#")||url.includes("..")||path.length>65536)return false;
 return /^\/v1\/(?:records|interfaces|queries|pages)\//.test(url)&&(!body||/\/query$/.test(url));
}

async function identityKey(connection:Connection){
 const bytes=new TextEncoder().encode(connection.token),hash=await crypto.subtle.digest("SHA-256",bytes);
 return `prepared-identity:${serverOf(connection)}:${Array.from(new Uint8Array(hash),byte=>byte.toString(16).padStart(2,"0")).join("")}`;
}
type IdentityIndex={active:string;tenants:Record<string,Scope>};
function identityIndex(raw:string,server:string):IdentityIndex{
 try{
  const index:unknown=JSON.parse(raw);
  if(!object(index)||typeof index.active!=="string"||!object(index.tenants)||Object.keys(index.tenants).length>16||!Object.hasOwn(index.tenants,index.active))throw Error();
  for(const [tenant,scope] of Object.entries(index.tenants))if(!object(scope)||scope.server!==server||scope.tenant!==tenant||!tenant||typeof scope.principal!=="string"||!scope.principal)throw Error();
  return index as IdentityIndex;
 }catch{throw Error("Prepared identity could not be opened. Local storage was not replaced.");}
}
export async function rememberPreparedIdentity(connection:Connection){
 const scope=scopeOf(connection),key=await identityKey(connection),raw=localStorage.getItem(key);
 const index=raw===null?{active:scope.tenant,tenants:{}}:identityIndex(raw,scope.server);
 if(!index.tenants[scope.tenant]&&Object.keys(index.tenants).length>=16)throw Error("At most 16 tenant identities can be prepared for this session.");
 localStorage.setItem(key,JSON.stringify({active:scope.tenant,tenants:{...index.tenants,[scope.tenant]:scope}}));
}
export async function preparedIdentity(connection:Connection):Promise<PreparedWorkspace|undefined>{
 const raw=localStorage.getItem(await identityKey(connection));if(raw===null)return;
 const index=identityIndex(raw,serverOf(connection)),scope=index.tenants[connection.tenant||index.active];
 if(!scope)return;
 const prepared=new PreparedWorkspace({...connection,tenant:scope.tenant,principal:scope.principal});
 prepared.read<{tenantId:string;principalId:string}>("/v1/me");return prepared;
}
