import type {Api} from "@platform/kernel";

/** Resolve the one frozen interface source through the original producer. */
type InterfaceSection=Pick<Api.Section,"id"|"widget"|"collectionVariable"|"recordVariable">;
export function interfaceQueryForSection(page:Pick<Api.Page,"document">&{sections?:readonly InterfaceSection[]},section:InterfaceSection):Api.PageQuery|undefined {
  const variables=page.document?.variables??{};
  const producer=section.collectionVariable?section:page.sections?.find(s=>s.id===variables[section.recordVariable??""]?.source?.section&&s.widget==="record-picker");
  const variable=variables[producer?.collectionVariable??""];
  if(variable?.mode!=="resource"||variable.source?.kind!=="plan")return;
  const query=page.document?.queries?.[variable.source.query??""];
  return query?.interface?query:undefined;
}

export function interfaceWindowSignature(name:string,binding:Api.AssetBinding,query:unknown):string {
  return JSON.stringify([{interface:name,binding},query]);
}
