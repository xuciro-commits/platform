import { useNewRecord } from "@platform/app";
import { Button, Input, Panel, Select, Textarea, t, useWorkspace } from "@platform/ui";
import { useState } from "react";
import type { Project } from "./project";
import type { OwnedResource } from "./resource-inventory";
import { refKey, resourceKinds, resourceRoute, type ResourceKindInfo } from "./resources";
import { studioRoute } from "./studio-model";

/** Optional guidance opens the original forms, editor drafts and release gate.
 * It has no generated definition format, progress store or publication path. */
export function StudioGuide({ project, owned, disabled, intent, onIntent, onCreated }: {
  project: Project; owned: OwnedResource[]; disabled: boolean; intent: string;
  onIntent: (value: string) => void;
  onCreated: (kind: ResourceKindInfo, target: { type: string; id: string }) => void;
}) {
  const { open } = useWorkspace();
  const kind = resourceKinds.find(kind => kind.kind === "object")!;
  const create = useNewRecord(kind.type, target => onCreated(kind, target));
  const [title, setTitle] = useState(""), [name, setName] = useState("");
  const objects = owned.filter(item => item.ref.kind === "object" && item.record);
  const [selected, select] = useState("");
  const object = objects.find(item => refKey(item.ref) === selected) ?? objects[0];
  return <div className="grid max-w-4xl gap-4 p-4" role="region" aria-label={t("Guided start")}>
    <div className="flex flex-wrap items-center gap-2"><h2 className="text-sm font-semibold">{t("Start from business intent")}</h2><Button size="sm" variant="ghost" onClick={() => open(studioRoute(project.id, "model"))}>{t("Skip guidance")}</Button></div>
    <p className="text-xs text-muted">{t("Enter at any step. Every suggestion opens the same draft and release tools used for manual building.")}</p>
    <label className="grid gap-1 text-xs">{t("Business intent")}<Textarea aria-label={t("Business intent")} rows={3} value={intent} disabled={disabled} onChange={event => onIntent(event.target.value)} /></label>
    <Panel className="grid gap-3 p-3">
      <strong className="text-sm">{t("Suggested model skeleton")}</strong>
      <p className="text-xs text-muted">{t("Start with one business object, describe its fields, then give it states and an action. Add relationships when another object is needed.")}</p>
      <div className="grid gap-2 sm:grid-cols-2"><label className="grid gap-1 text-xs">{t("Business object title")}<Input aria-label={t("Business object title")} value={title} onChange={event => setTitle(event.target.value)} /></label>
        <label className="grid gap-1 text-xs">{t("Business object name")}<Input aria-label={t("Business object name")} value={name} onChange={event => setName(event.target.value)} /></label></div>
      <Button className="w-fit" disabled={disabled || !create.available || !title.trim() || !/^[a-z][a-z0-9]*$/.test(name)} onClick={() => create.take(undefined, { title: title.trim(), name, description: intent })}>{t("Review suggested object draft")}</Button>
      {object && <Button className="w-fit" variant="ghost" onClick={() => open(resourceRoute(object.kind, object.record!.id, project.id))}>{t("Continue existing model")}</Button>}
    </Panel>
    <Panel className="grid gap-2 p-3"><strong className="text-sm">{t("Suggested behavior")}</strong><p className="text-xs text-muted">{t("Describe the allowed state change and its conditions in the object's original action editor.")}</p>
      {objects.length > 0 && <label className="grid gap-1 text-xs">{t("Business object")}<Select value={object ? refKey(object.ref) : ""} onChange={event => select(event.target.value)}>{objects.map(item => <option key={refKey(item.ref)} value={refKey(item.ref)}>{item.title}</option>)}</Select></label>}
      <Button className="w-fit" disabled={!object} onClick={() => object && open({ view: "object-type", params: { id: object.record!.id, application: project.id, tab: "lifecycle" } })}>{t("Design states and actions")}</Button></Panel>
    <Panel className="grid gap-2 p-3"><strong className="text-sm">{t("Suggested interface")}</strong><p className="text-xs text-muted">{t("Use a record workspace template, bind a saved object and its actions, then publish the model and page together.")}</p><Button className="w-fit" onClick={() => open({ view: "project", params: { id: project.id, area: "interface", section: "templates" } })}>{t("Design pages from templates")}</Button></Panel>
    <Panel className="grid gap-2 p-3"><strong className="text-sm">{t("Connect data and systems")}</strong><p className="text-xs text-muted">{t("Choose the connections, observations and flows this application needs. Site addresses and credentials stay in environment bindings.")}</p><Button className="w-fit" onClick={() => open(studioRoute(project.id, "assemble"))}>{t("Connect data and systems")}</Button></Panel>
    <Panel className="grid gap-2 p-3"><strong className="text-sm">{t("Verify and deliver")}</strong><p className="text-xs text-muted">{t("Review the application's dependent drafts, save one immutable candidate and activate it through the original release gate.")}</p><Button className="w-fit" onClick={() => open(studioRoute(project.id, "verify"))}>{t("Verify application")}</Button></Panel>
    {create.dialog}
  </div>;
}
