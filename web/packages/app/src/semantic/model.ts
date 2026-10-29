import type {Definition as WorkspaceDefinition} from "../index";
import type { Api } from "@platform/kernel";

export type PropertyRef = { object: Api.AssetRef; field: string };
export type ReferenceRelationRef = { kind: "reference"; object: Api.AssetRef; field: string; direction: "outbound" | "inbound" };
type LinkRelationRef = Omit<ReferenceRelationRef,"kind"> & {kind:"link-type";binding:Api.AssetBinding};
export type SemanticRelation = {
  definition?:WorkspaceDefinition;linkType?:Api.LinkType;
  ref: ReferenceRelationRef|LinkRelationRef; target: Api.AssetRef; title: string; inverse?: string;
  field: Api.FieldInfo; owner: WorkspaceDefinition; targetDefinition: WorkspaceDefinition;
};
export type SemanticModelView = {
  objects: WorkspaceDefinition[]; relations: SemanticRelation[];
  propertyTypes: SemanticPropertyType[];
};

export type SemanticPropertyType = { binding: Api.AssetBinding; property: Api.PropertyType; definition: WorkspaceDefinition };
export const assetBindingKey = (binding: Api.AssetBinding) => `${key(binding.ref)}@${binding.sourceVersion}`;

export function semanticPropertyTypes(definitions: WorkspaceDefinition[]): SemanticPropertyType[] {
  return definitions.filter(d => d.ref.kind === "property-type" && d.propertyType).flatMap(definition => {
    const versions = new Map(Object.entries(definition.propertyVersions ?? {}));
    versions.set(definition.version, definition.propertyType!);
    return [...versions].map(([sourceVersion, property]) => ({ binding: { ref: definition.ref, sourceVersion }, property, definition }));
  });
}

const key = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

export function semanticModelView(definitions: WorkspaceDefinition[]): SemanticModelView {
  const objects = definitions.filter((definition) => definition.ref.kind === "object" && definition.entity);
  const byType = new Map(objects.map((definition) => [definition.ref.name, definition]));
  const relations: SemanticRelation[] = [];
  for (const owner of objects) for (const field of owner.entity!.fields) {
    const targetDefinition = field.ref ? byType.get(field.ref) : undefined;
    if (!targetDefinition || !["reference", "references"].includes(field.type)) continue;
    const declared=definitions.filter(d=>d.linkType&&d.linkType.child.name===owner.ref.name&&d.linkType.parent.name===targetDefinition.ref.name&&d.linkType.via===field.name);
    if(declared.length)for(const definition of declared){const l=definition.linkType!,binding={ref:definition.ref,sourceVersion:definition.version};relations.push({ref:{kind:"link-type",object:owner.ref,field:field.name,direction:"outbound",binding},target:targetDefinition.ref,title:l.reverse,inverse:l.forward,field,owner,targetDefinition,definition,linkType:l});}
    else relations.push({ ref: { kind: "reference", object: owner.ref, field: field.name, direction: "outbound" },
      target: targetDefinition.ref, title: field.title, inverse: field.inverse, field, owner, targetDefinition });
  }
  return { objects, relations, propertyTypes: semanticPropertyTypes(definitions) };
}

export const propertyKey = (ref: PropertyRef) => `${key(ref.object)}#${ref.field}`;
export const relationKey = (ref: ReferenceRelationRef|LinkRelationRef) => ref.kind==="link-type"?`${key(ref.binding.ref)}@${ref.binding.sourceVersion}:${ref.direction}`:`${propertyKey(ref)}:${ref.direction}`;
