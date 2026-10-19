import {useState} from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,test,vi} from 'vitest';
import {z} from 'zod';
import type {Api} from '@platform/kernel';
import {StructuredEditor} from './StructuredEditor';
import {EntityForm,RecordForm} from './EntityForm';
import {InputDraftProvider,useInputDrafts} from '../fields/draft';
import {defineEntity} from '../fields/entity';
import {text} from '../fields/types';
import type {StructuredValue} from '../fields/structured-source';

afterEach(cleanup);
function DraftState() {
  const drafts=useInputDrafts();
  return <><output data-testid="buffer">{JSON.stringify({dirty:drafts?.dirty,invalid:drafts?.invalid})}</output><button disabled={drafts?.invalid}>Save draft</button></>;
}
function Editor({initial,onChange=()=>{}}:{initial:StructuredValue;onChange?:(value:StructuredValue)=>void}) {
  const [value,setValue]=useState(initial);
  return <InputDraftProvider scope="test"><StructuredEditor value={value} scope="document" onChange={next=>{setValue(next);onChange(next);}}><input aria-label="Name" value={String(value.name??'')} onChange={event=>setValue({...value,name:event.target.value})}/></StructuredEditor><output data-testid="value">{JSON.stringify(value)}</output><DraftState/></InputDraftProvider>;
}
const source=()=>screen.getByRole('textbox',{name:'Configuration source'}) as HTMLTextAreaElement;
const buffer=()=>JSON.parse(screen.getByTestId('buffer').textContent!);

test('visual and source edits share one draft while preserving JSON values through YAML',()=>{
  const changes=vi.fn(),initial={name:'Original',zero:0,enabled:false,clear:null,empty:[],advanced:{nodes:[{id:'b'},{id:'a'}]}};
  render(<Editor initial={initial} onChange={changes}/>);
  fireEvent.change(screen.getByRole('textbox',{name:'Name'}),{target:{value:'Visual edit'}});
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  expect(JSON.parse(source().value)).toEqual({...initial,name:'Visual edit'});
  const next={...initial,name:'Source edit'};
  fireEvent.change(source(),{target:{value:JSON.stringify(next)}});
  expect(buffer()).toEqual({dirty:true,invalid:true});
  expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button',{name:'Apply source'}));
  expect(changes).toHaveBeenCalledExactlyOnceWith(next);
  expect(buffer()).toEqual({dirty:false,invalid:false});
  fireEvent.change(screen.getByRole('combobox',{name:'Source format'}),{target:{value:'yaml'}});
  expect(source().value).toContain('enabled: false');
  expect(buffer()).toEqual({dirty:false,invalid:false});
  fireEvent.change(screen.getByRole('combobox',{name:'Source format'}),{target:{value:'json'}});
  expect(JSON.parse(source().value)).toEqual(next);
  fireEvent.click(screen.getByRole('button',{name:'Visual'}));
  expect((screen.getByRole('textbox',{name:'Name'}) as HTMLInputElement).value).toBe('Source edit');
});

test('invalid raw input remains visible and prevents mode, format and submission changes',()=>{
  const changes=vi.fn();render(<Editor initial={{name:'Saved'}} onChange={changes}/>);
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  fireEvent.change(source(),{target:{value:'{"name":'}});
  fireEvent.click(screen.getByRole('button',{name:'Visual'}));
  expect(source().value).toBe('{"name":');
  expect((screen.getByRole('combobox',{name:'Source format'}) as HTMLSelectElement).disabled).toBe(true);
  expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true);
  expect(JSON.parse(screen.getByTestId('value').textContent!)).toEqual({name:'Saved'});
  expect(changes).not.toHaveBeenCalled();
  fireEvent.change(source(),{target:{value:'{"name":"Corrected"}'}});
  fireEvent.click(screen.getByRole('button',{name:'Visual'}));
  expect((screen.getByRole('textbox',{name:'Name'}) as HTMLInputElement).value).toBe('Corrected');
  expect(buffer()).toEqual({dirty:false,invalid:false});
});

test('unchanged formatting does not dirty the draft or call its update function',()=>{
  const changes=vi.fn();render(<Editor initial={{count:0,active:false}} onChange={changes}/>);
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  fireEvent.click(screen.getByRole('button',{name:'Format'}));
  for(const format of ['yaml','json'])fireEvent.change(screen.getByRole('combobox',{name:'Source format'}),{target:{value:format}});
  expect(buffer()).toEqual({dirty:false,invalid:false});
  expect(changes).not.toHaveBeenCalled();
});

