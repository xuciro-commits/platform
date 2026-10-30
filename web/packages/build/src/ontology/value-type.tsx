import { useState, type ReactNode } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { Api } from "@platform/kernel";
import { semanticValueTypes, useHost } from "@platform/app";
import { Button, Checkbox, EmptyState, Input, Notice, PageHeader, PanelSection, PropertyList, Select, StructuredEditor, Tag, Textarea, Workbench, inputIssues, t } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, ResourceControls, WorkbenchMessage, savingState } from "../editor/workbench";

type Draft = Api.ValueType & { id: string; revision: number; state?: string; published?: string; version?: number; versions?: string[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", description: "", base: "text" });
const hydrate = (record: Draft): Draft => ({ ...empty(), ...record });
const bases = ["text", "longtext", "integer", "decimal", "date", "datetime"] as const;
const textual = (base: string) => base === "text" || base === "longtext";
const numeric = (base: string) => base === "integer" || base === "decimal";
const inputType = (base: string): Api.Field["type"] => base === "integer" ? "integer" : base === "decimal" ? "number" : base === "date" || base === "datetime" ? base : "string";

export function ValueTypes() {
  const { source, holds } = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder")) return <PageHeader title={t("Value types")} description={t("Only a builder can edit value types.")} />;
  return <div className="grid gap-3"><PageHeader title={t("Value types")} help={t("Publish a reusable base type with bounds, a pattern and listed values, and bind object fields to an exact version.")} actions={<Button variant="primary" onClick={() => open({ view: "value-type", params: { id: "new" } })}>{t("New value type")}</Button>} />
    <ResourceList source={source} type="build.valuetype" fields={["title", "name", "base", "version"]} onOpen={record => open({ view: "value-type", params: { id: record.id } })} /></div>;
}

function Labeled({ text, hint, children }: { text: string; hint?: string; children: ReactNode }) {
  return <label className="grid min-w-0 gap-1 text-xs"><span className="font-medium">{text}</span>{children}{hint && <span className="text-muted">{hint}</span>}</label>;
}

function NumberInput({ label, value, min, max, step, onChange }: { label: string; value?: number; min?: number; max?: number; step?: number; onChange: (value?: number) => void }) {
  return <Labeled text={label}><Input type="number" min={min} max={max} step={step} value={value ?? ""} onChange={event => onChange(event.target.value === "" ? undefined : event.target.valueAsNumber)} /></Labeled>;
}

function Constraints({ draft, patch }: { draft: Draft; patch: (change: Partial<Draft>) => void }) {
  const c = draft.constraints ?? {};
  const set = (change: Partial<Api.InputConstraints>) => {
    const next = { ...c, ...change };
    patch({ constraints: Object.fromEntries(Object.entries(next).filter(([, value]) => value !== undefined && value !== false)) as Api.InputConstraints });
  };
  if (draft.base === "date" || draft.base === "datetime") return <Notice tone="info" message={t("A date value type takes no bounds. Bounds between date fields belong to the object that owns them.")} />;
  return <div className="grid gap-3">
    {numeric(draft.base) && <div className="grid grid-cols-2 gap-3">
      <NumberInput label={t("Minimum")} value={c.min} step={draft.base === "integer" ? 1 : undefined} onChange={min => set({ min, exclusiveMin: min === undefined ? undefined : c.exclusiveMin })} />
      <NumberInput label={t("Maximum")} value={c.max} step={draft.base === "integer" ? 1 : undefined} onChange={max => set({ max, exclusiveMax: max === undefined ? undefined : c.exclusiveMax })} />
      <Checkbox disabled={c.min === undefined} checked={!!c.exclusiveMin} onChange={exclusiveMin => set({ exclusiveMin })}>{t("Exclude the minimum")}</Checkbox>
      <Checkbox disabled={c.max === undefined} checked={!!c.exclusiveMax} onChange={exclusiveMax => set({ exclusiveMax })}>{t("Exclude the maximum")}</Checkbox>
    </div>}
    {textual(draft.base) && <div className="grid grid-cols-2 gap-3">
      <NumberInput label={t("Minimum length")} value={c.minLength} min={0} max={4096} step={1} onChange={minLength => set({ minLength })} />
      <NumberInput label={t("Maximum length")} value={c.maxLength} min={0} max={4096} step={1} onChange={maxLength => set({ maxLength })} />
    </div>}
  </div>;
}

function Choices({ draft, patch }: { draft: Draft; patch: (change: Partial<Draft>) => void }) {
  const choices = draft.choices ?? [];
  return <div className="grid gap-2">
    {choices.length === 0 && <p className="text-xs text-muted">{t("No listed values. Any value that passes the other rules is accepted.")}</p>}
    {choices.map((choice, at) => <div key={at} className="flex items-center gap-1">
      <Input aria-label={t("Listed value")} value={choice} onChange={event => patch({ choices: choices.map((entry, index) => index === at ? event.target.value : entry) })} />
      <Button size="sm" variant="ghost" aria-label={t("Remove listed value")} onClick={() => patch({ choices: choices.filter((_, index) => index !== at) })}><Trash2 /></Button>
    </div>)}
    <div><Button size="sm" onClick={() => patch({ choices: [...choices, ""] })}><Plus />{t("Add a listed value")}</Button></div>
  </div>;
}

function TryValue({ draft }: { draft: Draft }) {
  const [text, setText] = useState("");
  const raw = text.trim();
  const value: unknown = raw === "" ? undefined : numeric(draft.base) ? Number(raw) : text;
  const field: Api.Field = { name: "value", type: inputType(draft.base), description: draft.title, required: true, constraints: { ...draft.constraints, pattern: undefined }, choices: draft.choices?.length ? draft.choices : undefined };
  const issues = value === undefined ? [] : inputIssues([field], { value });
  return <div className="grid gap-3 p-3">
    <Labeled text={t("Try a value")}><Input value={text} placeholder={draft.base === "date" ? "2026-01-31" : draft.base === "datetime" ? "2026-01-31T09:30:00Z" : undefined} onChange={event => setText(event.target.value)} /></Labeled>
    {value === undefined ? <p role="status" className="text-xs text-muted">{t("Enter a value to check it against this draft.")}</p>
      : issues.length === 0 ? <div role="status" className="flex items-center gap-2 text-xs"><Tag label={t("Passes")} tone="success" /></div>
      : <div role="status" className="grid gap-1 text-xs"><Tag label={t("Fails")} tone="danger" /><ul className="grid gap-0.5">{issues.map((issue, index) => <li key={index}>{issue.message}</li>)}</ul></div>}
    {draft.pattern && <p className="text-xs text-muted">{t("Pattern: checked on save")}</p>}
  </div>;
}

export function ValueTypeEditor({ id }: { id: string }) {
  const { definitions, holds } = useHost(), { open } = useApplicationWorkspace();
  const editor = useDefinitionDraft<Draft>({
    id, type: "build.valuetype", kind: "value-type", view: "value-type", empty, normalize: hydrate, joinApplication: false,
    failure: t("The value type could not be saved or loaded. Your draft is still here."),
    payload: ({ name, title, description, base, constraints, pattern, choices }) => ({ name, title, description, base, constraints: constraints ?? {}, pattern: pattern ?? "", choices: choices ?? [] }),
  });
  const { draft, query, dirty, busy, error, inputs, patch, perform, save, review, transition, discardChanges } = editor;
  const versions = semanticValueTypes(definitions).filter(v => v.binding.ref.app === "build" && v.binding.ref.name === draft.name);
  if (!holds("build", "builder")) return <Workbench storageKey="value-type" title={t("Value type")}><WorkbenchMessage>{t("Only a builder can edit value types.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="value-type" title={t("Value type")}><WorkbenchMessage>{query.isError ? t("The value type could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const locked = !!draft.published;
  const invalid = !!inputs?.invalid || !draft.name || !draft.title || !draft.description;
  const changeBase = (base: string) => patch({ base: base as Draft["base"], constraints: {}, pattern: "", choices: [] });
  return <Workbench storageKey="value-type" crumbs={[{ label: t("Value types"), onClick: () => open({ view: "value-type" }) }]} title={draft.title || t("New value type")}
    status={<DraftStatus state={draft.state ?? "draft"} />} saving={savingState(dirty, busy, error || undefined)}
    actions={<ResourceControls type="build.valuetype" record={draft} dirty={dirty} busy={busy} invalid={invalid} empty={!draft.id}
      onSave={() => void perform(save)} onReview={() => void perform(review)} onInstall={() => void perform(() => transition("publish"))} onCancel={discardChanges} route={{ view: "value-type", params: { id } }} />}
    right={{ label: t("Value type inspector"), tabs: [
      { id: "try", title: t("Try a value"), content: <TryValue draft={draft} /> },
      { id: "versions", title: t("Versions"), badge: versions.length, content: versions.length
        ? <ul className="divide-y divide-border">{versions.map(v => <li key={v.binding.sourceVersion} className="grid gap-1 p-3 text-xs"><PropertyList items={[[t("Version"), <span className="font-mono">{v.binding.sourceVersion}</span>], [t("Type"), t(v.value.base)], [t("Title"), v.value.title]]} /></li>)}</ul>
        : <EmptyState className="p-6" title={t("No published version yet.")} hint={t("Publishing keeps this version for the fields that bind to it.")} /> },
    ] }}>
    {error && <Notice tone="danger" role="alert" message={error} />}
    <StructuredEditor {...editor.structured}>
      <fieldset disabled={busy} className="grid min-h-0 min-w-0 flex-1 content-start gap-1 overflow-auto p-1">
        <PanelSection title={t("Definition")}>
          <div className="grid max-w-xl gap-3 px-1">
            <Labeled text={t("Value type name")} hint={locked ? t("A published value type keeps its identity and base type.") : undefined}><Input className="font-mono" disabled={locked} value={draft.name} onChange={event => patch({ name: event.target.value })} /></Labeled>
            <Labeled text={t("Value type title")}><Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></Labeled>
            <Labeled text={t("Value type description")}><Textarea value={draft.description} onChange={event => patch({ description: event.target.value })} /></Labeled>
            <Labeled text={t("Base type")}><Select disabled={locked} value={draft.base} onChange={event => changeBase(event.target.value)}>{bases.map(base => <option key={base} value={base}>{t(base)}</option>)}</Select></Labeled>
          </div>
        </PanelSection>
        <PanelSection title={t("Constraints")}><div className="max-w-xl px-1"><Constraints draft={draft} patch={patch} /></div></PanelSection>
        {textual(draft.base) && <PanelSection title={t("Pattern")}>
          <div className="grid max-w-xl gap-1 px-1">
            <Input className="font-mono" aria-label={t("Pattern")} value={draft.pattern ?? ""} onChange={event => patch({ pattern: event.target.value })} />
            <p className="text-xs text-muted">{t("An RE2 regular expression the whole text must match; anchor it with ^ and $. It is checked on save, not in this preview.")}</p>
          </div>
        </PanelSection>}
        {textual(draft.base) && <PanelSection title={t("Listed values")}><div className="max-w-xl px-1"><Choices draft={draft} patch={patch} /></div></PanelSection>}
      </fieldset>
    </StructuredEditor>
  </Workbench>;
}
