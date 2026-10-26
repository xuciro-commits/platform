import { Button, Checkbox, Input, Panel, Select, t, type EntityInfo } from "@platform/ui";
import type { Process } from "./object-model";
import type { Api } from "@platform/kernel";

export function BusinessVersionEditor({process,entities,onChange}:{process:Process;entities:EntityInfo[];onChange:(next:Process)=>void}) {
  const versioning=process.versioning;
  const patch=(next:Partial<Api.Versioning>)=>onChange({...process,versioning:{...versioning!,...next}});
  const fields=(name:keyof Api.Versioning,label:string,kinds:string[]) => <label className="grid gap-1 text-xs">{t(label)}<Select value={versioning?.[name]??""} onChange={event=>patch({[name]:event.target.value})}><option value="">{t("Choose a field")}</option>{process.fields.filter(field=>kinds.includes(field.type)).map(field=><option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>;
  const pins=process.versionPins??[];
  const references=process.fields.filter(field=>field.type==="reference"&&entities.find(entity=>entity.type===field.ref)?.versioning);
  return <div className="grid content-start gap-3 p-4">
    <Panel title={t("Business versioning")}>
      <label className="flex items-center gap-2 text-sm"><Checkbox checked={!!versioning} onChange={checked=>onChange({...process,versioning:checked?{key:"",version:"",validFrom:"",pin:"none"}:undefined})}/>{t("Retain business versions as ordinary records")}</label>
      {versioning&&<div className="mt-3 grid gap-3 sm:grid-cols-2">
        {fields("key","Business key",["text","choice"])}{fields("version","Business version",["text","integer"])}
        {fields("validFrom","Valid from field",["date","datetime"])}{fields("validUntil","Valid until field",["date","datetime"])}
        <label className="grid gap-1 text-xs">{t("Pin policy")}<Select value={versioning.pin.startsWith("on_state:")?"on_state":versioning.pin} onChange={event=>patch({pin:event.target.value==="on_state"?"on_state:":event.target.value})}><option value="none">{t("No automatic pin")}</option><option value="on_create">{t("On consumer creation")}</option><option value="on_state">{t("On consumer state")}</option></Select></label>
        {versioning.pin.startsWith("on_state:")&&<label className="grid gap-1 text-xs">{t("Consumer state name")}<Input value={versioning.pin.slice(9)} onChange={event=>patch({pin:`on_state:${event.target.value}`})}/></label>}
      </div>}
    </Panel>
    <Panel title={t("Pinned business versions")}>
      <p className="mb-3 text-sm text-muted">{t("Choose a versioned reference and the field that receives its version. The source policy decides when it becomes fixed.")}</p>
      {pins.map((pin,at)=><div className="mb-3 grid gap-2 sm:grid-cols-[1fr_1fr_auto]" key={at}>
        <label className="grid gap-1 text-xs">{t("Versioned reference")}<Select value={pin.reference} onChange={event=>onChange({...process,versionPins:pins.map((value,i)=>i===at?{reference:event.target.value,version:""}:value)})}><option value="">{t("Choose a field")}</option>{references.map(field=><option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>
        <label className="grid gap-1 text-xs">{t("Pinned version field")}<Select value={pin.version} onChange={event=>onChange({...process,versionPins:pins.map((value,i)=>i===at?{...value,version:event.target.value}:value)})}><option value="">{t("Choose a field")}</option>{process.fields.filter(field=>["text","integer"].includes(field.type)&&field.name!==pin.reference).map(field=><option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>
        <Button variant="ghost" onClick={()=>onChange({...process,versionPins:pins.filter((_,i)=>i!==at)})}>{t("Remove pin")}</Button>
      </div>)}
      <Button onClick={()=>onChange({...process,versionPins:[...pins,{reference:"",version:""}]})}>{t("Add a version pin")}</Button>
    </Panel>
  </div>;
}
