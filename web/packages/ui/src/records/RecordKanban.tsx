import {useState,type ReactNode} from "react";
import {Button} from "../primitives/button";
import {Select} from "../primitives/input";
import {EntityCard} from "../components/EntityCard";
import {Tag,type Tone} from "../components/StatusTag";
import {t} from "../i18n";
import type {EntityRecord} from "./Records";

export type KanbanLane={name:string;title:string;tone?:Tone};
export type KanbanMove={schema:string;title:string;from:string[];to:string;input?:string};
const kanbanMoveKey=(move:KanbanMove)=>move.input?`${move.schema}/${move.to}`:move.schema;
export function kanbanMove(record:EntityRecord,field:string,target:string,moves:KanbanMove[]) {
 const matching=moves.filter(m=>m.from.includes(String(record[field]))&&m.to===target&&m.to!==record[field]);
 return matching.length===1?matching[0]:undefined;
}

/** Original records and transitions only; a move gesture emits a command and
 * waits for its owner to provide the next window. No optimistic state write. */
export function RecordKanban({records,lanes,stateField,labelField,selected,onSelect,moves=[],onMove,properties=()=>[],label}: {
 records:EntityRecord[];lanes:KanbanLane[];stateField:string;labelField:string;selected?:string;
 onSelect:(record?:EntityRecord)=>void;moves?:KanbanMove[];onMove?:(record:EntityRecord,schema:string,destination?:string)=>void;
 properties?:(record:EntityRecord)=>[string,ReactNode][];label:string;
}) {
 const [dragging,setDragging]=useState<string>(),[over,setOver]=useState<string>();
 if(records.length>200||lanes.length>32)return <p role="alert">{t("Kanban records or lanes exceed their display bound.")}</p>;
 const known=new Set(lanes.map(l=>l.name)),invalid=records.filter(r=>!known.has(String(r[stateField]))).length;
 const dragged=records.find(r=>r.id===dragging);
 return <div role="region" aria-label={label} className="grid min-w-0 grid-cols-1 gap-2">
 {invalid>0&&<p role="status" className="text-xs text-warning">{t("{count} records have a missing or undeclared state.",{count:invalid})}</p>}
 <div className="flex min-w-0 gap-3 overflow-x-auto pb-2">{lanes.map(lane=>{
 const cards=records.filter(r=>r[stateField]===lane.name),canDrop=!!onMove&&!!dragged&&!!kanbanMove(dragged,stateField,lane.name,moves);
 return <section key={lane.name} aria-label={lane.title} className={`flex min-h-40 w-72 shrink-0 flex-col gap-2 rounded-md border p-2 ${over===lane.name&&canDrop?"border-primary bg-row-selected":"border-border bg-row-hover"}`} onDragOver={e=>{if(canDrop){e.preventDefault();setOver(lane.name);}}} onDragLeave={()=>setOver(undefined)} onDrop={e=>{e.preventDefault();if(dragged&&onMove){const move=kanbanMove(dragged,stateField,lane.name,moves);if(move){if(move.input)onMove(dragged,move.schema,move.to);else onMove(dragged,move.schema);}}setDragging(undefined);setOver(undefined);}}>
 <div className="flex items-center justify-between gap-2"><Tag label={lane.title} tone={lane.tone}/><span className="text-xs tabular-nums text-muted">{cards.length}</span></div>
 <div className="grid max-h-[28rem] content-start gap-2 overflow-y-auto">{cards.map(record=>{
 const title=typeof record[labelField]==="string"&&record[labelField]?String(record[labelField]):record.id;
 const available=moves.filter(m=>m.from.includes(String(record[stateField]))&&m.to!==record[stateField]);
 return <div key={record.id} draggable={!!onMove&&available.length>0} onDragStart={e=>{setDragging(record.id);e.dataTransfer.setData("application/platform-record",record.id);e.dataTransfer.effectAllowed="move";}} onDragEnd={()=>{setDragging(undefined);setOver(undefined);}} className={selected===record.id?"rounded-md ring-2 ring-primary":""}>
 <EntityCard title={<Button variant="ghost" className="h-auto max-w-full justify-start whitespace-normal p-0 text-left" aria-label={title} aria-pressed={selected===record.id} onClick={()=>onSelect(selected===record.id?undefined:record)}>{title}</Button>} subtitle={record.id} properties={properties(record)} actions={onMove&&available.length>0?<Select aria-label={t("Move {record} with action",{record:title})} value="" onChange={e=>{const move=available.find(m=>kanbanMoveKey(m)===e.target.value);if(move){if(move.input)onMove(record,move.schema,move.to);else onMove(record,move.schema);}}}><option value="">{t("Move with action")}</option>{available.map(m=><option key={kanbanMoveKey(m)} value={kanbanMoveKey(m)}>{m.title} → {lanes.find(l=>l.name===m.to)?.title??m.to}</option>)}</Select>:undefined}/>
 </div>;
 })}{!cards.length&&<p className="p-2 text-xs text-muted">{t("No cards in this window.")}</p>}</div>
 </section>;
 })}</div></div>;
}
