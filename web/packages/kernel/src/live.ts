import type { LiveQueryFrame, LiveQueryResult } from "./gen/host";

/** One connection per edge, with leases owned by visible readers. No durable
 * state or permission decisions live here: snapshots are original host reads. */
export class LiveReads {
  private readers = new Map<string, Set<() => void>>();
  private snapshots = new Map<string, LiveQueryResult>();
  private controller?: AbortController;
  private timer?: ReturnType<typeof setTimeout>;
  private running = false;
  constructor(private connect: (paths: string[], signal: AbortSignal) => Promise<Response>) {}
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
  refresh() { this.snapshots.clear(); this.restart(); }
  private stop() { clearTimeout(this.timer); this.controller?.abort(); this.controller = undefined; }
  private restart() {
    this.stop();
    if (!this.running || !this.readers.size) return;
    this.timer = setTimeout(() => {
      const controller = new AbortController(); this.controller = controller;
      void this.follow([...this.readers.keys()].sort(), controller.signal);
    }, 50);
  }
  private failure(paths: string[], status: number) {
    for (const path of paths) if (this.readers.has(path)) {
      this.snapshots.set(path, { path, status, body: { error: "Live read unavailable" } });
      this.readers.get(path)?.forEach(changed => changed());
    }
  }
  private async follow(paths: string[], signal: AbortSignal) {
    let wait = 1000;
    while (!signal.aborted) {
      let status = 0;
      try {
        const response = await this.connect(paths, signal);
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
            for (const result of frame.results ?? []) if (this.readers.has(result.path)) this.snapshots.set(result.path, result);
            for (const result of frame.results ?? []) this.readers.get(result.path)?.forEach(changed => changed());
          }
        }
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
