import type { Api } from "@platform/kernel";

export type PropertyRef = { object: Api.AssetRef; field: string };
export type ReferenceRelationRef = { kind: "reference"; object: Api.AssetRef; field: string; direction: "outbound" | "inbound" };
export type SemanticRelation = {
  ref: ReferenceRelationRef; target: Api.AssetRef; title: string; inverse?: string;
  field: Api.FieldInfo; owner: Api.Definition; targetDefinition: Api.Definition;
};
export type SemanticModelView = {
  objects: Api.Definition[]; relations: SemanticRelation[];
  usages: { owner: Api.Definition; resource: Api.AssetRef }[];
};

const key = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

/** A member-filtered view of the existing registry. Unknown/hidden reference
 * targets are omitted; this projection creates neither assets nor grants.
 * References remain field-backed relationships, with no inferred cardinality
 * or delete/uniqueness guarantees.
 */
export function semanticModelView(definitions: Api.Definition[]): SemanticModelView {
  const objects = definitions.filter((definition) => definition.ref.kind === "object" && definition.entity);
  const byType = new Map(objects.map((definition) => [definition.ref.name, definition]));
  const known = new Set(definitions.map((definition) => key(definition.ref)));
  const relations: SemanticRelation[] = [];
  for (const owner of objects) for (const field of owner.entity!.fields) {
    const targetDefinition = field.ref ? byType.get(field.ref) : undefined;
    if (!targetDefinition || !["reference", "references"].includes(field.type)) continue;
    relations.push({ ref: { kind: "reference", object: owner.ref, field: field.name, direction: "outbound" },
      target: targetDefinition.ref, title: field.title, inverse: field.inverse, field, owner, targetDefinition });
  }
  const usages = definitions.flatMap((owner) => {
    // Public definitions contain member-filtered descriptors; their typed
    // page/query bindings add precise usage when the compact Requires list
    // carries only the primary object. Never scan free text or hidden data.
    const refs = [...(owner.requires ?? [])];
    if (owner.page) {
      refs.push(owner.page.object, ...(owner.page.selections ?? []).map((selection) => selection.object));
      for (const section of owner.page.sections ?? []) if (section.object?.name) refs.push(section.object);
    }
    const target = owner.query?.object ?? owner.action?.target;
    if (target && byType.has(target)) refs.push(byType.get(target)!.ref);
    const distinct = new Map(refs.filter((ref) => known.has(key(ref))).map((ref) => [key(ref), ref]));
    return [...distinct.values()].map((resource) => ({ owner, resource }));
  });
  return { objects, relations, usages };
}

export const propertyKey = (ref: PropertyRef) => `${key(ref.object)}#${ref.field}`;
export const relationKey = (ref: ReferenceRelationRef) => `${propertyKey(ref)}:${ref.direction}`;
