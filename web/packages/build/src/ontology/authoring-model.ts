import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { t, type EntityInfo } from "@platform/ui";
import type { Api } from "@platform/kernel";
import type { ObjectRecord } from "./object-model";
import { actionDestinations, actionStateInput } from "./process-rules";

type AuthoringObject = Pick<ObjectRecord, "name" | "title" | "fields"> & Partial<Pick<ObjectRecord,
  "states" | "actions" | "implements" | "extends" | "constraints" | "sets" | "versioning" | "versionPins"
>> & { plural?: string; description?: string };
export type AuthoringAction = { schema: string; title: string; target: string; draft?: boolean };

/** Saved drafts describe authoring choices only. They never install definitions
 * or claim that the member can execute the described actions. */
export function authoringObject(record: AuthoringObject, installed?: EntityInfo): EntityInfo {
  const type = `build.${record.name}`, fields = record.fields ?? [], states = record.states ?? [];
  return {
    ...installed, type, app: "build", title: record.title,
    plural: record.plural || installed?.plural || record.title,
    description: record.description,
    display: installed?.display || fields.find(field => field.type === "text")?.name || "id",
    standard: ["create", "edit", "archive"],
    implements: record.implements, extends: record.extends,
    constraints: record.constraints, sets: record.sets, versioning: record.versioning, versionPins: record.versionPins,
    fields: [
      ...fields.map((field): Api.FieldInfo => ({
        name: field.name, title: field.title, type: field.type as Api.FieldInfo["type"],
        ref: field.ref, inverse: field.inverse, required: field.required, stereotype: field.stereotype, stereotypes: field.stereotypes,
        readOnly: field.readOnly || !!(field.formula || field.formulaExpr || field.aliasOf) || record.versionPins?.some(pin => pin.version === field.name), aliasOf: field.aliasOf,
        property: field.property, aiInput: field.aiInput, sourceType: field.sourceType as Api.FieldInfo["sourceType"],
        search: field.search, read: field.read, write: field.write,
        choices: field.choices?.split(",").map(value => value.trim()).filter(Boolean),
      })),
      ...(states.length ? [{ name: "state", title: t("State"), type: "choice" as const, readOnly: true, choices: states.map(state => state.name) }] : []),
    ],
    lifecycle: states.length ? {
      field: "state", initial: states[0]!.name,
      states: states.map(state => ({ ...state, tone: state.tone as Api.State["tone"] })),
      transitions: (record.actions ?? []).map(action => ({
        name: action.name, schema: `${type}.${action.name}`, title: action.title,
        from: action.from, toInput: actionStateInput(action), to: actionDestinations(action),
      })),
    } : undefined,
  };
}

export function useAuthoringModel() {
  const host = useHost();
  const builder = host.holds("build", "builder");
  const query = useRecordInventory<ObjectRecord & { archived?: boolean }>("build.object", 1000, builder);
  // A disabled query can still return cached builder data. Do not expose that
  // answer to another role, including consumers of the returned draft inventory.
  const drafts = { ...query, data: builder ? query.data : undefined, isError: builder && query.isError };
  const entities = new Map(host.entities.map(entity => [entity.type, entity]));
  const actions = new Map<string, AuthoringAction>(host.catalog.map(action => [action.schema, action]));
  for (const draft of drafts.data?.records ?? []) {
    if (draft.archived) continue;
    const type = `build.${draft.name}`;
    entities.set(type, authoringObject(draft, entities.get(type)));
    for (const [schema, action] of actions) if (action.target === type) actions.delete(schema);
    for (const [name, title] of [["create", t("Create")], ["edit", t("Edit")], ["archive", t("Archive")]] as const) {
      const schema = `${type}.${name}`;
      actions.set(schema, { schema, title, target: type, draft: true });
    }
    for (const action of draft.actions ?? []) {
      const schema = `${type}.${action.name}`;
      actions.set(schema, { schema, title: action.title, target: type, draft: true });
    }
  }
  return { entities: [...entities.values()], entity: (type: string) => entities.get(type) ?? host.source.entity(type), actions: [...actions.values()], drafts };
}

/** Draft actions replace their object's installed actions for editing only.
 * All other blocks retain their original published source and version. */
export function useAuthoringCapabilities<T extends Api.CapabilityDescriptor = Api.CapabilityDescriptor>() {
  const builder = useHost().holds("build", "builder");
  const catalog = useReadQuery<T[]>("/v1/capabilities");
  const actions = useReadQuery<T[]>("/v1/build/drafts/actions", undefined, builder);
  const drafts = builder ? actions.data ?? [] : [];
  const targets = new Set(drafts.map(action => action.target));
  return {
    data: [...(catalog.data ?? []).filter(capability => capability.kind !== "action" || !capability.target || !targets.has(capability.target)), ...drafts],
    isLoading: catalog.isLoading || builder && actions.isLoading,
    isError: catalog.isError || builder && actions.isError,
    refetch: () => Promise.all([catalog.refetch(), ...(builder ? [actions.refetch()] : [])]),
  };
}
