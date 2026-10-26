import type { Api } from "@platform/kernel";
import { useHost, useRecordInventory } from "@platform/app";
import { Button, Checkbox, Disclosure, Input, Panel, Select, Tag, t } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";
import { nameOf, type Field } from "./object-model";

type Named = { id: string; name: string; title: string; state: string; object?: string; on?: string; kind?: string };
const fieldClass = "grid min-w-0 gap-1 text-xs";
const statusNames: Record<string, string> = { accepted: "Accepted by target", running: "In transit", completed: "Completed", stuck: "Stuck", failed: "Failed", unknown: "Outcome unknown" };
const policyNames: Record<string, string> = { manual: "Human intervention", escalate: "Notify reviewers", compensate: "Send reverse command", retry: "Automatically resend" };

/** Commands belong to the original object; each transaction selects one and
 * retains its own business key, target, readback and exception decisions. */
export function DispatchEditor({ value, onChange, commands, onCommandsChange, object, fields, choices, roles }: {
  value?: Api.Dispatch; onChange: (value?: Api.Dispatch) => void;
  commands: Api.DispatchCommand[]; onCommandsChange: (commands: Api.DispatchCommand[], rename?: { before: string; after: string }) => void;
  object: string; fields: Field[]; choices: ExpressionChoice[]; roles: string[];
}) {
  const { source, entities } = useHost();
  const writes = useRecordInventory<Named>("build.writeback"), connections = useRecordInventory<Named>("build.connection");
  const sources = useRecordInventory<Named>("build.source"), observations = useRecordInventory<Named>("build.observation");
  const command = commands.find(item => item.name === value?.command);
  const patch = (next: Partial<Api.Dispatch>) => value && onChange({ ...value, ...next });
  const read = (next: Partial<Api.DispatchRead>) => value && patch({ readback: { ...value.readback, ...next } });
  const updateCommand = (index: number, next: Partial<Api.DispatchCommand>) => {
    const before = commands[index]!;
    onCommandsChange(commands.map((item, at) => at === index ? { ...item, ...next } : item), next.name === undefined ? undefined : { before: before.name, after: next.name });
  };
  const initial = (): Api.Dispatch => ({ command: commands[0]?.name ?? "", target: "", key: "$subject", deadlineSeconds: 300, readback: { name: "status", source: "", keyField: "id", resultField: "state", states: { accepted: "accepted", running: "running", completed: "completed", stuck: "stuck", failed: "failed" }, everySeconds: 10 }, onStuck: "manual", onFail: "manual", reviewerRole: roles[0] ?? "builder" });
  const publishedWrites = writes.data?.records.filter(item => item.state === "published" && item.object === object && item.on === "dispatch") ?? [];
  const readKind = value?.readback.observation !== undefined ? "observation" : value?.readback.record !== undefined ? "record" : "source";
  const readings = Object.entries(value?.readback.states ?? {});
  const changeState = (index: number, external: string, state: string) => read({ states: Object.fromEntries(readings.map((entry, at) => at === index ? [external, state] : entry)) });
  const safe = !!command && (command.idempotent || command.receiverDeduplicates);
  return <Panel title={t("Device and black-box dispatch")} className="grid min-w-0 gap-3">
    <p className="rounded-md border border-border bg-row-hover p-3 text-sm">{t("Control loops, safety interlocks and emergency stops stay in the device, gateway or PLC. The platform only dispatches commands and reads status.")}</p>
    <Checkbox checked={!!value} onChange={enabled => onChange(enabled ? initial() : undefined)}>{t("Dispatch a declared command after this transaction")}</Checkbox>
    {(!!value || commands.length > 0) && <Disclosure summary={t("Object command declarations ({n})", { n: commands.length })} defaultOpen={!!value && !commands.length}>
      <div className="grid min-w-0 gap-3 pt-2">
        <p className="text-xs text-muted">{t("These named commands are shared by this object's actions. Request formats come from published dispatch writebacks; addresses and credentials stay in connections.")}</p>
        {commands.map((item, index) => <div key={index} className="grid min-w-0 gap-3 rounded-md border border-border p-3">
          <div className="grid gap-2 sm:grid-cols-2">
            <label className={fieldClass}>{t("Command name")}<Input value={item.name} onChange={event => updateCommand(index, { name: event.target.value })} /></label>
            <label className={fieldClass}>{t("Command title")}<Input value={item.title} onChange={event => updateCommand(index, { title: event.target.value })} /></label>
            <label className={fieldClass}>{t("Endpoint kind")}<Input value={item.endpointKind} onChange={event => updateCommand(index, { endpointKind: event.target.value })} /><span className="text-muted">{t("A logical adapter category using lowercase letters and digits.")}</span></label>
            <label className={fieldClass}>{t("Dispatch writeback")}<Select value={item.writeback} onChange={event => updateCommand(index, { writeback: event.target.value })}><option value="">{t("Choose a dispatch writeback")}</option>{publishedWrites.map(write => <option key={write.id} value={write.id}>{write.title}</option>)}</Select></label>
            <label className={fieldClass}>{t("Resend safety")}<Select value={item.idempotent ? "idempotent" : item.receiverDeduplicates ? "receiver" : "manual"} onChange={event => updateCommand(index, { idempotent: event.target.value === "idempotent", receiverDeduplicates: event.target.value === "receiver" })}><option value="manual">{t("Unsafe to resend automatically")}</option><option value="idempotent">{t("Idempotent command")}</option><option value="receiver">{t("Target deduplicates by business key")}</option></Select></label>
            <label className={fieldClass}>{t("Reverse command")}<Select value={item.compensate ?? ""} onChange={event => updateCommand(index, { compensate: event.target.value || undefined })}><option value="">{t("No reverse command")}</option>{commands.filter(candidate => candidate.name !== item.name).map(candidate => <option key={candidate.name} value={candidate.name}>{candidate.title || candidate.name}</option>)}</Select></label>
          </div>
          {item.receiverDeduplicates && <p className="text-xs text-warning">{t("Resending depends on the target honoring the stable business key. The platform cannot guarantee physical execution or exactly-once delivery.")}</p>}
          <Button size="sm" variant="ghost" className="w-fit" onClick={() => onCommandsChange(commands.filter((_, at) => at !== index))}>{t("Remove command declaration")}</Button>
        </div>)}
        {!publishedWrites.length && <p className="text-xs text-muted">{t("Publish a writeback for this object with 'Only declared device dispatches' selected, then choose it here.")}</p>}
        <Button size="sm" className="w-fit" onClick={() => onCommandsChange([...commands, { name: nameOf("command", commands.map(item => item.name)), title: "", endpointKind: "endpoint", writeback: "", idempotent: false }])}>{t("Add command declaration")}</Button>
      </div>
    </Disclosure>}
    {value && <>
      <div className="grid gap-2 sm:grid-cols-2">
        <label className={fieldClass}>{t("Declared command")}<Select value={value.command} onChange={event => patch({ command: event.target.value })}><option value="">{t("Choose a declared command")}</option>{commands.map(item => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select></label>
        <label className={fieldClass}>{t("Dispatch target")}<Select value={value.target} onChange={event => patch({ target: event.target.value })}><option value="">{t("Choose a ready external connection")}</option>{connections.data?.records.filter(item => item.state === "ready" && ["http", "odata"].includes(item.kind ?? "")).map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</Select></label>
        <label className={fieldClass}>{t("Stable business key")}<Select value={value.key} onChange={event => patch({ key: event.target.value })}><option value="$subject">{t("Subject record ID")}</option>{fields.filter(field => ["text", "reference", "choice"].includes(field.type) && field.readOnly).map(field => <option key={field.name} value={`record.${field.name}`}>{field.title || field.name}</option>)}</Select><span className="text-muted">{t("The same key is retained for every send attempt. Use the record ID or an immutable business identifier.")}</span></label>
        <label className={fieldClass}>{t("Business deadline (seconds)")}<Input type="number" min={1} max={604800} value={value.deadlineSeconds} onChange={event => patch({ deadlineSeconds: Number(event.target.value) })} /></label>
      </div>
      <Disclosure summary={t("Command parameters ({n})", { n: value.payload?.length ?? 0 })}>
        <div className="grid gap-3 pt-2">{value.payload?.map((item, index) => <div key={index} className="grid gap-2 rounded border border-border p-3">
          <label className={fieldClass}>{t("Parameter name")}<Input value={item.field} onChange={event => patch({ payload: value.payload!.map((entry, at) => at === index ? { ...entry, field: event.target.value } : entry) })} /></label>
          <ExpressionEditor value={item.expr ?? literalExpression("text")} kind={item.expr?.kind ?? "text"} choices={choices} lookup={source.entity} onChange={expr => patch({ payload: value.payload!.map((entry, at) => at === index ? { field: entry.field, expr } : entry) })} />
          <Button size="sm" variant="ghost" className="w-fit" onClick={() => patch({ payload: value.payload!.filter((_, at) => at !== index) })}>{t("Remove parameter")}</Button>
        </div>)}<Button size="sm" className="w-fit" onClick={() => patch({ payload: [...value.payload ?? [], { field: "", expr: literalExpression("text") }] })}>{t("Add command parameter")}</Button></div>
      </Disclosure>
      <fieldset className="grid gap-3 rounded border border-border p-3"><legend className="px-1 text-xs font-medium">{t("Required status readback")}</legend>
        <div className="grid gap-2 sm:grid-cols-2">
          <label className={fieldClass}>{t("Readback name")}<Input value={value.readback.name} onChange={event => read({ name: event.target.value })} /></label>
          <label className={fieldClass}>{t("Read status from")}<Select value={readKind} onChange={event => read({ source: event.target.value === "source" ? "" : undefined, observation: event.target.value === "observation" ? "" : undefined, record: event.target.value === "record" ? "" : undefined })}><option value="source">{t("Published data source")}</option><option value="observation">{t("Published observation source")}</option><option value="record">{t("Authorized status records")}</option></Select></label>
          {readKind === "record" ? <label className={fieldClass}>{t("Status record type")}<Select value={value.readback.record ?? ""} onChange={event => read({ record: event.target.value })}><option value="">{t("Choose an object type")}</option>{entities.map(entity => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select></label> : <label className={fieldClass}>{t(readKind === "observation" ? "Published observation source" : "Published data source")}<Select value={value.readback[readKind] ?? ""} onChange={event => read({ [readKind]: event.target.value })}><option value="">{t("Choose a source")}</option>{(readKind === "observation" ? observations : sources).data?.records.filter(item => item.state === "published" && (readKind !== "observation" || item.object === object)).map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</Select></label>}
          <label className={fieldClass}>{t("Readback period (seconds)")}<Input type="number" min={1} max={86400} value={value.readback.everySeconds} onChange={event => read({ everySeconds: Number(event.target.value) })} /></label>
          {readKind !== "observation" && <>
            <label className={fieldClass}>{t("Business key field in status source")}<Input value={value.readback.keyField ?? ""} onChange={event => read({ keyField: event.target.value })} /></label>
            <label className={fieldClass}>{t("Status field")}<Input value={value.readback.resultField ?? ""} onChange={event => read({ resultField: event.target.value })} /></label>
            <label className={fieldClass}>{t("Reason field (optional)")}<Input value={value.readback.reasonField ?? ""} onChange={event => read({ reasonField: event.target.value || undefined })} /></label>
          </>}
        </div>
        <div className="grid gap-2">{readings.map(([external, state], index) => <div key={index} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
          <label className={fieldClass}>{t("External status value")}<Input value={external} onChange={event => changeState(index, event.target.value, state)} /></label>
          <label className={fieldClass}>{t("Business dispatch state")}<Select value={state} onChange={event => changeState(index, external, event.target.value)}>{Object.entries(statusNames).map(([key, title]) => <option key={key} value={key}>{t(title)}</option>)}</Select></label>
          <Button size="sm" variant="ghost" className="self-end" onClick={() => read({ states: Object.fromEntries(readings.filter((_, at) => at !== index)) })}>{t("Remove")}</Button>
        </div>)}<Button size="sm" className="w-fit" onClick={() => read({ states: { ...value.readback.states, [nameOf("status", Object.keys(value.readback.states))]: "unknown" } })}>{t("Add status mapping")}</Button></div>
        <p className="text-xs text-muted">{t("A missing or unmapped status means outcome unknown. Sending a request does not mean the physical task has completed.")}</p>
      </fieldset>
      <div className="grid gap-2 sm:grid-cols-2">{(["onStuck", "onFail"] as const).map(key => <label key={key} className={fieldClass}>{t(key === "onStuck" ? "When stuck or overdue" : "When the target reports failure")}<Select value={value[key]} onChange={event => patch({ [key]: event.target.value })}>{Object.entries(policyNames).map(([policy, title]) => <option key={policy} value={policy} disabled={policy === "retry" && !safe || policy === "compensate" && !command?.compensate}>{t(title)}</option>)}</Select></label>)}
        <label className={fieldClass}>{t("Dispatch reviewer role")}<Select value={value.reviewerRole} onChange={event => patch({ reviewerRole: event.target.value })}>{roles.map(role => <option key={role}>{role}</option>)}</Select></label>
      </div>
      {command && <Tag label={t(safe ? command.idempotent ? "Idempotent command" : "Target deduplicates by business key" : "Unsafe to resend automatically")} tone={safe ? "info" : "warning"} />}
      {!safe && (value.onStuck === "retry" || value.onFail === "retry") && <p role="alert" className="text-xs text-danger">{t("Automatic retry is unavailable for this command. Choose human intervention or another declared policy before publishing.")}</p>}
    </>}
  </Panel>;
}