test('late business trial results cannot describe a newer draft or operation context',async()=>{
  let resolve!:(value:Api.Simulation)=>void;
  const trial=vi.fn(()=>new Promise<Api.Simulation>(done=>{resolve=done;}));
  const view=render(<InputDraftProvider><StructuredEditor value={{name:'Earlier'}} scope="same" context="order:one@1" onTrial={trial}/></InputDraftProvider>);
  fireEvent.click(screen.getByRole('button',{name:'Try operation'}));
  expect(trial).toHaveBeenCalledExactlyOnceWith({name:'Earlier'});
  view.rerender(<InputDraftProvider><StructuredEditor value={{name:'Newer'}} scope="same" context="order:two@2" onTrial={trial}/></InputDraftProvider>);
  await act(async()=>{resolve({accepted:true,changes:[]});});
  expect(screen.queryByText('Business trial passed. Nothing was saved; submission checks again.')).toBeNull();
  expect(screen.getByRole('button',{name:'Try operation'})).toBeTruthy();
});

test('a revision change invalidates an in-flight trial even when values and target are unchanged',async()=>{
  let resolve!:(value:Api.Simulation)=>void;
  const trial=()=>new Promise<Api.Simulation>(done=>{resolve=done;}),value={name:'Same'};
  const editor=(revision:number)=><InputDraftProvider><StructuredEditor value={value} scope="same" context="order:one" expectedRevision={revision} onTrial={trial}/></InputDraftProvider>;
  const view=render(editor(1));
  fireEvent.click(screen.getByRole('button',{name:'Try operation'}));
  view.rerender(editor(2));
  await act(async()=>{resolve({accepted:true,changes:[]});});
  expect(screen.queryByText('Business trial passed. Nothing was saved; submission checks again.')).toBeNull();
});

test('RecordForm submits complete declared source input through its original action and retains it on refusal',async()=>{
  const entity=defineEntity<{name:string}>({name:'Draft',primary:'name',fields:{name:text({label:'Name',required:true})}});
  const fields:Api.FieldInfo[]=[{name:'name',title:'Name',type:'text',required:true},{name:'advanced',title:'Advanced',type:'json'},{name:'count',title:'Count',type:'integer'},{name:'enabled',title:'Enabled',type:'boolean'},{name:'clear',title:'Clear',type:'text'},{name:'rows',title:'Rows',type:'json'}];
  const submit=vi.fn(()=>false);
  render(<RecordForm entity={entity} keys={['name']} sourceFields={fields} defaultValues={{name:'Original'}} onSubmit={submit}/>);
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  const next={name:'Source input',advanced:{binding:{reference:'kept'},order:['b','a']},count:0,enabled:false,clear:null,rows:[]};
  fireEvent.change(source(),{target:{value:JSON.stringify(next)}});
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Apply source'}));});
  expect(submit).not.toHaveBeenCalled();
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Save'}));});
  expect(submit).toHaveBeenCalledExactlyOnceWith(next);
  expect(JSON.parse(source().value)).toEqual(next);
  expect(screen.getByText('The request was not confirmed. Your input is retained.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Visual'}));
  expect((screen.getByRole('textbox',{name:'Name *'}) as HTMLInputElement).value).toBe('Source input');
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  expect(JSON.parse(source().value)).toEqual(next);
});

test('EntityForm source excludes immutable identity while its original schema and submission retain it',async()=>{
  const submit=vi.fn(()=>false);
  render(<EntityForm schema={z.object({id:z.string().min(1),name:z.string().min(1)})} fields={[{name:'id',label:'ID',required:true,readOnly:true},{name:'name',label:'Name',required:true}]} defaultValues={{id:'original-id',name:'Original'}} onSubmit={submit}/>);
  fireEvent.click(screen.getByRole('button',{name:'Source editor'}));
  expect(JSON.parse(source().value)).toEqual({name:'Original'});
  fireEvent.change(source(),{target:{value:JSON.stringify({id:'forged-id',name:'Changed'})}});
  fireEvent.click(screen.getByRole('button',{name:'Apply source'}));
  expect(screen.getByText('id: This field is not declared.')).toBeTruthy();
  expect(submit).not.toHaveBeenCalled();
  fireEvent.change(source(),{target:{value:JSON.stringify({name:'Changed'})}});
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Apply source'}));});
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Save'}));});
  expect(submit).toHaveBeenCalledExactlyOnceWith({id:'original-id',name:'Changed'},expect.anything());
  expect(JSON.parse(source().value)).toEqual({name:'Changed'});
});

