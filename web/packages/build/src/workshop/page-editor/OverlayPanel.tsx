import type { Api } from "@platform/kernel";
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
    {value.size==="custom"&&<label className="grid gap-1 text-xs">{t("Overlay custom width")}<Input draftKey="overlay-custom-width" type="number" min={240} max={1200} value={value.customWidth??420} onChange={e=>update({customWidth:e.target.valueAsNumber})}/></label>}
    <Checkbox checked={value.backdrop} onChange={backdrop=>update({backdrop})}>{t("Show overlay backdrop")}</Checkbox><Checkbox checked={value.closeOnBackdrop} onChange={closeOnBackdrop=>update({closeOnBackdrop})}>{t("Close on backdrop")}</Checkbox><Checkbox checked={value.closeOnEsc} onChange={closeOnEsc=>update({closeOnEsc})}>{t("Close on Escape")}</Checkbox>
    <p className="text-xs text-muted">{t("Closing clears local inputs. The page selection stays available.")}</p>
    <Button onClick={onRemove}>{t("Remove overlay and its triggers")}</Button>
  </Card>;
}
