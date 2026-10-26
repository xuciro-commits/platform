// Build the trusted browser evaluator from the identical Go rule source.
import {execFileSync} from "node:child_process";
import {copyFileSync,mkdirSync,renameSync,rmSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {join} from "node:path";
const root=fileURLToPath(new URL("../",import.meta.url)),dir=join(root,"web/public/rules");
mkdirSync(dir,{recursive:true});
const wasmTemp=join(dir,`engine-${process.pid}.wasm.tmp`),runtimeTemp=join(dir,`runtime-${process.pid}.js.tmp`);
const go=process.env.GO??"go",goroot=execFileSync(go,["env","GOROOT"],{encoding:"utf8"}).trim();
execFileSync(go,["build","-trimpath","-o",wasmTemp,"./cmd/rule-browser"],{cwd:join(root,"capabilities/server"),env:{...process.env,GOOS:"js",GOARCH:"wasm"},stdio:"inherit"});
try{copyFileSync(join(goroot,"lib/wasm/wasm_exec.js"),runtimeTemp);renameSync(wasmTemp,join(dir,"engine.wasm"));renameSync(runtimeTemp,join(dir,"wasm_exec.js"));}finally{rmSync(wasmTemp,{force:true});rmSync(runtimeTemp,{force:true});}
