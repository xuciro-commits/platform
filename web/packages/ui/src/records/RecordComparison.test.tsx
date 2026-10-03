import {afterEach,expect,test} from "vitest";
import {cleanup,render,screen,within} from "@testing-library/react";
import {RecordComparison} from "./RecordComparison";
import {setLanguage} from "../i18n";
import type {EntityInfo,EntityRecord} from "./Records";
afterEach(()=>{cleanup();setLanguage("en");});
const stamp={by:"test",at:"2026-10-03T00:00:00Z"};
const info={type:"sample.note",title:"Notes",fields:[
 {name:"name",title:"Title",type:"text"},{name:"decimal",title:"Decimal",type:"decimal"},
 {name:"integer",title:"Integer",type:"integer"},{name:"money",title:"Money",type:"money"},
 {name:"boolean",title:"Boolean",type:"boolean"},{name:"text",title:"Text",type:"text"},
 {name:"date",title:"Date",type:"date"},{name:"datetime",title:"Time",type:"datetime"},
 {name:"choice",title:"Choice",type:"choice",choices:["open"],choiceTitles:["Open"]},
 {name:"other",title:"Decimal",type:"decimal"},
]} as EntityInfo;
const record=(id:string,values:Record<string,unknown>={}):EntityRecord=>({id,revision:1,created:stamp,changed:stamp,name:"<b>Same title</b>",...values});
const props={info,labelField:"name",fields:["decimal"]};
test("duplicate literal titles retain independent stable identities and original decimal formatting never decides differences",()=>{
 const records=[record("A",{decimal:1.001}),record("B",{decimal:1.002})];
 const {container}=render(<RecordComparison {...props} records={records}/>);
 expect(screen.getByRole("table",{name:"Record comparison"})).toBeTruthy();
 expect(screen.getAllByText("<b>Same title</b>")).toHaveLength(2);expect(container.querySelector("b")).toBeNull();
 expect(screen.getByRole("columnheader",{name:"<b>Same title</b> A"})).toBeTruthy();
 expect(screen.getByRole("columnheader",{name:"<b>Same title</b> B"})).toBeTruthy();
 expect(screen.getByRole("rowheader",{name:"Decimal Different"})).toBeTruthy();expect(screen.getAllByText("1.00")).toHaveLength(2);
 expect(screen.queryByRole("button")).toBeNull();
});
test("money compares original amount and currency and rejects malformed or unsafe minor units",()=>{
 const view=render(<RecordComparison {...props} fields={["money"]} records={[record("A",{money:{amount:100,currency:"USD"}}),record("B",{money:{amount:100,currency:"EUR"}})]}/>);
 expect(screen.getByRole("rowheader",{name:"Money Different"})).toBeTruthy();
 view.rerender(<RecordComparison {...props} fields={["money"]} records={[record("A",{money:{amount:100,currency:"USD"}}),record("B",{money:{currency:"USD",amount:100}})]}/>);
 expect(screen.getByRole("rowheader",{name:"Money Same"})).toBeTruthy();
 for(const money of [{amount:1.5,currency:"USD"},{amount:1,currency:"usd"},{amount:1,currency:"€"},{amount:Number.MAX_SAFE_INTEGER+1,currency:"USD"},"1 USD"]){
  view.rerender(<RecordComparison {...props} fields={["money"]} records={[record("A",{money}),record("B",{money})]}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("table")).toBeNull();
 }
});
test("missing values agree while zero, false and empty text remain original distinct values",()=>{
 const records=[record("A",{integer:null,boolean:null,text:null}),record("B",{integer:0,boolean:false,text:""})];
 const view=render(<RecordComparison {...props} fields={["integer","boolean","text"]} records={records}/>);
 for(const title of ["Integer","Boolean","Text"])expect(screen.getByRole("rowheader",{name:`${title} Different`})).toBeTruthy();
 expect(screen.getByText("0")).toBeTruthy();expect(screen.getByText("No")).toBeTruthy();
 view.rerender(<RecordComparison {...props} fields={["integer","boolean","text"]} records={[records[0]!,record("B")]}/>);
 for(const title of ["Integer","Boolean","Text"])expect(screen.getByRole("rowheader",{name:`${title} Same`})).toBeTruthy();
 view.rerender(<RecordComparison {...props} fields={["integer","decimal"]} records={[record("A",{integer:0,decimal:-0}),record("B",{integer:-0,decimal:0})]}/>);
 for(const title of ["Integer","Decimal"])expect(screen.getByRole("rowheader",{name:`${title} Same`})).toBeTruthy();
});
test("datetime compares instants at nanosecond precision across UTC offsets and civil year boundaries",()=>{
 const view=render(<RecordComparison {...props} fields={["datetime"]} records={[record("A",{datetime:"2028-02-29T08:00:00.123456789+08:00"}),record("B",{datetime:"2028-02-29T00:00:00.123456789Z"})]}/>);
 expect(screen.getByRole("rowheader",{name:"Time Same"})).toBeTruthy();
 view.rerender(<RecordComparison {...props} fields={["datetime"]} records={[record("A",{datetime:"2028-02-29T00:00:00.123456789Z"}),record("B",{datetime:"2028-02-29T00:00:00.123456788Z"})]}/>);
 expect(screen.getByRole("rowheader",{name:"Time Different"})).toBeTruthy();expect(screen.getAllByText("2028-02-29 00:00")).toHaveLength(2);
 for(const [a,b] of [["0001-01-01T00:00:00Z","0001-01-01T01:00:00+01:00"],["2000-12-31T23:30:00Z","2001-01-01T00:30:00+01:00"],["9999-12-31T23:59:59.1Z","9999-12-31T23:59:59.100000000+00:00"]]){
  view.rerender(<RecordComparison {...props} fields={["datetime"]} records={[record("A",{datetime:a}),record("B",{datetime:b})]}/>);expect(screen.getByRole("rowheader",{name:"Time Same"})).toBeTruthy();
 }
});
test("malformed scalars refuse comparison without false equality or data coercion",()=>{
 const invalid:[string,unknown][]=[["boolean","false"],["text",false],["integer","0"],["integer",1.1],["integer",Number.MAX_SAFE_INTEGER+1],["decimal",Infinity],["decimal",NaN],["date","2026-02-29"],["date",""] ,["datetime","2028-02-29T00:00:00-00:00"],["datetime","2028-02-29T00:00:00.1234567890Z"]];
 const view=render(<RecordComparison {...props} records={[]}/>);
 for(const [field,value] of invalid){view.rerender(<RecordComparison {...props} fields={[field]} records={[record("A",{[field]:value}),record("B",{[field]:value})]}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByText("Same")).toBeNull();expect(screen.queryByRole("table")).toBeNull();}
});
test("comparison requires two to four complete unique live identities and retains over-budget selections",()=>{
 const rows=Array.from({length:5},(_,i)=>record(`R${i}`,{decimal:i})),view=render(<RecordComparison {...props} records={rows.slice(0,1)}/>);
 expect(screen.getByRole("status").textContent).toBe("Select two to four records to compare.");
 view.rerender(<RecordComparison {...props} records={rows.slice(0,4)}/>);expect(screen.getAllByRole("columnheader")).toHaveLength(5);
 view.rerender(<RecordComparison {...props} records={rows}/>);expect(screen.getByRole("alert").textContent).toContain("All selected records are retained");expect(rows).toHaveLength(5);expect(screen.queryByRole("table")).toBeNull();
 for(const records of [[rows[0]!,rows[0]!],[rows[0]!,{...rows[1]!,archived:true}],[rows[0]!,record("",{decimal:1})]]){
  view.rerender(<RecordComparison {...props} records={records}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("table")).toBeNull();
 }
});
test("private titles and fields are refused, field identities survive duplicate display names, and properties stay within budget",()=>{
 const records=[record("A",{decimal:1,other:2,secret:"private"}),record("B",{decimal:1,other:3,secret:"private"})],view=render(<RecordComparison {...props} records={records} fields={["decimal","other"]}/>);
 expect(screen.getByRole("rowheader",{name:"Decimal Same"})).toBeTruthy();expect(screen.getByRole("rowheader",{name:"Decimal Different"})).toBeTruthy();
 view.rerender(<RecordComparison {...props} records={records} fields={["secret"]}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByText("private")).toBeNull();
 view.rerender(<RecordComparison {...props} records={records} info={{...info,fields:info.fields.filter(f=>f.name!=="name")}}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByText("<b>Same title</b>")).toBeNull();
 view.rerender(<RecordComparison {...props} records={records} fields={["decimal","decimal"]}/>);expect(screen.getByRole("alert")).toBeTruthy();
 const many=Array.from({length:65},(_,i)=>({name:`f${i}`,title:`Field ${i}`,type:"text" as const})),manyInfo={...info,fields:many};
 view.rerender(<RecordComparison info={manyInfo} fields={many.map(f=>f.name)} records={records} labelField="id"/>);expect(screen.getByRole("alert")).toBeTruthy();
 view.rerender(<RecordComparison info={manyInfo} fields={many.slice(0,64).map(f=>f.name)} records={records} labelField="id"/>);expect(screen.getAllByRole("rowheader")).toHaveLength(64);
});
test("choice display uses original formatter and comparison labels translate independently of business data",()=>{
 setLanguage("zh-CN");render(<RecordComparison {...props} records={[record("A",{choice:"open"}),record("B",{choice:"open"})]} fields={["choice"]}/>);
 const table=screen.getByRole("table",{name:"记录对比"});expect(within(table).getByRole("rowheader",{name:"Choice 相同"})).toBeTruthy();expect(within(table).getAllByText("Open")).toHaveLength(2);expect(within(table).getAllByText("<b>Same title</b>")).toHaveLength(2);
});
