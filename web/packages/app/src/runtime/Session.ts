import type { EntityRecord, RecordPageData, RecordQuery, RecordSource } from "@platform/ui";

export type RecordReference = { object: string; id: string };
export type ReadState<T> = { status: "empty"; value?: T } | { status: "pending"; value?: T } | { status: "value"; value: T } | { status: "error"; error: string };
export type QueryWindow = {
  object: string; query: RecordQuery; revision?: number;
  records: RecordReference[]; total: number; complete: boolean;
};
export type PageSessionSnapshot = {
  scalars: Record<string, string | boolean>;
  records: Record<string, ReadState<RecordReference>>;
  filters: Record<string, Record<string, unknown>>;
  queries: Record<string, ReadState<QueryWindow>>;
};
export type SelectionPlan = { objects: ReadonlyMap<string, string>; children: ReadonlyMap<string, ReadonlySet<string>>; queryParents: ReadonlyMap<string, string> };

/** One member/definition-scoped presentation session. References and query
 * windows are values; record fields/revisions are a separate ephemeral cache.
 * The source remains the sole authority for reads and record permissions.
 */
export class PageSessionStore {
  private source: RecordSource;
  private plan: SelectionPlan;
  private state: PageSessionSnapshot = { scalars: {}, records: {}, filters: {}, queries: {} };
  private listeners = new Set<() => void>();
  private recordCache = new Map<string, EntityRecord>();
  private recordEpoch = new Map<string, number>();
  private queries = new Map<string, { signature: string; object: string; promise: Promise<RecordPageData> }>();
  private sources = new Map<string, RecordSource>();
  private queryObjects = new Map<string, string>();
  private version = 0;
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
  setScalar(id: string, value: string | boolean) {
    if (this.state.scalars[id] !== value) this.publish({ scalars: { ...this.state.scalars, [id]: value } });
  }
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
        this.queries.delete(key); queries[key] = { status: "empty" };
      }
    }
    return queries;
  }
  private clear(keys: string[]) {
    const records = { ...this.state.records }, visited = new Set<string>();
    const remove = (key: string) => {
      if (visited.has(key)) return;
      visited.add(key); records[key] = { status: "empty" }; this.recordCache.delete(key);
      this.recordEpoch.set(key, (this.recordEpoch.get(key) ?? 0) + 1);
      for (const child of this.plan.children.get(key) ?? []) remove(child);
    };
    keys.forEach(remove);
    return records;
  }
  select(key: string, record?: EntityRecord) {
    const object = this.plan.objects.get(key);
    if (this.disposed || !object) return;
    if (record && (typeof record.id !== "string" || !record.id)) return;
    const records = this.clear([key]);
    const queries = this.invalidate([key]);
    if (!record) { this.publish({ records, queries }); return; }
    this.recordCache.set(key, record);
    records[key] = { status: "value", value: { object, id: record.id } };
    this.publish({ records, queries });
    void this.read(key, { object, id: record.id });
  }
  filter(object: string, field: string, value: unknown) {
    const descriptor = this.source.entity(object)?.fields.find((item) => item.name === field);
    if (!descriptor || !["choice", "boolean", "reference"].includes(descriptor.type)) return;
    if (value !== undefined && value !== "" && (descriptor.type === "boolean" ? typeof value !== "boolean"
      : typeof value !== "string" || descriptor.type === "choice" && !descriptor.choices?.includes(value))) return;
    if (Object.is(this.state.filters[object]?.[field], value === "" ? undefined : value)) return;
    // Filtering invalidates every selection over this object and its descendants.
    const keys = [...this.plan.objects].filter(([, type]) => type === object).map(([key]) => key);
    const fields = { ...this.state.filters[object] };
    if (value === undefined || value === "") delete fields[field]; else fields[field] = value;
    this.publish({ filters: { ...this.state.filters, [object]: fields }, records: this.clear(keys), queries: this.invalidate(keys, object) });
  }
  private async read(key: string, reference: RecordReference) {
    const epoch = (this.recordEpoch.get(key) ?? 0) + 1, source = this.source, scope = source.scope;
    this.recordEpoch.set(key, epoch);
    this.publish({ records: { ...this.state.records, [key]: { status: "pending", value: reference } } });
    try {
      const view = await source.get(reference.object, reference.id);
      if (this.disposed || source !== this.source || source.scope !== scope || this.recordEpoch.get(key) !== epoch) return;
      if (view.record.id !== reference.id) throw new Error("Record identity mismatch");
      this.recordCache.set(key, view.record);
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
    this.queries.clear();
    this.publish({ queries: Object.fromEntries(Object.keys(this.state.queries).map((key) => [key, { status: "empty" }])) });
    if (changedScope) {
      this.publish({ scalars: {}, filters: {}, records: this.clear([...this.plan.objects.keys()]) });
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
  private query(key: string, object: string, input: RecordQuery): Promise<RecordPageData> {
    const query = structuredClone(input), source = this.source, scope = source.scope;
    this.queryObjects.set(key, object);
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
        const value: QueryWindow = { object, query, revision, records: page.records.map((record) => ({ object, id: record.id })), total: page.total,
          complete: (query.offset ?? 0) === 0 && page.records.length === page.total };
        this.publish({ queries: { ...this.state.queries, [key]: page.records.length ? { status: "value", value } : { status: "empty", value } } });
      }
      return page;
    }, (error) => {
      if (!this.disposed && this.queries.get(key) === request && source === this.source && source.scope === scope) {
        this.queries.delete(key);
        this.publish({ queries: { ...this.state.queries, [key]: { status: "error", error: String(error) } } });
      }
      throw error;
    });
    return request.promise;
  }
  dispose() {
    this.disposed = true; this.queries.clear(); this.queryObjects.clear(); this.recordCache.clear(); this.sources.clear();
    for (const [key, epoch] of this.recordEpoch) this.recordEpoch.set(key, epoch + 1);
    this.state = { scalars: {}, records: {}, filters: {}, queries: {} };
    this.listeners.clear();
  }
}
