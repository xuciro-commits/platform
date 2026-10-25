import assert from "node:assert/strict";
import test from "node:test";
import { releaseCommandBytes, releaseDraftsSelection, releaseDraftsValue } from "./selection.ts";

const kinds = ["object", "query", "flow", "app", "migration"];
const choice = (index) => ({ kind: "object", id: `00000000-0000-4000-8000-${String(index).padStart(12, "0")}` });
const choices = (count) => Array.from({ length: count }, (_, index) => choice(index));

test("release selection accepts 1, 32, 33 and 128 drafts and refuses 129 without truncation", () => {
  for (const count of [1, 32, 33, 128]) {
    const result = releaseDraftsSelection(releaseDraftsValue(choices(count)), kinds);
    assert.equal(result.error, undefined);
    assert.equal(result.drafts.length, count);
  }
  const tooMany = releaseDraftsSelection(releaseDraftsValue(choices(129)), kinds);
  assert.deepEqual(tooMany.drafts, []);
  assert.ok(tooMany.error);
});

test("release selection keeps long UUIDs, colons in ids, and deduplicates", () => {
  const long = { kind: "object", id: "550e8400-e29b-41d4-a716-446655440000:segment:12" };
  const parsed = releaseDraftsSelection(releaseDraftsValue([choice(1), long, choice(1)]), kinds);
  assert.equal(parsed.drafts.length, 2);
  assert.ok(parsed.drafts.some((item) => item.id === long.id));
  // Legacy comma form: the first colon separates the kind, the rest is the id.
  const legacy = releaseDraftsSelection("object:550e8400-e29b-41d4-a716-446655440000:segment", kinds);
  assert.equal(legacy.drafts[0]?.kind, "object");
  assert.equal(legacy.drafts[0]?.id, "550e8400-e29b-41d4-a716-446655440000:segment");
});

test("release selection refuses unknown kinds, empty ids and malformed payloads", () => {
  assert.ok(releaseDraftsSelection(releaseDraftsValue([{ kind: "widget", id: "x" }]), kinds).error);
  assert.ok(releaseDraftsSelection(releaseDraftsValue([{ kind: "object", id: "" }]), kinds).error);
  assert.ok(releaseDraftsSelection("{\"kind\":\"object\"", kinds).error);
  assert.ok(releaseDraftsSelection("not-json-at-all", kinds).error);
  assert.deepEqual(releaseDraftsSelection(undefined, kinds).drafts, []);
});

test("the command budget is the original host budget: 128 drafts x 512 bytes", () => {
  assert.equal(releaseCommandBytes, 65536);
  // A full 128-draft selection of normal UUIDs stays inside it.
  const body = JSON.stringify({ drafts: choices(128).map((item) => ({ ...item, kind: "object" })) });
  assert.ok(new TextEncoder().encode(body).length <= releaseCommandBytes);
});
