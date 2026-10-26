import { afterEach, expect, test, vi } from "vitest";
import { EdgeClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

test("tenant SSE separates queue-only progress, ignores keepalive and coalesces mixed bursts into data changes", async () => {
  const stop = new AbortController();
  const stream = new ReadableStream({ start(controller) {
    stop.signal.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")));
    for (const frame of ["event: operations\ndata: 1\n\n", ": keep-alive\n\n", "event: operations\ndata: 2\n\nevent: changed\ndata: 3\n\n"]) {
      controller.enqueue(new TextEncoder().encode(frame));
    }
  } });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream)));
  const client = new EdgeClient({ server: "https://test.invalid", token: "test", tenant: "tenant", principal: "member" });
  const events: string[] = [];
  await client.follow(kind => { events.push(kind); if (kind === "changed") stop.abort(); }, stop.signal);
  expect(events).toEqual(["operations", "changed"]);
});
