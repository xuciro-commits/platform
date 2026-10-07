import {Panel,RecordKanban,entityFrom,valueOf,t,type EntityInfo,type EntityRecord,type KanbanMove,type Tone} from "@platform/ui";
import {useTransition} from "../actions/actions";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
export type KanbanPorts={object:string;info?:EntityInfo;window?:QueryWindow;cardLabel:string;fields?:string[];moves:KanbanMove[];selected?:EntityRecord;onSelect:(record?:EntityRecord)=>void;live:boolean;title:string};
export function KanbanRenderer({object,info,window,cardLabel,fields=[],moves,selected,onSelect,live,title}:KanbanPorts) {
 const transition=useTransition(object),l=info?.lifecycle;
 if(!info||!l||!info.fields.some(f=>f.name===l.field)||l.states.length>32||fields.length>4||cardLabel!=="id"&&!info.fields.some(f=>f.name===cardLabel)||fields.some(name=>!info.fields.some(f=>f.name===name)))return <Panel role="alert">{t("Kanban fields or lifecycle are unavailable.")}</Panel>;
 const entity=entityFrom(info);
 const tone=(value?:string):Tone=>["info","success","warning","danger"].includes(value??"")?value as Tone:"neutral";
 return <><QueryWindowFrame window={window} description={t("This board shows the current query window.")}>{page=><RecordKanban records={page.records} lanes={l.states.map(s=>({name:s.name,title:s.title,tone:tone(s.tone)}))} stateField={l.field} labelField={cardLabel} selected={selected?.id} onSelect={onSelect} moves={moves} onMove={live?(record,schema,destination)=>{const move=moves.find(m=>m.schema===schema&&(!m.input||m.to===destination)&&m.from.includes(String(record[l.field])));if(move)transition.take(schema,record,move.input?{parameter:move.input,value:move.to}:undefined);}:undefined} properties={record=>fields.map(name=>[entity.fields[name]!.label,entity.fields[name]!.display(valueOf(entity,name,record),record)])} label={title}/>}</QueryWindowFrame>{transition.dialog}</>;
}
