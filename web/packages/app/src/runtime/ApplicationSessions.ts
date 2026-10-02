import type { Api } from "@platform/kernel";
import type { RecordSource } from "@platform/ui";
import { PageSessionStore, type RecordReference } from "./Session";
export type ApplicationReads = {source:RecordSource;queries:Record<string,Api.PageQuery>};

/** Ephemeral application-instance state, owned by one workspace. Values never
 * appear in routes, persisted layout or the module's global scope. */
export class ApplicationSession {
  private state: Record<string, string | boolean> = {};
  private listeners = new Set<() => void>();
  private pages = new Map<symbol, () => void>();
  private alive = true;
  private recordOwners=new Map<string,symbol>();
  retired = false;
  readonly variables: Record<string, Api.PageVariable>;
  readonly reads?:PageSessionStore;
  readonly queries:Record<string,Api.PageQuery>;
  constructor(variables: Record<string, Api.PageVariable>, reads?:ApplicationReads) {
    this.variables = variables;this.queries=reads?.queries??{};
    if(reads){this.reads=new PageSessionStore(reads.source,{objects:new Map(Object.entries(variables).filter(([,v])=>v.mode==="resource"&&v.type==="record"&&v.source?.object).map(([id,v])=>[id,v.source!.object!.name])),children:new Map(),queryParents:new Map()});this.reads.subscribe(()=>{if(this.alive){this.state={...this.state};this.listeners.forEach((listener)=>listener());}});}
  }
  snapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  set(id: string, value: string | boolean) {
    const variable = this.variables[id];
    if (!this.alive || variable?.mode !== "state" || variable.scope !== "application" || typeof value !== variable.type || typeof value === "string" && new TextEncoder().encode(value).length > 4096 || this.state[id] === value) return;
    this.state = { ...this.state, [id]: value }; this.listeners.forEach((listener) => listener());
  }
  select(id:string,reference:RecordReference|undefined,owner:symbol,onlyOwner=false) {
    const variable=this.variables[id];
    if(!this.alive||this.retired||variable?.mode!=="resource"||variable.type!=="record"||variable.source?.kind!=="record"||!this.reads||onlyOwner&&this.recordOwners.get(id)!==owner||reference&&(reference.object!==variable.source.object?.name||!reference.id))return;
    if(reference)this.recordOwners.set(id,owner);else this.recordOwners.delete(id);
    this.reads.selectReference(id,reference);
  }
  attach(owner: symbol, close: () => void) { this.alive = true; this.retired = false; this.pages.set(owner, close); this.reads?.activate(); }
  detach(owner: symbol) { this.pages.delete(owner); if (!this.pages.size) this.clear(); }
  close() { const pages = [...this.pages.values()]; this.clear(); pages.forEach((close) => close()); }
  clear() { this.alive = false; this.retired = true; this.reads?.dispose(); this.recordOwners.clear(); this.state = {}; this.listeners.forEach((listener) => listener()); }
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
