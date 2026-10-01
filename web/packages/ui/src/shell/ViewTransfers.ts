/** Workspace-local, ephemeral calls. Payloads never enter saved routes. */
export class ViewTransfers {
  private calls = new Map<string, { from: string; to: string; input: unknown; result: (value: unknown) => void }>();
  start(from: string, input: unknown, result: (value: unknown) => void) {
    if (this.calls.size >= 64) throw new Error("Too many open view calls");
    const id = crypto.randomUUID(); this.calls.set(id, { from, to: "", input, result }); return id;
  }
  bind(id: string, to: string) { const call = this.calls.get(id); if (call) call.to = to; }
  read(id: string, to: string) { const call = this.calls.get(id); return call?.to === to ? { input: call.input } : undefined; }
  finish(id: string, to: string, value: unknown) {
    const call = this.calls.get(id); if (!call || call.to !== to) return;
    this.calls.delete(id); call.result(value); return call.from;
  }
  remove(panel: string) { for (const [id, call] of this.calls) if (call.from === panel || call.to === panel) this.calls.delete(id); }
}
