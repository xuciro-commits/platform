// Run the same pure authoring compiler against original, live host metadata.
import {registerHooks} from 'node:module';import {readFileSync} from 'node:fs';
const manifest=JSON.parse(readFileSync(new URL('../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(s,c,next){if(s==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}}});
const {compileWorkshopModule}=await import('../packages/build/src/workshop/module-import/compile.ts');
process.stdin.setEncoding('utf8');let input='';for await(const chunk of process.stdin)input+=chunk;
const q=JSON.parse(input),report=compileWorkshopModule(q.source,q.page,q.bindings,q.target);process.stdout.write(JSON.stringify(report));
