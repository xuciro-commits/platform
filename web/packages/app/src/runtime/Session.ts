import {isDecimal} from "./decimal";
import type {PropertyReader} from "./variables";
import type {ScalarValue} from "./decimal";
import type { EntityRecord, RecordPageData, RecordQuery, RecordSource, RecordView } from "@platform/ui";

export type RecordReference = { object: string; id: string };
export type ReadState<T> = { status: "empty"; value?: T } | { status: "pending"; value?: T } | { status: "value"; value: T } | { status: "error"; error: string };
export type QueryWindow = {
  object: string; query: RecordQuery; revision?: number;
  records: RecordReference[]; total: number; complete: boolean;
};
export type QueryView = {search?:string;sort?:string[];offset?:number};
export type PageSessionSnapshot = {
  views: Record<string,QueryView & {base:string}>;
  scalars: Record<string, ScalarValue>;
  items: Record<string, Record<string, ScalarValue>>;
  records: Record<string, ReadState<RecordReference>>;
  filters: Record<string, Record<string, unknown>>;
  queries: Record<string, ReadState<QueryWindow>>;
};
export type SelectionPlan = { filterObjects?:ReadonlyMap<string,string>;filterFields?:ReadonlyMap<string,ReadonlySet<string>>; objects: ReadonlyMap<string, string>; children: ReadonlyMap<string, ReadonlySet<string>>; queryParents: ReadonlyMap<string, string>; overlayScopes?:ReadonlyMap<string,{queries:ReadonlySet<string>;selections:ReadonlySet<string>;filters?:ReadonlySet<string>;loops?:ReadonlySet<string>}>; filterSelections?:ReadonlyMap<string,ReadonlySet<string>>;filterQueries?:ReadonlyMap<string,ReadonlySet<string>>;querySelections?:ReadonlyMap<string,ReadonlySet<string>> };

/** One member/definition-scoped presentation session. References and query
 * windows are values; record fields/revisions are a separate ephemeral cache.
 * The source remains the sole authority for reads and record permissions.
 */
