import test from "node:test";
import assert from "node:assert/strict";
import { readOnlyRequest } from "./capture-ui.mjs";

test("capture permits bounded read APIs, including their POST query forms", () => {
  for (const pathname of ["/v1/me", "/v1/definitions", "/v1/records/build.item"]) {
    assert.equal(readOnlyRequest("GET", pathname), true);
  }
  for (const pathname of ["/v1/page-model/read", "/v1/releases/preview", "/v1/observations/build/temperature/read", "/v1/records/build.item/query", "/v1/aggregates/build.item/query", "/v1/link-types/build/items/1/forward/R1"]) {
    assert.equal(readOnlyRequest("POST", pathname), true);
  }
});

test("capture cannot submit decisions, install packages, change credentials or evaluate observations", () => {
  for (const pathname of ["/v1/submissions", "/v1/packages/import", "/v1/packages/x/cleanup", "/v1/credentials/seal", "/v1/host/tenants/t/composition", "/v1/observations/build/temperature/evaluate", "/v1/capabilities/invoke", "/v1/unknown"]) {
    assert.equal(readOnlyRequest("POST", pathname), false, pathname);
  }
  for (const method of ["PUT", "PATCH", "DELETE"]) assert.equal(readOnlyRequest(method, "/v1/records/build.item/query"), false);
});
