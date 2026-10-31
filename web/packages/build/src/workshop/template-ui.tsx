import { newId, useHost } from "@platform/app";
import { Button, Checkbox, Form, Input, PageHeader, Panel, Select, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { pageTemplates, templateDraft, type TemplateId } from "./templates";
import { useAuthoringModel } from "../ontology/authoring-model";

export function StudioTemplates({ initial, onCreated, onBusy, onClose, closeGuard }: { initial?: string; onCreated?: (target: { type: string; id: string }) => void; onBusy?: (busy: boolean) => void; onClose?: () => void; closeGuard?: { current: (() => void) | undefined } }) {
  const host = useHost();
  const model = useAuthoringModel();
  const { open } = useWorkspace();
  const [templateId, setTemplateId] = useState<TemplateId>((pageTemplates.find((item) => item.id === initial) ?? pageTemplates[0]!).id);
  const template = pageTemplates.find((item) => item.id === templateId) ?? pageTemplates[0]!;
  const [objectType, setObjectType] = useState("");
  const [childType, setChildType] = useState("");
  const [fields, setFields] = useState<string[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const lock = useRef(false);
  const request = useRef<ReturnType<typeof templateDraft> | undefined>(undefined);
  const [pending, setPending] = useState(false), [created, setCreated] = useState(false);
  const locked = busy || pending || created;
  useEffect(() => { onBusy?.(busy); return () => onBusy?.(false); }, [busy, onBusy]);
  const { markSaved, confirmOwnDiscard } = useUnsavedChanges(!created && (!!name || !!title || !!objectType || pending), () => {
    request.current = undefined; setPending(false); setName(""); setTitle(""); setObjectType(""); setChildType(""); setFields([]); setActions([]); setError("");
  }, busy);
  if (closeGuard) closeGuard.current = () => { if (!lock.current && onClose) confirmOwnDiscard(onClose); };
  const ontology = new Set((model.drafts.data?.records ?? []).filter((record) => !record.archived).map((record) => `build.${record.name}`));
  const objects = model.entities.filter((entity) => ontology.has(entity.type) && entity.fields.length > 0);
  const object = objects.find((entity) => entity.type === objectType);
  const offered = model.actions.filter((action) => action.target === objectType);
  const children = objects.flatMap((entity) => entity.fields.filter((field) => field.type === "reference" && field.ref === objectType).map((field) => ({ entity, relation: field.inverse })));
  const child = children.find((c) => c.entity.type === childType);
  const chooseObject = (type: string) => {
    setObjectType(type); setChildType(""); setError("");
    setActions(model.actions.filter((action) => action.target === type && action.schema !== `${type}.archive`).map((action) => action.schema));
    setFields(objects.find((entity) => entity.type === type)?.fields.filter((field) => field.type !== "lines").slice(0, 4).map((field) => field.name) ?? []);
  };
  const toggle = (values: string[], name: string, selected: boolean) => selected ? [...values, name] : values.filter((value) => value !== name);
  const valid = !!object && fields.length > 0 && /^[a-z][a-z0-9]*$/.test(name) && !!title.trim() && (!template.child || !!child);
  const [pageID]=useState(()=>newId("PAGE"));
  const create = async () => {
    if (lock.current || created || !host.can("build.page.create") || (!request.current && (!object || !valid))) return;
    if (!request.current && object) {
      const description = `${t(template.summary)}
${t("Created from template {template} (revision {revision}).", { template: template.id, revision: template.revision })}`;
      request.current = templateDraft(template.id, { object, name, title: title.trim(), fields: [...fields], actions: [...actions], description, child: child?.entity, relation: child?.relation });
    }
    const submitted = request.current;
    if (!submitted) return;
    lock.current = true; setBusy(true); setError("");
    try {
      const confirmed = await host.decide("build.page.create", { type: "build.page", id: pageID }, submitted, {
        expectedRevision: 0, onRefused: setError,
        onOutcome: entry => {
          const uncertain = entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_UNKNOWN";
          setPending(uncertain);
          if (entry.state === "SUBMISSION_STATE_REJECTED") request.current = undefined;
        },
      });
      if (!confirmed) return;
      request.current = undefined; setPending(false); setCreated(true); markSaved();
      if (onCreated) onCreated({ type: "build.page", id: pageID });
      else open({ view: "module", params: { page: pageID } });
    } catch (failure) {
      setPending(true); setError(failure instanceof Error ? failure.message : t("The draft could not be created. Retry its original request."));
    } finally { lock.current = false; setBusy(false); }
  };
  return <div className="grid max-w-5xl gap-4">
    <PageHeader title={t("Studio templates")} help={t("Start with an existing controlled page. Bind your object before creating its draft.")} />
    {!host.can("build.page.create") ? <Panel role="status" className="text-sm text-muted">{t("Creating a page draft requires the original build.page.create permission.")}</Panel> : <>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{pageTemplates.map((item) => <div key={item.id} role="button" tabIndex={0} onClick={() => { if (!locked) setTemplateId(item.id); }} onKeyDown={(e) => { if (!locked && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); setTemplateId(item.id); } }} aria-pressed={item.id === template.id} aria-disabled={locked}
        className={`grid cursor-pointer gap-1 rounded-lg border p-3 text-left ${item.id === template.id ? "border-primary bg-primary/5" : "border-border hover:bg-surface-raised"}`}>
        <span className="text-sm font-semibold">{t(item.title)}</span><span className="text-xs text-muted">{t(item.summary)}</span><span className="font-mono text-2xs text-muted">{item.id} · {item.revision}</span></div>)}</div>
      <Form className="grid gap-4" onSubmit={() => create()}>
        <Panel className="grid gap-3 p-4">
          <h3 className="text-sm font-semibold">{t("Required bindings")}</h3>
          <label className="grid gap-1 text-xs">{t("Object")}<Select value={objectType} onChange={(event) => chooseObject(event.target.value)} disabled={locked}>
            <option value="">{t("Choose an object")}</option>{objects.map((entity) => <option key={entity.type} value={entity.type}>{entity.title} · {entity.type}{model.drafts.data?.records.some(draft => !draft.archived && `build.${draft.name}` === entity.type) ? ` · ${t("Saved draft")}` : ""}</option>)}
          </Select></label>
          {template.child && object && <label className="grid gap-1 text-xs">{t("Child object")}<Select value={childType} onChange={(event) => setChildType(event.target.value)} disabled={locked}>
            <option value="">{t("Choose a child object")}</option>{children.map((c) => <option key={c.entity.type} value={c.entity.type}>{c.entity.title} · {c.entity.type}{c.relation ? ` (${c.relation})` : ""}</option>)}
          </Select>{children.length === 0 && <span className="text-muted">{t("No object refers to this one yet. Add a reference field on the child object first.")}</span>}</label>}
          {model.drafts.isError && <p role="alert" className="text-xs text-danger">{t("Saved object drafts could not be loaded.")}</p>}
          {model.drafts.isPending && <p role="status" className="text-xs text-muted">{t("Loading…")}</p>}
          {!model.drafts.isPending && !model.drafts.isError && objects.length === 0 && <p className="text-xs text-muted">{t("No object types with properties yet. Create an object type and add a property first.")}</p>}
          {object && <div className="grid gap-3 sm:grid-cols-2">
            <fieldset className="grid content-start gap-2"><legend className="mb-2 text-xs font-medium">{t("Visible fields")}</legend>
              {object.fields.map((field) => <Checkbox key={field.name} checked={fields.includes(field.name)} disabled={locked}
                onChange={(selected) => setFields(toggle(fields, field.name, selected))}>{field.title}</Checkbox>)}
              <p className="text-xs text-muted">{t("Choose at least one field. The host still filters unreadable fields.")}</p>
            </fieldset>
            <fieldset className="grid content-start gap-2"><legend className="mb-2 text-xs font-medium">{t("Offered actions")}</legend>
              {offered.map((action) => <Checkbox key={action.schema} checked={actions.includes(action.schema)} disabled={locked}
                onChange={(selected) => setActions(toggle(actions, action.schema, selected))}>{action.title}{action.draft ? ` · ${t("Saved draft")}` : ""}</Checkbox>)}
              <p className="text-xs text-muted">{t("Bind saved draft actions or actions already offered to you. Draft bindings are checked at joint publication; execution still requires permission.")}</p>
            </fieldset>
          </div>}
        </Panel>
        <Panel className="grid gap-3 p-4 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Page name")}<Input aria-label={t("Page name")} aria-describedby="template-page-name-help"
            value={name} onChange={(event) => setName(event.target.value.trim())} disabled={locked} placeholder="recordhandling" required />
            <span id="template-page-name-help" className="text-muted">{t("Use lowercase letters and digits; start with a letter.")}</span></label>
          <label className="grid gap-1 text-xs">{t("Page title")}<Input value={title} onChange={(event) => setTitle(event.target.value)} disabled={locked} required /></label>
        </Panel>
        {pending && <Panel role="alert" className="grid gap-2 text-sm"><p>{t("Page creation is unconfirmed. Its original ID and payload are retained; retry does not create a second page.")}</p>
          <code>{pageID}</code><Button variant="ghost" disabled={busy} onClick={() => confirmOwnDiscard(() => { request.current = undefined; setPending(false); })}>{t("Forget local request")}</Button></Panel>}
        {created && <Panel role="status" className="text-sm"><p>{t("The page draft was created. It is not published.")}</p><Button onClick={() => onCreated ? onCreated({ type: "build.page", id: pageID }) : open({ view: "module", params: { page: pageID } })}>{t("Open created page")}</Button></Panel>}
        {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
        <div className="flex flex-wrap items-center gap-3">{onClose && <Button variant="ghost" disabled={busy} onClick={() => confirmOwnDiscard(onClose)}>{t("Cancel")}</Button>}<Button type="submit" variant="primary" loading={busy} disabled={created || !pending && !valid}>{pending ? t("Retry original page creation") : t("Create draft in Studio")}</Button>
          <p className="text-xs text-muted">{t("Creates a draft only. Review bindings in the page editor, then test and publish through the original release route.")}</p></div>
      </Form>
    </>}
  </div>;
}
