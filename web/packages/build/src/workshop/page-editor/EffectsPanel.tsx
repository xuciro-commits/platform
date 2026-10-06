// A page event runs an ordered chain of effects (ADR-0053 §11): set state,
// open or close an overlay, take an action, open another page, return to the
// caller. This panel edits that chain for one event of one widget (or one
// control of a button group). Navigation and return end the chain, so they are
// only offered as the last step.
import { variableAccessible } from "../page-layout";
import type { Api } from "@platform/kernel";
import { useHost, pageUIProfile, assetKey } from "@platform/app";
import { Button, Card, Checkbox, Input, Select, t } from "@platform/ui";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";

type Kind = "set" | "overlay" | "action" | "navigate" | "return";
type Allowed = { readonly kinds: readonly Kind[]; readonly selection?: boolean };
export const clickEffects: Allowed = { kinds: ["set", "overlay", "action", "navigate", "return"] };
export const groupEffects: Allowed = { kinds: ["set", "overlay", "action"] };
export const selectEffects: Allowed = { kinds: ["set"], selection: true };

export function EventEffects({ document, section, control, eventName, owner, overlay, onChange, allowed, title }: {
  document: Api.PageDocument; section: string; control?: string; eventName: string; owner?: string; overlay?: string;
  onChange: (document: Api.PageDocument) => void; allowed: Allowed; title?: string;
}) {
  const { definitions, catalog } = useHost();
  const match = (event: Api.PageEventBinding) => event.source === section && (event.control ?? "") === (control ?? "") && event.event === eventName;
  const event = document.events?.find(match), effects = event?.effects ?? [];
  const overlays = Object.values(document.overlays ?? {}), openVariables = new Set(overlays.map((o) => o.openVariable));
  const pages = definitions.filter((d) => d.page);
  const variables = Object.entries(document.variables ?? {}).filter(([, v]) => variableAccessible(v, owner, overlay));
  const writable = variables.filter(([id, v]) => !openVariables.has(id) && (allowed.selection
    ? ["string", "boolean"].includes(v.type) && (v.mode === "state" && ["page", "overlay"].includes(v.scope) || v.mode === "shared" && v.scope === "application" && v.writable)
    : v.mode === "state" || v.mode === "shared" && v.writable));
  const records = variables.filter(([, v]) => v.type === "record");
  const write = (next: Api.PageEffect[]) => onChange({ ...document, uiProfile: pageUIProfile,
    events: [...(document.events ?? []).filter((e) => !match(e)), ...(next.length ? [{ source: section, control, event: eventName, effects: next }] : [])] });
  const replace = (at: number, effect: Api.PageEffect) => write(effects.map((e, i) => i === at ? effect : e));
  const kindOf = (effect: Api.PageEffect): Kind => effect.kind === "set" && openVariables.has(effect.target ?? "") ? "overlay" : effect.kind as Kind;
  const fresh = (kind: Kind): Api.PageEffect => {
    switch (kind) {
      case "overlay": return { kind: "set", target: overlays[0]?.openVariable ?? "", value: true };
      case "set": { const [id, v] = writable[0] ?? ["", undefined]; return { kind: "set", target: id, value: v?.type === "boolean" ? false : "" }; }
      case "action": { const declared = catalog.find((a) => !a.new) ?? catalog[0]; return { kind: "action", action: { ref: { app: declared?.schema.split(".")[0] ?? "build", kind: "action", name: declared?.schema ?? "" }, recordVariable: declared?.new ? undefined : records[0]?.[0] } }; }
      case "navigate": { const page = pages[0]; return { kind: "navigate", navigate: { page: page?.ref ?? { app: "", kind: "page", name: "" }, interfaceVersion: page?.page?.document?.interface?.version ?? 0 } }; }
      case "return": return { kind: "return" };
    }
  };
  const terminal = (e: Api.PageEffect) => e.kind === "navigate" || e.kind === "return";
  const ended = effects.some(terminal), full = effects.length >= 8;
  const labels: Record<Kind, string> = { set: t("Set page state"), overlay: t("Open or close an overlay"), action: t("Take an action"), navigate: t("Open another page"), return: t("Return to caller") };
  const kinds = allowed.kinds.filter((k) => k !== "overlay" || overlays.length) .filter((k) => k !== "return" || document.interface);
  return <Card className="grid gap-3 p-3">
    <strong className="text-xs">{title ?? t(allowed.selection ? "On record selection" : "On click")}</strong>
    {effects.length === 0 && <p className="text-xs text-muted">{t(allowed.selection ? "Nothing happens on selection yet." : "Nothing happens on click yet. Add the first effect.")}</p>}
    <ol className="grid gap-2">{effects.map((effect, at) => {
      const kind = kindOf(effect), last = at === effects.length - 1;
      return <li key={at} className="grid gap-2 rounded-md border border-border p-2">
        <div className="flex items-center gap-1 text-xs">
          <span className="w-5 text-muted">{at + 1}.</span>
          <Select aria-label={t("Effect")} className="min-w-0 flex-1" value={kind} onChange={(e) => replace(at, fresh(e.target.value as Kind))}>
            {kinds.filter((k) => last || !["navigate", "return"].includes(k)).map((k) => <option key={k} value={k}>{labels[k]}</option>)}
          </Select>
          <Button size="sm" variant="ghost" aria-label={t("Move up")} disabled={at === 0 || terminal(effect)} onClick={() => write(swap(effects, at, at - 1))}><ArrowUp className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Move down")} disabled={last || terminal(effects[at + 1]!)} onClick={() => write(swap(effects, at, at + 1))}><ArrowDown className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Remove effect")} onClick={() => write(effects.filter((_, i) => i !== at))}><Trash2 className="size-3" /></Button>
        </div>
        {kind === "overlay" && <Select aria-label={t("Overlay action")} value={`${effect.target}:${effect.value === true ? "open" : "close"}`} onChange={(e) => { const i = e.target.value.lastIndexOf(":"); replace(at, { kind: "set", target: e.target.value.slice(0, i), value: e.target.value.slice(i + 1) === "open" }); }}>
          {overlays.flatMap((o) => [true, false].map((open) => <option key={`${o.openVariable}:${open}`} value={`${o.openVariable}:${open ? "open" : "close"}`}>{t(open ? "Open {title}" : "Close {title}", { title: o.title })}</option>))}
        </Select>}
        {kind === "set" && <SetEffect document={document} effect={effect} writable={writable} onChange={(next) => replace(at, next)} />}
        {kind === "action" && <ActionEffect effect={effect} records={records} document={document} onChange={(next) => replace(at, next)} />}
        {kind === "navigate" && effect.navigate && <NavigateEffect navigation={effect.navigate} variables={variables} onChange={(navigate) => replace(at, { kind: "navigate", navigate })} />}
        {kind === "return" && <p className="text-xs text-muted">{t("Returns this page's interface outputs to the page that opened it.")}</p>}
      </li>;
    })}</ol>
    <div className="flex flex-wrap items-center gap-2">
      <Select aria-label={t("Add effect")} value="" disabled={ended || full} onChange={(e) => { if (e.target.value) write([...effects, fresh(e.target.value as Kind)]); }}>
        <option value="">{t("Add an effect…")}</option>{kinds.map((k) => <option key={k} value={k}>{labels[k]}</option>)}
      </Select>
      {ended && <span className="text-xs text-muted">{t("Opening a page or returning ends the chain.")}</span>}
    </div>
    {effects.length > 0 && allowed.selection && <Button size="sm" onClick={() => write([])}>{t("Remove selection event")}</Button>}
    {effects.some((e) => e.kind === "action") && <p className="text-xs text-muted">{t("An action that is refused or cancelled stops the chain.")}</p>}
  </Card>;
}

