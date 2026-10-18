import { ProtocolSelect, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Input, InputDraftProvider, Notice, Select, t } from "@platform/ui";
import { useState } from "react";
import { nameOf, type Process, type ProtocolBinding } from "./object-model";

/** Protocol declarations remain part of the original object draft. */
export function ObjectProtocolsEditor({ process, onChange }: { process: Process; onChange: (value: Process) => void }) {
  const query = useReadQuery<Api.ProtocolInfo[]>("/v1/protocols");
  const bindings = process.protocols ?? [];
  const actions = [
    { name: "create", title: t("Create") }, { name: "edit", title: t("Edit") }, { name: "archive", title: t("Archive") },
    ...process.actions.map(action => ({ name: action.name, title: action.title || action.name })),
  ];
  const patch = (at: number, change: Partial<ProtocolBinding>) => onChange({ ...process, protocols: bindings.map((binding, index) => index === at ? { ...binding, ...change } : binding) });
  return <div className="grid max-w-4xl content-start gap-4 p-4">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-sm font-semibold">{t("Protocols provided by this object")}</h2>
      <Button size="sm" disabled={bindings.length >= 16} onClick={() => onChange({ ...process, protocols: [...bindings, { name: "", version: 1, actions: {} }] })}>{t("Add protocol")}</Button>
    </div>
    <p className="text-sm text-muted">{t("Map a versioned protocol to this object's actions and readable records. Save keeps the declaration as a draft; publishing makes it available through the original protocol routes and permissions.")}</p>
    {query.isError && <Notice tone="warning" role="alert" message={t("The protocol directory could not be loaded. Existing declarations are preserved.")} />}
    {!bindings.length && <p className="text-sm text-muted">{t("This object does not provide a protocol yet.")}</p>}
    {bindings.map((binding, at) => <InputDraftProvider key={at} scope={JSON.stringify({ kind: "protocol", at })}>
      <fieldset className="grid min-w-0 gap-3 border-t border-border pt-4" aria-label={t("Protocol binding {number}", { number: at + 1 })}>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs sm:col-span-2">{t("Use an installed protocol")}<ProtocolSelect value={binding.name ? `${binding.name}/${binding.version}` : ""} empty={t("Choose an installed protocol, or declare one below")} onChange={id => {
            const contract = query.data?.find(item => item.id === id);
            if (!contract) return;
            const slash = id.lastIndexOf("/");
            const matching = (name: string) => actions.some(action => action.name === name) ? name : "";
            patch(at, { name: id.slice(0, slash), version: Number(id.slice(slash + 1)), contract: contract.contract?.Name ? contract.contract : undefined, actions: Object.fromEntries((contract.actions ?? []).map(name => [name, binding.actions[name] ?? matching(name)])), reads: [...(contract.reads ?? [])], events: Object.fromEntries((contract.events ?? []).map(event => [event.name, binding.events?.[event.name] ?? matching(event.name)])) });
          }} /></label>
          <label className="grid gap-1 text-xs">{t("Protocol name")}<Input value={binding.name} placeholder="production.orders" onChange={event => patch(at, { name: event.target.value, contract: event.target.value === binding.name ? binding.contract : undefined })} /></label>
          <label className="grid gap-1 text-xs">{t("Protocol version")}<Input type="number" draftKey="protocol-version" min={1} step={1} max={Number.MAX_SAFE_INTEGER} value={binding.version} onChange={event => patch(at, { version: event.target.valueAsNumber, contract: event.target.valueAsNumber === binding.version ? binding.contract : undefined })} /></label>
        </div>
        {binding.contract && <p className="text-xs text-muted">{t("Uses the installed protocol contract. Publishing checks that mapped object actions accept its declared inputs.")}</p>}
        <ProtocolMappings kind="actions" value={binding.actions} actions={actions} onChange={value => patch(at, { actions: value })} />
        <fieldset className="grid gap-2"><legend className="mb-2 text-xs font-medium">{t("Record reads")}</legend>
          <p className="text-xs text-muted">{t("Each named read returns this object's records within the caller's current read permissions.")}</p>
          {(binding.reads ?? []).map((read, index) => <div key={index} className="flex items-center gap-2">
            <Input aria-label={t("Protocol read {number}", { number: index + 1 })} value={read} onChange={event => patch(at, { reads: binding.reads!.map((name, ri) => ri === index ? event.target.value : name) })} />
            <Button size="sm" variant="ghost" onClick={() => patch(at, { reads: binding.reads!.filter((_, ri) => ri !== index) })}>{t("Remove read")}</Button>
          </div>)}
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => patch(at, { reads: [...(binding.reads ?? []), nameOf("records", binding.reads ?? [])] })}>{t("Add record read")}</Button>
        </fieldset>
        <ProtocolMappings kind="events" value={binding.events ?? {}} actions={actions} onChange={value => patch(at, { events: value })} />
        <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => onChange({ ...process, protocols: bindings.filter((_, index) => index !== at) })}>{t("Remove protocol")}</Button>
      </fieldset>
    </InputDraftProvider>)}
  </div>;
}

/** Existing names are stable map keys; adding a name never overwrites a mapping. */
function ProtocolMappings({ kind, value, actions, onChange }: {
  kind: "actions" | "events"; value: Record<string, string>; actions: { name: string; title: string }[]; onChange: (value: Record<string, string>) => void;
}) {
  const [name, setName] = useState("");
  const available = (selected: string) => <>
    <option value="">{t("Choose an object action")}</option>
    {selected && !actions.some(action => action.name === selected) && <option value={selected}>{selected}</option>}
    {actions.map(action => <option key={action.name} value={action.name}>{action.title} · {action.name}</option>)}
  </>;
  return <fieldset className="grid gap-2" aria-label={t(kind === "actions" ? "Protocol action mappings" : "Protocol event mappings")}>
    <legend className="mb-2 text-xs font-medium">{t(kind === "actions" ? "Protocol actions" : "Protocol events")}</legend>
    {kind === "events" && <p className="text-xs text-muted">{t("An accepted object action emits its mapped protocol event.")}</p>}
    {Object.entries(value).map(([verb, action]) => <div key={verb} className="grid items-end gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
      <label className="grid min-w-0 gap-1 text-xs">{verb}<Select aria-label={t("Object action for {name}", { name: verb })} value={action} onChange={event => onChange({ ...value, [verb]: event.target.value })}>{available(action)}</Select></label>
      <Button size="sm" variant="ghost" onClick={() => onChange(Object.fromEntries(Object.entries(value).filter(([key]) => key !== verb)))}>{t("Remove mapping")}</Button>
    </div>)}
    <div className="flex flex-wrap items-end gap-2">
      <label className="grid min-w-0 flex-1 gap-1 text-xs">{t(kind === "actions" ? "Protocol action name" : "Protocol event name")}<Input value={name} onChange={event => setName(event.target.value)} /></label>
      <Button size="sm" variant="ghost" disabled={!name.trim() || Object.hasOwn(value, name.trim())} onClick={() => { onChange({ ...value, [name.trim()]: "" }); setName(""); }}>{t(kind === "actions" ? "Add action mapping" : "Add event mapping")}</Button>
    </div>
  </fieldset>;
}
