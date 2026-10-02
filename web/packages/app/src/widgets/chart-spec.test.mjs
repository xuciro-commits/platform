import {registerHooks} from "node:module";
import assert from "node:assert/strict";
import test from "node:test";
registerHooks({resolve(s,c,n){if(s==="@platform/ui")return {url:"data:text/javascript,export {}",shortCircuit:true};return n(s,c)}});
const {compileChartSpec,checkPieData}=await import("./chart-spec.ts");
test("declared pie variants compile native value labels and ring geometry without changing aggregate encodings",()=>{const base={object:"sample.note",mark:"arc",group:"bucket",measure:"count"},pie=compileChartSpec({...base,chartVariant:"pie"}),donut=compileChartSpec({...base,chartVariant:"donut"});assert.deepEqual(pie.mark,{type:"arc",donut:false,showValues:true});assert.deepEqual(donut.mark,{type:"arc",donut:true,showValues:true});assert.deepEqual(pie.encoding,donut.encoding);assert.equal(pie.encoding.theta.aggregate,"count");assert.equal(compileChartSpec(base).mark,"arc");});
test("chart marks compile through the existing grammar and pie shares reject negative or incompatible units",()=>{
 const line=compileChartSpec({object:"sample.note",mark:"line",group:"created:month",measure:"sum:qty"});assert.equal(line.mark,"line");assert.equal(line.encoding.x.type,"temporal");assert.equal(line.encoding.x.timeUnit,"month");assert.equal(line.encoding.y.aggregate,"sum");
 const pie=compileChartSpec({object:"sample.note",mark:"arc",group:"bucket",measure:"sum:qty"});assert.equal(pie.encoding.theta.field,"qty");assert.equal(pie.encoding.color.field,"bucket");assert.equal(pie.encoding.x,undefined);
 const data={columns:[{kind:"measure"}],rows:[{"sum:qty":2}]};assert.equal(checkPieData(pie,data),true);
 for(const value of [-1,Infinity,NaN,"2",null])assert.equal(checkPieData(pie,{...data,rows:[{"sum:qty":value}]}),false);
 assert.equal(checkPieData(pie,{...data,columns:[{kind:"measure",money:true}]}),false);
 assert.equal(checkPieData(compileChartSpec({object:"sample.note",mark:"arc",group:"bucket",measure:"avg:qty"}),data),false);
 assert.equal(compileChartSpec({object:"sample.note",group:"bucket"}).mark,"bar");
});
