import type * as PageUI from "../pageui/types";
import type {Api} from '@platform/kernel';
import {Button,DataTable,FlowSteps,t,type ColumnDef,type EntityRecord,type FlowStepNode,type FlowStepEdge,type Tone} from '@platform/ui';
import {useMemo} from 'react';

type Row=Record<string,unknown>;
const text=(value:unknown)=>value===undefined||value===null?'—':typeof value==='boolean'?t(value?'Yes':'No'):String(value);
const role=(row:Row,name:string)=>row[`$view.${name}`];
const title=(row:Row)=>text(role(row,'label')??row.id);
const tone=(config:PageUI.PageComposite,value:unknown):Tone|undefined=>config.tones?.[String(value)] as Tone|undefined;

/** Presentation only: inputs are the host's authorized expression results.
 * Columns retain typed identities; equal labels never merge distinct records. */
export function ModelMatrixRenderer({data,config,enabled,onSelect,selected}:{data?:Api.PageCompositeResult;config?:PageUI.PageComposite;enabled?:boolean;onSelect:(row?:EntityRecord)=>void;selected?:EntityRecord}){
 const rows=data?.sources.rows?.records??[],columns=data?.sources.columns?.records??[];
 const cells=useMemo(()=>new Map(data?.cells?.map(c=>[JSON.stringify([c.row,c.column]),c])),[data]);
 if(!data||!config)return <p role="status">{t('Configure the composite model bindings.')}</p>;
 const defs:ColumnDef<Row,unknown>[]=[{id:'$row',header:t('Row'),accessorFn:title,meta:{width:160}},...columns.map(column=>({id:`column:${column.id}`,header:title(column),accessorFn:(row:Row)=>cells.get(JSON.stringify([String(row.id),String(column.id)]))?.value,cell:({row}:{row:{original:Row}})=>{const cell=cells.get(JSON.stringify([String(row.original.id),String(column.id)]));return <span className="font-mono tabular-nums" data-tone={cell?tone(config,cell.value):undefined} style={cell&&tone(config,cell.value)?{color:`var(--tone-${tone(config,cell.value)})`}:undefined} title={cell?.kind}>{cell?text(cell.value):t('Unavailable')}</span>;},meta:{width:120}}))];
 return <DataTable data={rows} columns={defs} getRowId={row=>String(row.id)} selectedId={selected?.id} onRowClick={enabled===false?undefined:row=>onSelect(row as EntityRecord)} height={Math.min(480,Math.max(140,(rows.length+2)*28))} empty={t('No matching records.')}/>;
}

export function LayeredGraphRenderer({data,config,label,enabled,onSelect,onOpen,selected,storeKey}:{data?:Api.PageCompositeResult;config?:PageUI.PageComposite;label:string;enabled?:boolean;onSelect:(row?:EntityRecord)=>void;onOpen?:(type:string,row:EntityRecord)=>void;selected?:EntityRecord;storeKey:string}){
 if(!data||!config)return <p role="status">{t('Configure the composite model bindings.')}</p>;
 const raw=data.sources.nodes?.records??[],overlays=data.sources.overlay?.records??[],layers=data.sources.layers?.records??[],links=data.sources.edges?.records??[];
 const ordered=[...raw].sort((a,b)=>Number(role(a,'order')??0)-Number(role(b,'order')??0));
 const nodes:FlowStepNode[]=ordered.map(row=>{
  const patches=overlays.filter(o=>role(o,'node')===row.id),status=patches.map(o=>role(o,'state')).filter(v=>v!==undefined).at(-1)??role(row,'state');
  return {id:String(row.id),label:title(row),detail:[role(row,'detail'),...patches.map(o=>role(o,'detail')),status].filter(v=>v!==undefined&&v!==null&&v!=='').map(text).join(' · '),lane:role(row,'layer') as string|undefined,tone:tone(config,status),current:role(row,'current')===true||patches.some(o=>role(o,'current')===true),notation:role(row,'notation') as FlowStepNode['notation']};
 });
 const ids=new Set(nodes.map(n=>n.id));
 const edges:FlowStepEdge[]=links.filter(row=>ids.has(String(role(row,'from')))&&ids.has(String(role(row,'to')))).map(row=>({id:String(row.id),from:String(role(row,'from')),to:String(role(row,'to')),label:role(row,'label')===undefined?undefined:text(role(row,'label')),tone:tone(config,role(row,'state')),dashed:role(row,'dashed')===true}));
 const lanes=layers.map(row=>({id:String(row.id),title:title(row)}));
 const select=(node:FlowStepNode)=>{if(enabled!==false)onSelect(raw.find(r=>r.id===node.id) as EntityRecord|undefined);};
 const open=(node:FlowStepNode)=>{const row=raw.find(r=>r.id===node.id);if(row&&enabled!==false)onOpen?.(data.sources.nodes!.object,row as EntityRecord);};
 const focused=nodes.find(node=>node.id===selected?.id);
 const fields:ColumnDef<FlowStepNode,unknown>[]=[{accessorKey:'label',header:t('Node')},{id:'layer',header:t('Layer'),accessorFn:node=>lanes.find(l=>l.id===node.lane)?.title??node.lane??''},{accessorKey:'detail',header:t('Details')}];
 return <div className="grid min-w-0 grid-cols-1 gap-2"><div className="min-w-0 hidden md:block"><FlowSteps nodes={nodes} edges={edges} lanes={lanes} direction={config.direction??'right'} selected={selected?.id} height={Math.min(640,Math.max(360,lanes.length*160))} label={label} onOpen={select} onActivate={open} storeKey={storeKey}/></div><div className="min-w-0 overflow-x-auto md:hidden"><DataTable data={nodes} columns={fields} getRowId={row=>row.id} onRowClick={select} height={Math.min(480,Math.max(160,(nodes.length+2)*28))}/></div><p className="text-xs text-muted">{t('Select a node to share its record. Open it for the original details.')}</p>{focused&&selected&&<div className="grid min-w-0 grid-cols-1 gap-1 rounded border border-border p-2 text-xs"><strong>{focused.label}</strong>{focused.detail&&<p className="break-words" data-tone={focused.tone} style={focused.tone?{color:`var(--tone-${focused.tone})`}:undefined}>{focused.detail}</p>}<Button size="sm" className="justify-self-start" disabled={enabled===false||!onOpen} onClick={()=>onOpen?.(data.sources.nodes!.object,selected)}>{t('Open record')}</Button></div>}</div>;
}
