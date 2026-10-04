import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){try{return next(s,c)}catch(e){if(s.startsWith('./')&&!s.endsWith('.ts'))return next(`${s}.ts`,c);throw e;}}});
import assert from 'node:assert/strict';import test from 'node:test';
const {builderConditions}=await import('./collection-builder.ts');
const info={type:'sample.note',fields:[{name:'qty',type:'decimal'},{name:'whole',type:'integer'},{name:'text',type:'text'},{name:'state',type:'choice',choices:['ready','closed']}]},fields=info.fields.map(f=>f.name);
test('condition drafts preserve exact decimal and original vocabulary and reject incomplete, hidden, oversized or mistyped clauses',()=>{
 assert.deepEqual(builderConditions([{field:'qty',operator:'gte',arg:'0.1234567890123456789'}],fields,info),[{field:'qty',op:'>=',value:{literal:{kind:'decimal',value:'0.1234567890123456789'}}}]);
 assert.deepEqual(builderConditions([{field:'state',operator:'isNot',arg:'ready'},{field:'text',operator:'contains',arg:'literal'}],fields,info).map(c=>c.op),['!=','like']);
 for(const filter of [{field:'qty',operator:'gte',arg:''},{field:'whole',operator:'gte',arg:'1.5'},{field:'qty',operator:'gte',arg:2},{field:'state',operator:'contains',arg:'ready'},{field:'state',operator:'is',arg:'mock'},{field:'text',operator:'is',arg:'x'.repeat(4097)},{field:'hidden',operator:'is',arg:'x'}])assert.equal(builderConditions([filter],fields,info),undefined);
 assert.equal(builderConditions(Array.from({length:17},()=>({field:'text',operator:'is',arg:'a'})),fields,info),undefined);assert.equal(builderConditions([{field:'text',operator:'is',arg:'a'}],[],info),undefined);
});
