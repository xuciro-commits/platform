import {webcrypto} from "node:crypto";
import {readFileSync} from "node:fs";
globalThis.crypto??=webcrypto;
await import(new URL("../web/public/rules/wasm_exec.js",import.meta.url));
const go=new globalThis.Go();
const {instance}=await WebAssembly.instantiate(readFileSync(new URL("../web/public/rules/engine.wasm",import.meta.url)),go.importObject);
void go.run(instance);
