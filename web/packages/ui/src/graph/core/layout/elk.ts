/// <reference path="./worker-url.d.ts" />
import type { ELK, ElkNode } from "elkjs/lib/elk-api";
import workerUrl from "elkjs/lib/elk-worker.min.js?url";
let engine: Promise<ELK> | undefined;
function getEngine(): Promise<ELK> {
  // Browser uses the official standalone worker; SSR and owner tests use the same bundled algorithms.
  engine ??= (typeof Worker === "undefined"
    ? import("elkjs/lib/elk.bundled.js").then(({ default: Constructor }) => new Constructor())
    : import("elkjs/lib/elk-api.js").then(({ default: Constructor }) => new Constructor({ workerUrl })))
    .catch((error) => { engine = undefined; throw error; });
  return engine;
}
export const registeredLayoutAlgorithms = async () => (await getEngine()).knownLayoutAlgorithms();
export const runElk = async (graph: ElkNode) => (await getEngine()).layout(graph);
