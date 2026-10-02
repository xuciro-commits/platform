import { variableAccessible } from "../page-layout";
import type { Api } from "@platform/kernel";
import { useHost, pageUIProfile, assetKey } from "@platform/app";
import { Card, Checkbox, Input, Select, t } from "@platform/ui";

export function NavigationPanel({ document, section, owner, overlay, eventName, onChange }: { document: Api.PageDocument; section: string; eventName: string; owner?: string; overlay?: string; onChange: (document: Api.PageDocument) => void }) {
  const { definitions } = useHost(), event = document.events?.find((event) => event.source === section);
  const kind = event?.navigate ? "navigate" : event?.return ? "return" : "state";
  const pages = definitions.filter((d) => d.page);
  const update = (binding: Api.PageEventBinding) => onChange({ ...document, uiProfile: pageUIProfile, events: [...(document.events ?? []).filter((e) => e.source !== section), binding] });
  const target = pages.find((page) => assetKey(page.ref) === (event?.navigate && assetKey(event.navigate.page)));
  const eligible = Object.entries(document.variables ?? {}).filter(([, v]) => variableAccessible(v, owner, overlay));
  const patch = (navigation: Api.PageNavigation) => update({ source: section, event: eventName, target: "", navigate: navigation });
  return <Card className="grid gap-3 p-3">
    <label className="grid gap-1 text-xs">{t("Click handler")}<Select value={kind} onChange={(e) => {
      const kind = e.target.value;
      if (kind === "return") update({ source: section, event: eventName, target: "", return: true });
      else if (kind === "navigate") { const page = pages[0]; if (page) patch({ page: page.ref, interfaceVersion: page.page?.document?.interface?.version ?? 0 }); }
      else update({ source: section, event: eventName, target: "", value: false });
    }}><option value="state">{t("Set page state")}</option><option value="navigate">{t("Open another page")}</option><option value="return">{t("Return to caller")}</option></Select></label>
    {event?.navigate && <>
      <label className="grid gap-1 text-xs">{t("Target page")}<Select value={assetKey(event.navigate.page)} onChange={(e) => { const page = pages.find((p) => assetKey(p.ref) === e.target.value); if (page) patch({ page: page.ref, interfaceVersion: page.page?.document?.interface?.version ?? 0 }); }}>{pages.map((page) => <option key={assetKey(page.ref)} value={assetKey(page.ref)}>{page.page?.title || page.ref.name}</option>)}</Select></label>
      {Object.entries(target?.page?.document?.interface?.inputs ?? {}).map(([id, port]) => {
        const binding = event.navigate!.inputs?.[id];
        return <div key={id} className="grid gap-2 border-t border-border pt-2"><label className="grid gap-1 text-xs">{t("Input {name}", { name: id })}<Select value={binding?.variable ?? ""} onChange={(e) => patch({ ...event.navigate!, inputs: { ...event.navigate!.inputs, [id]: e.target.value ? { variable: e.target.value } : { literal: port.type === "boolean" ? false : "" } } })}><option value="">{t("Literal")}</option>{eligible.filter(([, v]) => v.type === port.type).map(([key, v]) => <option key={key} value={key}>{v.title || key}</option>)}</Select></label>
          {!binding?.variable && port.type !== "record" && (port.type === "boolean" ? <Checkbox checked={binding?.literal === true} onChange={(value) => patch({ ...event.navigate!, inputs: { ...event.navigate!.inputs, [id]: { literal: value } } })}>{t("Input value")}</Checkbox> : <Input aria-label={t("Input value")} value={String(binding?.literal ?? "")} onChange={(e) => patch({ ...event.navigate!, inputs: { ...event.navigate!.inputs, [id]: { literal: e.target.value } } })} />)}
        </div>;
      })}
      {Object.entries(target?.page?.document?.interface?.outputs ?? {}).map(([id, port]) => <label key={id} className="grid gap-1 text-xs">{t("Return {name} to state", { name: id })}<Select value={event.navigate!.results?.[id] ?? ""} onChange={(e) => { const results = { ...event.navigate!.results }; if (e.target.value) results[id] = e.target.value; else delete results[id]; patch({ ...event.navigate!, results }); }}><option value="">{t("Ignore output")}</option>{eligible.filter(([, v]) => (v.mode === "state" || v.mode === "shared" && v.writable) && v.type === port.type).map(([key, v]) => <option key={key} value={key}>{v.title || key}</option>)}</Select></label>)}
    </>}
  </Card>;
}