export class PageSessionStore {
  private childSessions=new Map<string,{owner:string;session:PageSessionStore}>();
  childSession(owner:string,key:string) {
    let child=this.childSessions.get(key);
    if(!child){child={owner,session:new PageSessionStore(this.readSource(),{objects:new Map(),children:new Map(),queryParents:new Map()})};this.childSessions.set(key,child);}
    return child.session;
  }
  private source: RecordSource;
  private plan: SelectionPlan;
  private state: PageSessionSnapshot = { views:{}, scalars: {}, items: {}, records: {}, filters: {}, queries: {} };
  private listeners = new Set<() => void>();
  private viewCache=new Map<string,RecordView>();
  private recordCache = new Map<string, EntityRecord>();
  private recordEpoch = new Map<string, number>();
  private selectionQueries=new Map<string,string>();
  private queries = new Map<string, { signature: string; object: string; promise: Promise<RecordPageData> }>();
  private sources = new Map<string, RecordSource>();
  private querySignatures = new Map<string, string>();
  querySignature(key: string) { return this.querySignatures.get(key); }
  private queryData = new Map<string,RecordPageData>();
  queryPage(key:string,signature:string) { return this.querySignatures.get(key)===signature ? this.queryData.get(key) : undefined; }
  reconcileQueryBase(key:string,base?:string) {
    const current=this.state.views[key];
    if(current&&current.base!==base){const views={...this.state.views};delete views[key];this.publish({views});}
  }
  private selectionKeys(query:string){return [...(this.plan.querySelections?.get(query)??[])].filter((slot)=>!this.selectionQueries.has(slot)||this.selectionQueries.get(slot)===query);}
  setQueryView(key:string,base:string,change:QueryView) {
    const current=this.state.views[key]?.base===base?this.state.views[key]:{base};
    this.queries.delete(key);this.querySignatures.delete(key);this.queryData.delete(key);
    const keys=this.selectionKeys(key);
    this.publish({views:{...this.state.views,[key]:{...current,...change}},records:this.clear(keys),queries:this.invalidate(keys)});
  }
  private queryObjects = new Map<string, string>();
  private version = 0;
  private itemOwners = new Map<string, string>();
  private loopItems = new Map<string, Set<string>>();
  hasLoopItem(owner: string, key: string) { return this.loopItems.get(owner)?.has(key) === true; }
  private loopQueries = new Map<string, string>();
  private reads = new Map<string, Promise<RecordView>>();
  private readAdapter?: RecordSource;
  private sourceRevision?: number;
  private sourceScope?: string;
  private disposed = false;
  constructor(source: RecordSource, plan: SelectionPlan) { this.source = source; this.sourceRevision = source.revision; this.sourceScope = source.scope; this.plan = plan; }
  activate() { this.disposed = false; }
  snapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish(patch: Partial<PageSessionSnapshot>) {
    if (this.disposed) return;
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((listener) => listener());
  }
  setScalar(id: string, value: ScalarValue) {
    this.setScalars({ [id]: value });
  }
  setScalars(values: Record<string, ScalarValue>, reset: string[] = []) {
    const scalars = { ...this.state.scalars };
    for (const id of reset) delete scalars[id];
    if (reset.some((id) => Object.hasOwn(this.state.scalars, id)) || Object.entries(values).some(([id, value]) => scalars[id] !== value)) this.publish({ scalars: { ...scalars, ...values } });
  }
  private overlayEpochs = new Map<string, number>();
  overlayEpoch(owner: string) { return this.overlayEpochs.get(owner) ?? 0; }
  endOverlay(owner: string) {
    this.overlayEpochs.set(owner, this.overlayEpoch(owner) + 1);
    const scope=this.plan.overlayScopes?.get(owner);
    if(!scope)return;
    for(const [key,child] of this.childSessions)if(scope.loops?.has(child.owner)){child.session.dispose();this.childSessions.delete(key)}
    const queries={...this.state.queries},views={...this.state.views},filters={...this.state.filters};
    for(const key of scope.filters??[])delete filters[key];
    for(const key of scope.queries){this.queries.delete(key);this.querySignatures.delete(key);this.queryData.delete(key);delete queries[key];delete views[key];}
    const items={...this.state.items};
    for(const owner of scope.loops??[]){for(const [key,parent] of this.itemOwners){if(parent===owner){delete items[key];this.itemOwners.delete(key)}}this.loopItems.delete(owner);this.loopQueries.delete(owner);}
    this.publish({queries,views,items,filters,records:this.clear([...scope.selections])});
  }

