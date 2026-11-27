import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
const root=new URL("..",import.meta.url).pathname;
const lock=JSON.parse(readFileSync(join(root,"package-fixtures.lock.json"),"utf8"));
const cache=join(root,".build/package-inputs");
const dirs=existsSync(cache)?readdirSync(cache).map(x=>join(cache,x,"source")):[];
let source=dirs.find(x=>{try{return execFileSync("git",["-C",x,"rev-parse","HEAD"],{encoding:"utf8",stdio:["ignore","pipe","ignore"]}).trim()===lock.revision}catch{return false}});
if(!source){
 const fixture=execFileSync("node",[join(root,"scripts/package-fixtures.mjs"),"--materialize"],{cwd:root,encoding:"utf8",timeout:55000}).trim().split("\n").at(-1);
 source=join(fixture,"source");
}
if(execFileSync("git",["-C",source,"rev-parse","HEAD"],{encoding:"utf8"}).trim()!==lock.revision)throw Error("Package vocabulary revision mismatch");
const manifests=execFileSync("git",["-C",source,"ls-tree","-r","--name-only",lock.revision,"packages"],{encoding:"utf8"}).trim().split("\n").filter(x=>x.endsWith("/package.yaml"));
const names=new Set();
for(const p of manifests){const raw=execFileSync("git",["-C",source,"show",lock.revision+":"+p],{encoding:"utf8"});for(const match of raw.matchAll(/^(?:id|namespace):\s*["']?([\w.-]+)["']?\s*(?:#.*)?$/gm))names.add(match[1]);}
if(!names.size)throw Error("Empty package vocabulary");
const tokens=[...names].sort((a,b)=>b.length-a.length).map(x=>x.replace(/[.*+?^${}()|[\]\\]/g,"\\$&"));
const regex=new RegExp("(?<![A-Za-z0-9_-])(?:"+tokens.join("|")+")(?![A-Za-z0-9_-])","g");
const paths=execFileSync("git",["ls-files","capabilities/server","frontend/src"],{cwd:root,encoding:"utf8"}).trim().split("\n").filter(p=>/\.(go|ts|tsx|js|jsx)$/.test(p)&&!/(?:_test\.go|\.(?:test|spec)\.[^.]+)$/.test(p)&&!/(?:^|\/)(?:gen|generated|fixtures|testdata|e2e|__tests__)(?:\/|$)/.test(p));
let hits=[];
for(const path of paths){const text=readFileSync(join(root,path),"utf8");for(const [i,line] of text.split("\n").entries()){if(path.endsWith("PackageCard.tsx")&&/^[\s]*[\w-]+:.*["']/.test(line)&&text.slice(0,text.indexOf(line)).lastIndexOf("listingLabels")>text.slice(0,text.indexOf(line)).lastIndexOf("};"))continue;const found=[...new Set([...line.matchAll(regex)].map(x=>x[0]))];if(found.length)hits.push({path,line:i+1,words:found});}}
console.log("purity: "+names.size+" locked identifiers, "+hits.length+" candidate lines");
const report=join(root,".build/purity/purity-candidates.json");mkdirSync(join(root,".build/purity"),{recursive:true});writeFileSync(report,JSON.stringify(hits,null,2));
for(const hit of hits.filter(h=>/PackageCard\.tsx|PolicyDialog\.tsx|app_views\.go|permissions\.go/.test(h.path)))console.log(hit.path+":"+hit.line+" "+hit.words.join(", "));
console.log("purity report: "+report);
if(process.argv.includes("--strict")&&hits.length)process.exitCode=1;
else if(hits.length)console.log("purity: report-only until cross-wave cleanup and domain-neutral collisions are resolved");
