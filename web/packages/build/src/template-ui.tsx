import { newId, useHost } from "@platform/app";
import { Button, Card, Checkbox, Form, Input, PageHeader, Panel, Select, t, useWorkspace } from "@platform/ui";
import { useState } from "react";
import { pageTemplates, recordHandlingDraft } from "./templates";

export function StudioTemplates({ initial }: { initial?: string }) {
  const host = useHost();
  const { open } = useWorkspace();
  const template = pageTemplates.find((item) => item.id === initial) ?? pageTemplates[0];
  const [objectType, setObjectType] = useState("");
  const [fields, setFields] = useState<string[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const objects = host.entities.filter((entity) => entity.fields.length > 0);
  const object = objects.find((entity) => entity.type === objectType);
  const offered = host.catalog.filter((action) => action.target === objectType);
  const chooseObject = (type: string) => {
    setObjectType(type); setActions([]); setError("");
    setFields(objects.find((entity) => entity.type === type)?.fields.filter((field) => field.type !== "lines").slice(0, 4).map((field) => field.name) ?? []);
  };
  const toggle = (values: string[], name: string, selected: boolean) => selected ? [...values, name] : values.filter((value) => value !== name);
  const valid = !!object && fields.length > 0 && /^[a-z][a-z0-9]*$/.test(name) && !!title.trim();
  const create = async () => {
    if (!object || !valid || busy || !host.can("build.page.create")) return;
    setBusy(true); setError("");
    try {
      const id = newId("PAGE");
      // Description is informative provenance. It remains editable like any
      // page description and is not a protected version or execution binding.
      const description = `${t(template.summary)}\n${t("Created from template {template} (revision {revision}).", { template: template.id, revision: template.revision })}`;
      if (await host.decide("build.page.create", { type: "build.page", id }, recordHandlingDraft(object, name, title.trim(), fields, actions, description),
        { expectedRevision: 0, onRefused: setError })) open({ view: "compose", params: { id } });
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("The draft could not be created. Try again."));
    } finally { setBusy(false); }
  };
  return <div className="grid max-w-5xl gap-4">
    <PageHeader title={t("Studio templates")} description={t("Start with an existing controlled page. Bind your object before creating its draft.")} />
    {!host.can("build.page.create") ? <Panel role="status" className="text-sm text-muted">{t("Creating a page draft requires the original build.page.create permission.")}</Panel> : <>
      <Card className="grid gap-1 p-4"><h2 className="text-base font-semibold">{t(template.title)}</h2><p className="text-sm text-muted">{t(template.summary)}</p>
        <p className="font-mono text-xs text-muted">{template.id} · {template.revision}</p></Card>
      <Form className="grid gap-4" onSubmit={() => void create()}>
        <Panel className="grid gap-3 p-4">
          <h3 className="text-sm font-semibold">{t("Required bindings")}</h3>
          <label className="grid gap-1 text-xs">{t("Object")}<Select value={objectType} onChange={(event) => chooseObject(event.target.value)} disabled={busy}>
            <option value="">{t("Choose an object")}</option>{objects.map((entity) => <option key={entity.type} value={entity.type}>{entity.title} · {entity.type}</option>)}
          </Select></label>
          {objects.length === 0 && <p className="text-xs text-muted">{t("No readable objects are available. Create or publish an object in Studio first.")}</p>}
          {object && <div className="grid gap-3 sm:grid-cols-2">
            <fieldset className="grid content-start gap-2"><legend className="mb-2 text-xs font-medium">{t("Visible fields")}</legend>
              {object.fields.map((field) => <Checkbox key={field.name} checked={fields.includes(field.name)} disabled={busy}
                onChange={(selected) => setFields(toggle(fields, field.name, selected))}>{field.title}</Checkbox>)}
              <p className="text-xs text-muted">{t("Choose at least one field. The host still filters unreadable fields.")}</p>
            </fieldset>
            <fieldset className="grid content-start gap-2"><legend className="mb-2 text-xs font-medium">{t("Offered actions")}</legend>
              {offered.map((action) => <Checkbox key={action.schema} checked={actions.includes(action.schema)} disabled={busy}
                onChange={(selected) => setActions(toggle(actions, action.schema, selected))}>{action.title}</Checkbox>)}
              <p className="text-xs text-muted">{t("Optional. Select from actions already offered to you; a template never grants access.")}</p>
            </fieldset>
          </div>}
        </Panel>
        <Panel className="grid gap-3 p-4 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Page name")}<Input aria-label={t("Page name")} aria-describedby="template-page-name-help"
            value={name} onChange={(event) => setName(event.target.value.trim())} disabled={busy} placeholder="recordhandling" required />
            <span id="template-page-name-help" className="text-muted">{t("Use lowercase letters and digits; start with a letter.")}</span></label>
          <label className="grid gap-1 text-xs">{t("Page title")}<Input value={title} onChange={(event) => setTitle(event.target.value)} disabled={busy} required /></label>
        </Panel>
        {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
        <div className="flex flex-wrap items-center gap-3"><Button type="submit" variant="primary" disabled={!valid || busy}>{busy ? t("Creating draft…") : t("Create draft in Studio")}</Button>
          <p className="text-xs text-muted">{t("Creates a draft only. Review bindings in the page editor, then test and publish through the original release route.")}</p></div>
      </Form>
    </>}
  </div>;
}
