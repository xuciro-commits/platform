import { expect, test } from 'vitest';
import { formatStructuredSource, parseStructuredSource, structuredIssues } from './structured-source';
import type { Api } from '@platform/kernel';

test('JSON and YAML preserve original values, absent fields and nested ordering',()=>{
  const value={name:'yes',empty:'',optional:undefined,zero:0,enabled:false,clear:null,rows:[{name:'first',advanced:{bindings:[]}}, {name:'second'}],date:'2026-10-18'};
  const expected=JSON.parse(JSON.stringify(value));
  for(const format of ['json','yaml'] as const) expect(parseStructuredSource(formatStructuredSource(value,format),format)).toEqual(expected);
});
test('source refuses ambiguous YAML and imprecise numeric data',()=>{
  for(const raw of ['a: 1\na: 2','a: &item 1\nb: *item','a: !custom 1','1: value','a: 1\n---\nb: 2','a: .inf','a: 9007199254740993','a: 0.1234567890123456789','- item']) expect(()=>parseStructuredSource(raw,'yaml'),raw).toThrow();
  for(const raw of ['{"a":1,"a":2}','{"a":9007199254740993}','{"a":0.1234567890123456789}','{} trailing']) expect(()=>parseStructuredSource(raw,'json'),raw).toThrow();
  expect(parseStructuredSource('value: 0.1\nname: "on"','yaml')).toEqual({value:0.1,name:'on'});
});
test('source constraints retain original field paths and do not strip fields',()=>{
  const fields:Api.FieldInfo[]=[{name:'name',title:'Name',type:'text',required:true},{name:'readonly',title:'State',type:'text',readOnly:true},{name:'rows',title:'Rows',type:'lines',fields:[{name:'amount',title:'Amount',type:'integer',constraints:{min:0}}]},{name:'advanced',title:'Advanced',type:'json'}];
  const value={name:'',readonly:'x',unknown:0,rows:[{amount:-1}],advanced:{anything:[false,null,0]}};
  const before=JSON.stringify(value),issues=structuredIssues(value,fields);
  expect(issues.map(issue=>issue.path.join('.'))).toEqual(expect.arrayContaining(['name','readonly','unknown','rows.0.amount']));
  expect(issues.some(issue=>issue.path[0]==='advanced')).toBe(false);
  expect(JSON.stringify(value)).toBe(before);
});
