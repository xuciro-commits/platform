import {scalarAssignable,type ScalarValue} from "./decimal";
import type { Api } from "@platform/kernel";
import type { RecordSource } from "@platform/ui";
import { PageSessionStore, type RecordReference } from "./Session";
export type ApplicationReads = {source:RecordSource;queries:Record<string,Api.PageQuery>};

/** Ephemeral application-instance state, owned by one workspace. Values never
 * appear in routes, persisted layout or the module's global scope. */
export class ApplicationSession {
  readonly readScope = crypto.randomUUID();
  private state: Record<string, ScalarValue> = {};
  private headerCollapsed:boolean|undefined;
  headerSnapshot=()=>this.headerCollapsed;
  setHeaderCollapsed=(collapsed:boolean)=>{if(!this.alive)return;this.headerCollapsed=collapsed;this.listeners.forEach(listener=>listener());};
  private listeners = new Set<() => void>();
  private pages = new Map<symbol, () => void>();
  private alive = true;
  private recordOwners=new Map<string,symbol>();
  private recordIntents=new Map<string,symbol>();
  retired = false;
  readonly variables: Record<string, Api.PageVariable>;
  readonly reads?:PageSessionStore;
  readonly queries:Record<string,Api.PageQuery>;
  constructor(variables: Record<string, Api.PageVariable>, reads?:ApplicationReads) {
    this.variables = variables;this.queries=reads?.queries??{};
    const filters=Object.entries(variables).filter(([,v])=>v.mode==="resource"&&v.type==="filter"&&v.source?.object);
    if(reads){this.reads=new PageSessionStore(reads.source,{objects:new Map(Object.entries(variables).filter(([,v])=>v.mode==="resource"&&["record","record-set"].includes(v.type)&&v.source?.object).map(([id,v])=>[id,v.source!.object!.name])),filterObjects:new Map(filters.map(([id,v])=>[id,v.source!.object!.name])),filterFields:new Map(filters.map(([id,v])=>[id,new Set(v.source!.fields??[])])),filterSelections:new Map(),filterQueries:new Map(),children:new Map(),queryParents:new Map()});this.reads.subscribe(()=>{if(this.alive){this.state={...this.state};this.listeners.forEach((listener)=>listener());}});}
  }
  snapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  set(id: string, value: ScalarValue) {
    const variable = this.variables[id];
    if (!this.alive || variable?.mode !== "state" || variable.scope !== "application" || !scalarAssignable(variable.type,value) || this.state[id] === value) return;
    this.state = { ...this.state, [id]: value }; this.listeners.forEach((listener) => listener());
  }
  select(id:string,reference:RecordReference|undefined,owner:symbol,onlyOwner=false) {
    const variable=this.variables[id];
    if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="record"||variable.source?.kind!=="record"||!this.reads||onlyOwner&&this.recordOwners.get(id)!==owner||reference&&(reference.object!==variable.source.object?.name||!reference.id))return;
    this.recordIntents.delete(id);
    if(reference)this.recordOwners.set(id,owner);else this.recordOwners.delete(id);
    this.reads.selectReference(id,reference);
  }
  /** Reserve the user's latest choice while its producer confirms window membership.
   * Another producer, a clear or teardown invalidates this completion. */
  beginSelection(id:string,owner:symbol):((reference:RecordReference|undefined)=>void)|undefined {
    const variable=this.variables[id];
    if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="record"||variable.source?.kind!=="record"||!this.reads)return;
    this.select(id,undefined,owner);
    const intent=Symbol();this.recordIntents.set(id,intent);this.recordOwners.set(id,owner);
    return reference=>{if(this.recordIntents.get(id)===intent)this.select(id,reference,owner,true);};
  }
  selectSet(id:string,references:RecordReference[]|undefined,owner:symbol,onlyOwner=false) {
    const variable=this.variables[id];if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="record-set"||variable.source?.kind!=="record-set"||!this.reads||onlyOwner&&this.recordOwners.get(id)!==owner||!this.reads.canSelectSetReferences(id,references))return;
    this.recordIntents.delete(id);if(references?.length)this.recordOwners.set(id,owner);else this.recordOwners.delete(id);void this.reads.selectSetReferences(id,references);
  }
  beginSetSelection(id:string,owner:symbol):((references:RecordReference[]|undefined)=>void)|undefined {
    const variable=this.variables[id];if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="record-set"||variable.source?.kind!=="record-set"||!this.reads)return;
    this.selectSet(id,undefined,owner);const intent=Symbol();this.recordIntents.set(id,intent);this.recordOwners.set(id,owner);
    return references=>{if(this.recordIntents.get(id)===intent)this.selectSet(id,references,owner,true);};
  }
  filter(id:string,field:string,value:unknown) {
    const variable=this.variables[id];if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="filter"||variable.source?.kind!=="filter")return;this.reads?.filterResource(id,field,value);
  }
  attach(owner: symbol, close: () => void) { this.alive = true; this.retired = false; this.pages.set(owner, close); this.reads?.activate(); }
  detach(owner: symbol) { this.pages.delete(owner); if (!this.pages.size) this.clear(); }
  close() { const pages = [...this.pages.values()]; this.clear(); pages.forEach((close) => close()); }
  clear() { this.alive = false; this.retired = true; this.reads?.dispose(); this.recordOwners.clear(); this.recordIntents.clear(); this.state = {}; this.listeners.forEach((listener) => listener()); }
}

export class ApplicationSessionHub {
  private sessions = new Map<string, ApplicationSession>();
  clear(){for(const session of this.sessions.values())session.clear();}
  get(key: string, variables: Record<string, Api.PageVariable>, reads?:ApplicationReads) {
    let session = this.sessions.get(key);
    if (!session || session.retired) {
      for (const [id, old] of this.sessions) if (old.retired) this.sessions.delete(id);
      if (this.sessions.size >= 64) throw new Error("Application instance limit exceeded.");
      this.sessions.set(key, session = new ApplicationSession(variables,reads));
    }
    return session;
  }
}
