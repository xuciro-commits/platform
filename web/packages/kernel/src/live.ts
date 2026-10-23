import type { LiveQueryFrame, LiveQueryResult } from "./gen/host";

const metadataPaths = new Set(["/v1/me", "/v1/actions", "/v1/entities", "/v1/definitions", "/v1/protocols", "/v1/views", "/v1/apps"]);
type LiveConnection = { paths: string[]; controller: AbortController };

/** Shared bounded connections, with leases owned by visible readers. No durable
 * state or permission decisions live here: snapshots are original host reads. */
export class LiveReads {
  private readers = new Map<string, Set<() => void>>();
  private snapshots = new Map<string, LiveQueryResult>();
  paths() { return [...this.readers.keys()]; }
  private connections = new Set<LiveConnection>();
  private timer?: ReturnType<typeof setTimeout>;
  private running = false;
  constructor(private connect: (paths: string[], signal: AbortSignal) => Promise<Response>) {}
  dispose() { this.running = false; this.stop(); this.snapshots.clear(); }
  enable() { this.running = true; this.restart(); return () => { this.running = false; this.stop(); this.snapshots.clear(); }; }
  subscribe(path: string, changed: () => void) {
    let listeners = this.readers.get(path);
    if (!listeners) { listeners = new Set(); this.readers.set(path, listeners); }
    const listener = () => changed();
    listeners.add(listener);
    if (listeners.size === 1) this.restart();
    return () => { listeners.delete(listener); if (!listeners.size) { this.readers.delete(path); this.snapshots.delete(path); this.restart(); } };
  }
  read<T>(path: string): { value: T } | undefined {
    const result = this.snapshots.get(path);
    if (!result) return;
    if (result.status === 0) throw Error(`${path.split("\n")[0]}: live connection unavailable`);
    if (result.status < 200 || result.status >= 300) throw Error(`${path.split("\n")[0]}: HTTP ${result.status}`);
    return { value: result.body as T };
  }
  refresh() { this.snapshots.clear(); this.stop(); this.restart(); }
  private close(connection: LiveConnection) { this.connections.delete(connection); connection.controller.abort(); }
  private stop() { clearTimeout(this.timer); for (const connection of this.connections) this.close(connection); }
  private batches(paths: string[]) {
    const batches: string[][] = [], encoder = new TextEncoder();
    let batch: string[] = [];
    for (const path of paths.sort()) {
      if (batch.length && (batch.length === 32 || encoder.encode(JSON.stringify([...batch, path])).length > (64 << 10))) {
        batches.push(batch); batch = [];
      }
      batch.push(path);
    }
    if (batch.length) batches.push(batch);
    return batches;
  }
  private start(paths: string[]) {
    if (new TextEncoder().encode(JSON.stringify(paths)).length > (64 << 10)) { this.failure(paths, 413); return; }
    const connection = { paths, controller: new AbortController() };
    this.connections.add(connection);
    void this.follow(paths, connection.controller.signal);
  }
  private restart() {
    clearTimeout(this.timer);
    if (!this.running || !this.readers.size) { this.stop(); return; }
    this.timer = setTimeout(() => {
      const paths = [...this.readers.keys()];
      // Keep metadata independent of record leases added by mounted editors.
      // Identical batches retain their connection; removed queries leave it.
      const batches = [...this.batches(paths.filter(path => metadataPaths.has(path))), ...this.batches(paths.filter(path => !metadataPaths.has(path)))];
      const pending = new Map(batches.map(batch => [JSON.stringify(batch), batch]));
      for (const connection of this.connections) {
        if (!pending.delete(JSON.stringify(connection.paths))) this.close(connection);
      }
      for (const batch of pending.values()) this.start(batch);
    }, 50);
  }
  private failure(paths: string[], status: number) {
    for (const path of paths) if (this.readers.has(path)) {
      const previous = this.snapshots.get(path);
      const unchanged = previous?.status === status && JSON.stringify(previous.body) === JSON.stringify({ error: "Live read unavailable" });
      this.snapshots.set(path, { path, status, body: { error: "Live read unavailable" } });
      if (!unchanged) this.readers.get(path)?.forEach(changed => changed());
    }
  }
  private async follow(paths: string[], signal: AbortSignal) {
    let wait = 1000;
    while (!signal.aborted) {
      let status = 0;
      try {
        const response = await this.connect(paths, signal);
        if (signal.aborted) return;
        if (!response.ok || !response.body) {
          status = response.status;
          // A rejected registration needs a changed lease/identity, not retries.
          if ([400, 401, 403, 413].includes(status)) { this.failure(paths, status); return; }
          throw Error("Live connection unavailable");
        }
        const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          if (signal.aborted) return;
          wait = 1000; buffer += value;
          const events = buffer.split("\n\n"); buffer = events.pop() ?? "";
          for (const event of events) {
            if (!event.startsWith("event: snapshot\n")) continue;
            const frame = JSON.parse(event.slice(event.indexOf("data: ") + 6)) as LiveQueryFrame;
            // Install the whole frame before notifying inventory/multi-read users.
            // Unchanged answers must not retire the table selection that opened it.
            const changed = new Set<string>();
            for (const result of frame.results ?? []) if (!signal.aborted && paths.includes(result.path) && this.readers.has(result.path)) {
              const previous = this.snapshots.get(result.path);
              if (!previous || previous.status !== result.status || JSON.stringify(previous.body) !== JSON.stringify(result.body)) changed.add(result.path);
              this.snapshots.set(result.path, result);
            }
            for (const path of changed) this.readers.get(path)?.forEach(notify => notify());
          }
        }
        if (signal.aborted) return;
        this.failure(paths, 0);
      } catch { if (signal.aborted) return; this.failure(paths, status); }
      await new Promise<void>(resolve => {
        const finish = () => { clearTimeout(timer); signal.removeEventListener("abort", finish); resolve(); };
        const timer = setTimeout(finish, wait); signal.addEventListener("abort", finish, { once: true });
      });
      wait = Math.min(wait * 2, 30000);
    }
  }
}
