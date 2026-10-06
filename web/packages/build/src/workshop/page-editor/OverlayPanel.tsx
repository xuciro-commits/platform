import { variableAccessible } from "../page-layout";
import type { Api } from "@platform/kernel";
import { pageUIProfile } from "@platform/app";
import { Button, Card, Checkbox, Input, Select, t } from "@platform/ui";

export function OverlayProperties({ overlay, onChange, onRemove }: {
  overlay: Api.PageOverlay; onChange: (patch: Partial<Api.PageOverlay>) => void; onRemove: () => void;
}) {
  const value=overlay.presentation??{side:"right",size:"medium",backdrop:true,closeOnBackdrop:true,closeOnEsc:true},update=(patch:Partial<Api.PageOverlayPresentation>)=>onChange({presentation:{...value,...patch}});
  return <Card className="grid gap-3 p-3"><strong className="text-xs">{t("Overlay")}</strong>
    <label className="grid gap-1 text-xs">{t("Overlay title")}<Input value={overlay.title} onChange={(event) => onChange({ title: event.target.value })} /></label>
    <label className="grid gap-1 text-xs">{t("Overlay kind")}<Select value={overlay.kind} onChange={(event) => onChange({ kind: event.target.value })}><option value="modal">{t("Modal")}</option><option value="drawer">{t("Drawer")}</option></Select></label>
    <label className="grid gap-1 text-xs">{t("Overlay side")}<Select value={value.side} onChange={e=>update({side:e.target.value})}><option value="right">{t("Right")}</option><option value="left">{t("Left")}</option></Select></label>
    <label className="grid gap-1 text-xs">{t("Overlay size")}<Select value={value.size} onChange={e=>update({size:e.target.value,customWidth:e.target.value==="custom"?value.customWidth??420:undefined})}>{["small","medium","large","custom"].map(size=><option key={size} value={size}>{t(size)}</option>)}</Select></label>
    {value.size==="custom"&&<label className="grid gap-1 text-xs">{t("Overlay custom width")}<Input type="number" min={240} max={1200} value={value.customWidth??420} onChange={e=>update({customWidth:e.target.valueAsNumber})}/></label>}
    <Checkbox checked={value.backdrop} onChange={backdrop=>update({backdrop})}>{t("Show overlay backdrop")}</Checkbox><Checkbox checked={value.closeOnBackdrop} onChange={closeOnBackdrop=>update({closeOnBackdrop})}>{t("Close on backdrop")}</Checkbox><Checkbox checked={value.closeOnEsc} onChange={closeOnEsc=>update({closeOnEsc})}>{t("Close on Escape")}</Checkbox>
    <p className="text-xs text-muted">{t("Closing clears local inputs. The page selection stays available.")}</p>
    <Button onClick={onRemove}>{t("Remove overlay and its triggers")}</Button>
  </Card>;
}

export function ButtonEventProperties({ document, section, owner, overlay, eventName, onChange,control }: {
  document: Api.PageDocument; section: string; eventName:string; owner?: string; overlay?: string;control?:string; onChange: (document: Api.PageDocument) => void;
}) {
  const event = document.events?.find((event) => event.source === section&&(event.control??"")===(control??"")), variable = document.variables?.[event?.target ?? ""];
  const tabs = Object.values(document.nodes).filter((node) => node.kind === "tabs" && node.activeVariable === event?.target).flatMap((node) => node.children ?? []);
  const update = (target: string, value: string | boolean) => onChange({ ...document, uiProfile: pageUIProfile,
    events: [...(document.events ?? []).filter((event) => event.source !== section||(event.control??"")!==(control??"")), { source: section,control, event: eventName, target, value }] });
  const selection=eventName==="select",openVariables=new Set(Object.values(document.overlays??{}).map(o=>o.openVariable));
  return <Card className="grid gap-3 p-3"><strong className="text-xs">{t(selection?"On record selection":"On click")}</strong>
    {!selection&&<label className="grid gap-1 text-xs">{t("Overlay action")}<Select value={Object.values(document.overlays ?? {}).some((overlay) => overlay.openVariable === event?.target) ? `${event!.target}:${event!.value === true ? "open" : "close"}` : ""} onChange={(e) => { const at = e.target.value.lastIndexOf(":"); if (at > 0) update(e.target.value.slice(0, at), e.target.value.slice(at + 1) === "open"); }}>
      <option value="">{t("Choose an overlay action")}</option>{Object.entries(document.overlays ?? {}).flatMap(([id, overlay]) => [true, false].map((open) => <option key={`${id}:${open}`} value={`${overlay.openVariable}:${open ? "open" : "close"}`}>{t(open ? "Open {title}" : "Close {title}", { title: overlay.title })}</option>))}
    </Select></label>}
    <label className="grid gap-1 text-xs">{t("Target state variable")}<Select value={event?.target ?? ""} onChange={(e) => { const variable = document.variables?.[e.target.value]; if (variable) update(e.target.value, variable.type === "boolean" ? false : ""); }}>
      <option value="">{t("Choose a state variable")}</option>{Object.entries(document.variables ?? {}).filter(([id, variable]) => (variable.mode === "state" || !selection&&variable.mode === "shared" && variable.writable) && variableAccessible(variable, owner, overlay)&&(!selection||["string","boolean"].includes(variable.type)&&["page","overlay"].includes(variable.scope)&&!openVariables.has(id))).map(([id, variable]) => <option key={id} value={id}>{variable.title || id}</option>)}
    </Select></label>
    {variable && (variable.type === "boolean" ? <Checkbox checked={event?.value === true} onChange={(value) => update(event!.target, value)}>{t("Event value")}</Checkbox> : tabs.length ? <label className="grid gap-1 text-xs">{t("Event value")}<Select value={String(event?.value ?? "")} onChange={(e) => update(event!.target, e.target.value)}><option value="">{t("Choose a tab")}</option>{tabs.map((id, index) => <option key={id} value={id}>{document.nodes[id]?.title || t("Tab {n}", { n: index + 1 })}</option>)}</Select></label> : <label className="grid gap-1 text-xs">{t("Event value")}<Input value={String(event?.value ?? "")} onChange={(e) => update(event!.target, e.target.value)} /></label>)}
    <p className="text-xs text-muted">{t("This event changes page state. Business actions use their original action widget.")}</p>
    {selection&&event&&<Button size="sm" onClick={()=>onChange({...document,events:document.events?.filter(e=>e.source!==section)})}>{t("Remove selection event")}</Button>}
  </Card>;
}
