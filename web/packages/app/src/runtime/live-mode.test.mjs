import assert from "node:assert/strict";
import test from "node:test";
const { resolvePageLiveMode } = await import("./live-mode.ts");

test("execution mode is declared by explicit authoring props, the preview route, or the published holder - never by holder presence alone", async () => {
  // Published application runtime: no explicit prop, holder not previewing, no preview route.
  assert.equal(resolvePageLiveMode(undefined, false, false), true);
  // Authoring compose canvas and page preview pass explicit live={false}.
  assert.equal(resolvePageLiveMode(false, false, false), false);
  // Preview route of a PUBLISHED definition: the holder knows it is a preview.
  assert.equal(resolvePageLiveMode(undefined, true, false), false);
  // Preview route of an UNPUBLISHED or standalone page: no holder context at
  // all, so the route itself is the only write boundary.
  assert.equal(resolvePageLiveMode(undefined, false, true), false);
  // Explicit live={false} wins even if every other authority says live.
  assert.equal(resolvePageLiveMode(false, false, false), false);
});
