import { afterEach, expect, test, vi } from "vitest";
import { EdgeClient } from "./client";

afterEach(() => vi.unstubAllGlobals());
const client = () => new EdgeClient({ server: "https://test.invalid", token: "test", tenant: "t", principal: "m" });
const rows = Array.from({ length: 501 }, (_, i) => ({ id: `row-${i}` }));

test("an inventory includes row 501 while carrying the same caller on both pages", async () => {
  const fetched = vi.fn(async (url: string, options: RequestInit) => {
    expect(options.headers).toMatchObject({ Authorization: "Bearer test", "Platform-Tenant": "t" });
    const offset = Number(new URL(url).searchParams.get("offset"));
    return new Response(JSON.stringify({ total: rows.length, records: rows.slice(offset, offset + 500) }));
  });
  vi.stubGlobal("fetch", fetched);
  expect((await client().inventory("build.process")).records.at(-1)).toEqual(rows[500]);
  expect(fetched).toHaveBeenCalledTimes(2);
});

test("a failed or incomplete second page cannot masquerade as a complete inventory", async () => {
  for (const second of [new Response("", { status: 503 }), new Response(JSON.stringify({ total: 501, records: [] })),
    new Response(JSON.stringify({ total: 500, records: [] }))]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ total: 501, records: rows.slice(0, 500) }))).mockResolvedValueOnce(second));
    await expect(client().inventory("build.process")).rejects.toThrow();
  }
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ total: 1001, records: rows.slice(0, 500) }))));
  await expect(client().inventory("build.process")).rejects.toThrow();
});