test('read-only source can be copied without exposing update or trial actions',async()=>{
  const copy=vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator,'clipboard',{value:{writeText:copy},configurable:true});
  const value={name:'Visible',reference:'allowed-id'};
  render(<InputDraftProvider><StructuredEditor value={value} scope="read" readOnly/></InputDraftProvider>);
  expect(source().readOnly).toBe(true);
  expect(screen.queryByRole('button',{name:'Apply source'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Check inputs'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Try operation'})).toBeNull();
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Copy'}));});
  expect(copy).toHaveBeenCalledExactlyOnceWith(JSON.stringify(value,null,2));
});

test('empty drafts show declared field guidance and copying an example never initializes real input',async()=>{
  const copy=vi.fn().mockResolvedValue(undefined),change=vi.fn();
  Object.defineProperty(navigator,'clipboard',{value:{writeText:copy},configurable:true});
  const fields:Api.FieldInfo[]=[
    {name:'name',title:'Application name',type:'text',required:true,example:'workshop',help:'Choose a unique name.'},
    {name:'state',title:'State',type:'choice',choices:['draft','ready']},
    {name:'owner',title:'Owner',type:'reference',ref:'core.person'},
    {name:'rows',title:'Lines',type:'lines',fields:[{name:'amount',title:'Amount',type:'integer',constraints:{min:1}}]},
    {name:'private',title:'Not writable',type:'text',readOnly:true},
  ];
  render(<InputDraftProvider><StructuredEditor value={{}} scope="empty" fields={fields} onChange={change}/><DraftState/></InputDraftProvider>);
  expect(screen.getByText('Fields and examples').closest('details')?.open).toBe(true);
  expect(screen.getByText('Choose a unique name.')).toBeTruthy();
  expect(screen.getByText('text · Required')).toBeTruthy();
  fireEvent.change(screen.getByRole('combobox',{name:'Field reference'}),{target:{value:'state'}});
  expect(screen.getByText('Allowed values: draft, ready')).toBeTruthy();
  fireEvent.change(screen.getByRole('combobox',{name:'Field reference'}),{target:{value:'rows.[].amount'}});
  expect(screen.getByText('rows.[].amount')).toBeTruthy();
  expect(screen.queryByText('Not writable')).toBeNull();
  expect(JSON.parse(source().value)).toEqual({});
  fireEvent.click(screen.getByText('Example document'));
  await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Copy example'}));});
  expect(JSON.parse(copy.mock.calls[0]![0])).toEqual({name:'workshop',state:'draft',owner:'<select a permitted record>',rows:[{amount:1}]});
  expect(JSON.parse(source().value)).toEqual({});
  expect(change).not.toHaveBeenCalled();
  expect(buffer()).toEqual({dirty:false,invalid:false});
});

test('reference helper inserts the selected ID into pending source without replacing advanced configuration',()=>{
  const change=vi.fn(),initial={owner:'person-old',advanced:{nodes:[{id:'b'},{id:'a'}]},empty:[]};
  const fields:Api.FieldInfo[]=[{name:'owner',title:'Owner',type:'reference',ref:'core.person'},{name:'advanced',title:'Advanced',type:'json'},{name:'empty',title:'Empty',type:'json'}];
  render(<InputDraftProvider><StructuredEditor value={initial} scope="references" fields={fields} onChange={change} fieldInput={(field,value,onChange)=>field.ref?<select aria-label="Choose permitted owner" value={String(value??'')} onChange={event=>onChange(event.target.value)}><option value="person-old">Former owner</option><option value="person-permitted">Permitted person</option></select>:null}/><DraftState/></InputDraftProvider>);
  fireEvent.click(screen.getByText('Fields and examples'));
  fireEvent.change(screen.getByRole('combobox',{name:'Choose permitted owner'}),{target:{value:'person-permitted'}});
  expect(JSON.parse(source().value)).toEqual({...initial,owner:'person-permitted'});
  expect(change).not.toHaveBeenCalled();
  expect(buffer()).toEqual({dirty:true,invalid:true});
  fireEvent.click(screen.getByRole('button',{name:'Apply source'}));
  expect(change).toHaveBeenCalledExactlyOnceWith({...initial,owner:'person-permitted'});
  fireEvent.change(source(),{target:{value:'{"owner":'}});
  expect(screen.getByRole('combobox',{name:'Choose permitted owner'}).closest('fieldset')?.disabled).toBe(true);
  expect(source().value).toBe('{"owner":');
});