  resetQueries(keys: string[]) {
    const queries = { ...this.state.queries };
    for (const key of keys) { this.queries.delete(key); this.querySignatures.delete(key);this.queryData.delete(key); delete queries[key]; }
    const selections=keys.flatMap((key)=>this.selectionKeys(key));
    this.publish({ queries, ...(selections.length?{records:this.clear(selections)}:{}) });
  }
  reconcileExternalWindow(key:string,signature:string,ids?:string[]) {
    const changed=this.querySignatures.get(key)!==signature;
    this.querySignatures.set(key,signature);
    const slots=this.selectionKeys(key);
    const removed=changed?slots:ids?slots.filter((slot)=>{const value=this.state.records[slot];return value&&"value" in value&&value.value&&!ids.includes(value.value.id);}):[];
    if(removed.length)this.publish({records:this.clear(removed)});
  }
  setItemScalar(owner: string, key: string, id: string, value: ScalarValue) {
    this.itemOwners.set(key, owner);
    if (this.state.items[key]?.[id] !== value) this.publish({ items: { ...this.state.items, [key]: { ...this.state.items[key], [id]: value } } });
  }
  reconcileLoop(owner: string, signature: string, keys: string[]) {
    const changed = this.loopQueries.get(owner) !== signature, allowed = new Set(keys), items = { ...this.state.items };
    let removed = false;
    for(const [key,child] of this.childSessions)if(child.owner===owner&&(changed||!allowed.has(key))){child.session.dispose();this.childSessions.delete(key);removed=true;}
    for (const [key, parent] of this.itemOwners) if (parent === owner && (changed || !allowed.has(key))) { delete items[key]; this.itemOwners.delete(key); removed = true; }
    this.loopQueries.set(owner, signature); this.loopItems.set(owner, allowed);
    if (removed) this.publish({ items });
  }
  itemValues(owner: string, signature: string, key: string) { return this.loopQueries.get(owner) === signature ? this.state.items[key] : undefined; }
  clearLoop(owner: string) { this.reconcileLoop(owner, "", []); this.loopQueries.delete(owner); }
  readSource(): RecordSource {
    if (!this.readAdapter) {
      const session = this;
      this.readAdapter = {
        entity: (type) => session.source.entity(type), list: (type, query) => session.source.list(type, query),
        get: (type, id) => session.readReference(type, id),
        get scope() { return session.source.scope; }, get revision() { return session.version; },
      };
    }
    return this.readAdapter;
  }
  private readReference(type: string, id: string): Promise<RecordView> {
    const key = JSON.stringify([type, id]), existing = this.reads.get(key);
    if (existing) return existing;
    const source = this.source, scope = source.scope, version = this.version;
    if (this.disposed) return Promise.reject(new Error("Page session ended"));
    if (this.reads.size >= 256) this.reads.delete(this.reads.keys().next().value!);
    const promise = Promise.resolve().then(() => source.get(type, id)).then((view) => {
      if (this.disposed || source !== this.source || source.scope !== scope || version !== this.version) throw new Error("Obsolete record read");
      if (view.record.id !== id) throw new Error("Record identity mismatch");
      this.cacheView(key,view);return view;
    });
    this.reads.set(key, promise);
    void promise.catch(() => { if (this.reads.get(key) === promise) this.reads.delete(key); });
    return promise;
  }
  private cacheView(key:string,view:RecordView){if(!this.viewCache.has(key)&&this.viewCache.size>=256)this.viewCache.delete(this.viewCache.keys().next().value!);this.viewCache.set(key,view);}
  property:PropertyReader=(reference,field,type)=>{const descriptor=this.source.entity(reference.object)?.fields.find((f)=>f.name===field),view=this.viewCache.get(JSON.stringify([reference.object,reference.id]));if(!descriptor||!view)return {status:"error",code:"Property value is unavailable."};if(view.valueErrors?.[field])return {status:"error",code:view.valueErrors[field]!};if(!view.values||!Object.hasOwn(view.values,field))return {status:"empty"};const value=view.values[field];if(type==="decimal"?!isDecimal(value):typeof value!==type)return {status:"error",code:"Property value type mismatch"};return {status:"value",value:value as import("./decimal").ScalarValue};};
  selected(key: string): EntityRecord | undefined { return this.recordCache.get(key); }
  private descendants(keys: string[]) {
    const seen = new Set<string>();
    const visit = (key: string) => { if (seen.has(key)) return; seen.add(key); for (const child of this.plan.children.get(key) ?? []) visit(child); };
    keys.forEach(visit); return seen;
  }
  private invalidate(keys: string[], object?: string) {
    const changed = this.descendants(keys), queries = { ...this.state.queries };
    for (const [key, type] of this.queryObjects) {
      if (object && type === object || changed.has(this.plan.queryParents.get(key) ?? "")) {
        this.queries.delete(key); this.querySignatures.delete(key); queries[key] = { status: "empty" };
      }
    }
    return queries;
  }
  private clear(keys: string[]) {
    const records = { ...this.state.records }, visited = new Set<string>();
    const remove = (key: string) => {
      if (visited.has(key)) return;
      visited.add(key); records[key] = { status: "empty" }; this.recordCache.delete(key);this.selectionQueries.delete(key);
      this.recordEpoch.set(key, (this.recordEpoch.get(key) ?? 0) + 1);
      for (const child of this.plan.children.get(key) ?? []) remove(child);
    };
    keys.forEach(remove);
    return records;
  }
  select(key: string, record?: EntityRecord, query?:string) {
    const object = this.plan.objects.get(key);
    if (this.disposed || !object) return;
    if (record && (typeof record.id !== "string" || !record.id)) return;
    const records = this.clear([key]);
    const queries = this.invalidate([key]);
    if (!record) { this.publish({ records, queries }); return; }
    if(query)this.selectionQueries.set(key,query);
    this.recordCache.set(key, record);
    records[key] = { status: "value", value: { object, id: record.id } };
    this.publish({ records, queries });
    void this.read(key, { object, id: record.id });
  }
  selectReference(key: string, reference?: RecordReference) {
    if (this.disposed || !this.plan.objects.has(key) || reference && (reference.object !== this.plan.objects.get(key) || !reference.id)) return;
    this.publish({ records: this.clear([key]), queries: this.invalidate([key]) });
    if (reference) void this.read(key, reference);
  }
  filter(object: string, field: string, value: unknown, owner?:string) {
    const filterKey=owner?`overlay:${owner}/${object}`:object;
    if(owner&&!this.plan.overlayScopes?.get(owner)?.filters?.has(filterKey))return;
    this.writeFilter(filterKey,object,field,value);
  }
  filterResource(key:string,field:string,value:unknown) {
    const object=this.plan.filterObjects?.get(key);if(!object||!this.plan.filterFields?.get(key)?.has(field)||this.disposed)return;this.writeFilter(key,object,field,value);
  }
  private writeFilter(filterKey:string,object:string,field:string,value:unknown) {
    const descriptor = this.source.entity(object)?.fields.find((item) => item.name === field);
    if (!descriptor || !["choice", "boolean", "reference"].includes(descriptor.type)) return;
    if (value !== undefined && value !== "" && (descriptor.type === "boolean" ? typeof value !== "boolean"
      : typeof value !== "string" || descriptor.type === "choice" && !descriptor.choices?.includes(value))) return;
    if (Object.is(this.state.filters[filterKey]?.[field], value === "" ? undefined : value)) return;
    // Filtering invalidates every selection over this object and its descendants.
    const keys = this.plan.filterSelections?[...(this.plan.filterSelections.get(filterKey)??[])]:[...this.plan.objects].filter(([,type])=>type===object).map(([key])=>key);
    const fields = { ...this.state.filters[filterKey] };
    if (value === undefined || value === "") delete fields[field]; else fields[field] = value;
    const queries=this.invalidate(keys,this.plan.filterQueries?undefined:object);
    for(const key of this.plan.filterQueries?.get(filterKey)??[]){this.queries.delete(key);this.querySignatures.delete(key);this.queryData.delete(key);queries[key]={status:"empty"};}
    this.publish({filters:{...this.state.filters,[filterKey]:fields},records:this.clear(keys),queries});
  }
  private async read(key: string, reference: RecordReference) {
    const epoch = (this.recordEpoch.get(key) ?? 0) + 1, source = this.source, scope = source.scope;
    this.recordEpoch.set(key, epoch);
    this.publish({ records: { ...this.state.records, [key]: { status: "pending", value: reference } } });
    try {
      const view = await source.get(reference.object, reference.id);
      if (this.disposed || source !== this.source || source.scope !== scope || this.recordEpoch.get(key) !== epoch) return;
      if (view.record.id !== reference.id) throw new Error("Record identity mismatch");
      this.recordCache.set(key, view.record);this.cacheView(JSON.stringify([reference.object,reference.id]),view);
      this.publish({ records: { ...this.state.records, [key]: { status: "value", value: reference } } });
    } catch (error) {
      if (this.disposed || source !== this.source || source.scope !== scope || this.recordEpoch.get(key) !== epoch) return;
      const records = this.clear([key]); records[key] = { status: "error", error: String(error) };
      this.publish({ records, queries: this.invalidate([key]) });
    }
  }
  updateSource(source: RecordSource) {
    if (this.disposed || source === this.source && source.revision === this.sourceRevision && source.scope === this.sourceScope) return;
    const changedScope = source.scope !== this.sourceScope;
    this.source = source; this.sourceRevision = source.revision; this.sourceScope = source.scope; this.version++;
    for(const child of this.childSessions.values())child.session.updateSource(this.readSource());
    this.queries.clear();this.queryData.clear(); this.reads.clear();this.viewCache.clear();
    this.publish({ queries: Object.fromEntries(Object.keys(this.state.queries).map((key) => [key, { status: "empty" }])) });
    if (changedScope) {
      this.itemOwners.clear(); this.loopQueries.clear(); this.loopItems.clear(); this.querySignatures.clear();
      this.publish({ views:{}, scalars: {}, items: {}, filters: {}, records: this.clear([...this.plan.objects.keys()]) });
      return;
    }
    for (const [key, record] of this.recordCache) {
      const object = this.plan.objects.get(key);
      if (object) void this.read(key, { object, id: record.id });
    }
  }
  querySource(key: string): RecordSource {
    let source = this.sources.get(key);
    if (!source) {
      const session = this;
      source = {
        entity: (object) => session.source.entity(object), get: (object, id) => session.source.get(object, id),
        list: (object, query) => session.query(key, object, query),
        get aggregate() { return session.source.aggregate; },
        get revision() { return session.version; },
        get scope() { return session.source.scope; },
      };
      this.sources.set(key, source);
    }
    return source;
  }
  clearQuery(key: string) { if (this.queries.has(key) || this.state.queries[key]) this.resetQueries([key]); }
  private query(key: string, object: string, input: RecordQuery): Promise<RecordPageData> {
    const query = structuredClone(input), source = this.source, scope = source.scope;
    const queryShape = JSON.stringify([object,query]);
    const changed=this.querySignatures.get(key)!==queryShape;
    this.queryObjects.set(key, object);
    this.querySignatures.set(key, queryShape);
    if(changed){this.queryData.delete(key);const keys=this.selectionKeys(key);if(keys.length)this.publish({records:this.clear(keys),queries:this.invalidate(keys)});}
    const signature = JSON.stringify([this.version, object, query]);
    const previous = this.queries.get(key);
    if (previous?.signature === signature) return previous.promise;
    if (this.disposed) return Promise.reject(new Error("Page session ended"));
    this.publish({ queries: { ...this.state.queries, [key]: { status: "pending" } } });
    const revision = source.revision;
    const request = { signature, object, promise: Promise.resolve().then(() => source.list(object, query)) };
    this.queries.set(key, request);
    request.promise = request.promise.then((page) => {
      if (!this.disposed && this.queries.get(key) === request && source === this.source && source.scope === scope) {
        this.queryData.set(key,page);
        const vanished=this.selectionKeys(key).filter((slot)=>{const state=this.state.records[slot];return state&&"value" in state&&state.value&&!page.records.some((record)=>record.id===state.value!.id);});
        if(vanished.length)this.publish({records:this.clear(vanished),queries:this.invalidate(vanished)});
        const value: QueryWindow = { object, query, revision, records: page.records.map((record) => ({ object, id: record.id })), total: page.total,
          complete: (query.offset ?? 0) === 0 && page.records.length === page.total };
        this.publish({ queries: { ...this.state.queries, [key]: page.records.length ? { status: "value", value } : { status: "empty", value } } });
      }
      return page;
    }, (error) => {
      if (!this.disposed && this.queries.get(key) === request && source === this.source && source.scope === scope) {
        this.queries.delete(key);this.queryData.delete(key);const keys=this.selectionKeys(key);
        if(keys.length)this.publish({records:this.clear(keys)});
        this.publish({ queries: { ...this.state.queries, [key]: { status: "error", error: String(error) } } });
      }
      throw error;
    });
    return request.promise;
  }
  dispose() {
    for(const child of this.childSessions.values())child.session.dispose();this.childSessions.clear();
    this.disposed = true; this.querySignatures.clear(); this.queries.clear(); this.queryObjects.clear();this.queryData.clear(); this.recordCache.clear();this.viewCache.clear();this.selectionQueries.clear(); this.sources.clear(); this.reads.clear(); this.itemOwners.clear(); this.loopQueries.clear(); this.loopItems.clear();
    for (const [key, epoch] of this.recordEpoch) this.recordEpoch.set(key, epoch + 1);
    this.state = { views:{}, scalars: {}, items: {}, records: {}, filters: {}, queries: {} };
    this.listeners.clear();
  }
}