const swap = (list: Api.PageEffect[], a: number, b: number) => { const next = [...list]; [next[a], next[b]] = [next[b]!, next[a]!]; return next; };

function SetEffect({ document, effect, writable, onChange }: { document: Api.PageDocument; effect: Api.PageEffect; writable: [string, Api.PageVariable][]; onChange: (next: Api.PageEffect) => void }) {
  const variable = document.variables?.[effect.target ?? ""];
  const tabs = Object.values(document.nodes).filter((node) => node.kind === "tabs" && node.activeVariable === effect.target).flatMap((node) => node.children ?? []);
  return <>
    <label className="grid gap-1 text-xs">{t("Target state variable")}<Select value={effect.target ?? ""} onChange={(e) => { const v = document.variables?.[e.target.value]; if (v) onChange({ kind: "set", target: e.target.value, value: v.type === "boolean" ? false : "" }); }}>
      <option value="">{t("Choose a state variable")}</option>{writable.map(([id, v]) => <option key={id} value={id}>{v.title || id}</option>)}</Select></label>
    {variable && (variable.type === "boolean" ? <Checkbox checked={effect.value === true} onChange={(value) => onChange({ ...effect, value })}>{t("Event value")}</Checkbox>
      : tabs.length ? <label className="grid gap-1 text-xs">{t("Event value")}<Select value={String(effect.value ?? "")} onChange={(e) => onChange({ ...effect, value: e.target.value })}><option value="">{t("Choose a tab")}</option>{tabs.map((id) => <option key={id} value={id}>{document.nodes[id]?.title || id}</option>)}</Select></label>
      : <label className="grid gap-1 text-xs">{t("Event value")}<Input value={String(effect.value ?? "")} onChange={(e) => onChange({ ...effect, value: e.target.value })} /></label>)}
  </>;
}

