import { definitionPresentation, semanticModelView, useHost, type Definition, type SemanticRelation } from "@platform/app";
import { Checkbox, GroupedList, Panel, Tag, t } from "@platform/ui";
import { useSyncExternalStore } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import { refKey, systemDefinitionsForType } from "../projects/resources";

const visibility = new Map<string, boolean>();
const listeners = new Set<() => void>();
const subscribe = (changed: () => void) => { listeners.add(changed); return () => { listeners.delete(changed); }; };

/** A display choice for this signed-in member and tenant, shared by asset lists. */
export function useSystemDefinitionsVisibility() {
  const { me } = useHost();
  const key = JSON.stringify([me.tenantId, me.principalId]);
  const checked = useSyncExternalStore(subscribe, () => visibility.get(key) ?? false, () => false);
  return [checked, (value: boolean) => { visibility.set(key, value); listeners.forEach(changed => changed()); }] as const;
}

export function SystemDefinitionsToggle({ checked, onChange }: { checked: boolean; onChange: (value: boolean) => void }) {
  return <Checkbox className="w-fit text-xs" checked={checked} onChange={onChange}>{t("Show system definitions")}</Checkbox>;
}

export function SystemDefinitionReferences({ type, search, title, description }: { type: string; search?: string; title?: string; description?: string }) {
  const { definitions } = useHost();
  const [shown, setShown] = useSystemDefinitionsVisibility();
  return <div className="grid min-w-0 gap-3"><SystemDefinitionsToggle checked={shown} onChange={setShown} />
    {shown && <SystemDefinitionsList definitions={systemDefinitionsForType(definitions, type)}
      relations={type === "build.linktype" ? semanticModelView(definitions).relations.filter(relation => relation.ref.kind === "reference" && relation.owner.source === "code") : []}
      search={search} title={title} description={description} />}
  </div>;
}

/** Original declarations and reference fields are inspectable without creating drafts. */
export function SystemDefinitionsList({ definitions, relations = [], search = "", title = "System definitions", description }: { definitions: Definition[]; relations?: SemanticRelation[]; search?: string; title?: string; description?: string }) {
  const { me } = useHost(), { open } = useApplicationWorkspace();
  const items: ({ definition: Definition } | { relation: SemanticRelation })[] = [
    ...definitions.map(definition => ({ definition })), ...relations.map(relation => ({ relation })),
  ];
  const name = (item: typeof items[number]) => "definition" in item ? definitionPresentation(item.definition).title : item.relation.owner.entity!.title + " · " + item.relation.field.title;
  const key = (item: typeof items[number]) => "definition" in item ? refKey(item.definition.ref) : refKey(item.relation.owner.ref) + "#" + item.relation.field.name;
  const owner = (item: typeof items[number]) => "definition" in item ? item.definition.ref.app : item.relation.owner.ref.app;
  const text = (item: typeof items[number]) => name(item) + " " + key(item) + ("relation" in item ? " " + item.relation.targetDefinition.entity!.title + " " + item.relation.target.name : "");
  const shown = items.filter(item => text(item).toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  return <Panel aria-label={t(title)} title={t(title)} description={description ? t(description) : t("Read-only reference")} className="grid min-w-0 gap-2">
    <GroupedList items={shown} id={key} text={text} className="max-h-96 overflow-auto"
      groupings={[{ id: "owner", label: t("Application"), of: item => ({ id: owner(item), label: me.apps.find(app => app.id === owner(item))?.title ?? owner(item) }) }]}
      onSelect={item => {
        if ("relation" in item) open({ view: "object-type", params: { object: item.relation.owner.ref.name, tab: "links" } });
        else open({ view: "definition", params: { ...item.definition.ref, surface: "studio" } });
      }}
      row={item => <><span className="min-w-0 flex-1 break-words">{name(item)}{"relation" in item && <span className="block text-muted">{item.relation.field.name} → {item.relation.targetDefinition.entity!.title}</span>}</span><span className="hidden shrink-0 sm:inline-flex"><Tag label={t("relation" in item ? "Reference relationship" : "System definition")} /></span><span className="shrink-0 text-muted">v{"definition" in item ? item.definition.version : item.relation.owner.version}</span></>}
      empty={t("No system definitions are available in this category.")} />
  </Panel>;
}