function ActionEffect({ effect, records, document, onChange }: { effect: Api.PageEffect; records: [string, Api.PageVariable][]; document: Api.PageDocument; onChange: (next: Api.PageEffect) => void }) {
  const { catalog } = useHost();
  const action = effect.action ?? { ref: { app: "", kind: "action", name: "" } };
  const variable = document.variables?.[action.recordVariable ?? ""], object = variable?.source?.object?.name;
  const offered = catalog.filter((a) => action.recordVariable ? !a.new && (!object || a.target === object) : a.new);
  const declared = catalog.find((a) => a.schema === action.ref.name);
  return <>
    <label className="grid gap-1 text-xs">{t("Record to act on")}<Select value={action.recordVariable ?? ""} onChange={(e) => { const next = catalog.find((a) => e.target.value ? !a.new : a.new); onChange({ kind: "action", action: { ref: { app: next?.schema.split(".")[0] ?? "build", kind: "action", name: next?.schema ?? "" }, recordVariable: e.target.value || undefined } }); }}>
      <option value="">{t("None: a creating action")}</option>{records.map(([id, v]) => <option key={id} value={id}>{v.title || id}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Action")}<Select value={action.ref.name} onChange={(e) => onChange({ kind: "action", action: { ...action, ref: { app: e.target.value.split(".")[0] ?? "build", kind: "action", name: e.target.value } } })}>
      <option value="">{t("Choose an action")}</option>{offered.map((a) => <option key={a.schema} value={a.schema}>{a.title} · {a.schema}</option>)}</Select></label>
    {declared && <p className="text-xs text-muted">{declared.payload.length ? t("Opens the action's form; the chain continues after it commits.") : t("Taken at once, without a form.")}</p>}
    {!declared && action.ref.name && <p className="text-xs text-danger">{t("This action is not offered to you.")}</p>}
  </>;
}

function NavigateEffect({ navigation, variables, onChange }: { navigation: Api.PageNavigation; variables: [string, Api.PageVariable][]; onChange: (next: Api.PageNavigation) => void }) {
  const { definitions } = useHost(), pages = definitions.filter((d) => d.page);
  const target = pages.find((page) => assetKey(page.ref) === assetKey(navigation.page));
  const choose = (key: string) => { const page = pages.find((p) => assetKey(p.ref) === key); if (page) onChange({ page: page.ref, interfaceVersion: page.page?.document?.interface?.version ?? 0 }); };
  return <>
    <label className="grid gap-1 text-xs">{t("Target page")}<Select value={assetKey(navigation.page)} onChange={(e) => choose(e.target.value)}>{pages.map((page) => <option key={assetKey(page.ref)} value={assetKey(page.ref)}>{page.page?.title || page.ref.name}</option>)}</Select></label>
    {Object.entries(target?.page?.document?.interface?.inputs ?? {}).map(([id, port]) => {
      const binding = navigation.inputs?.[id];
      const set = (value: Api.PageValue) => onChange({ ...navigation, inputs: { ...navigation.inputs, [id]: value } });
      return <div key={id} className="grid gap-2 border-t border-border pt-2">
        <label className="grid gap-1 text-xs">{t("Input {name}", { name: id })}<Select value={binding?.variable ?? ""} onChange={(e) => set(e.target.value ? { variable: e.target.value } : { literal: port.type === "boolean" ? false : "" })}><option value="">{t("Literal")}</option>{variables.filter(([, v]) => v.type === port.type).map(([key, v]) => <option key={key} value={key}>{v.title || key}</option>)}</Select></label>
        {!binding?.variable && port.type !== "record" && (port.type === "boolean" ? <Checkbox checked={binding?.literal === true} onChange={(value) => set({ literal: value })}>{t("Input value")}</Checkbox> : <Input aria-label={t("Input value")} value={String(binding?.literal ?? "")} onChange={(e) => set({ literal: e.target.value })} />)}
      </div>;
    })}
    {Object.entries(target?.page?.document?.interface?.outputs ?? {}).map(([id, port]) => <label key={id} className="grid gap-1 text-xs">{t("Return {name} to state", { name: id })}<Select value={navigation.results?.[id] ?? ""} onChange={(e) => { const results = { ...navigation.results }; if (e.target.value) results[id] = e.target.value; else delete results[id]; onChange({ ...navigation, results }); }}><option value="">{t("Ignore output")}</option>{variables.filter(([, v]) => (v.mode === "state" || v.mode === "shared" && v.writable) && v.type === port.type).map(([key, v]) => <option key={key} value={key}>{v.title || key}</option>)}</Select></label>)}
  </>;
}
