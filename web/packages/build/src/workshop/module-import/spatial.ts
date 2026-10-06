import type {Api} from "@platform/kernel";
import type {ImportTarget} from "./compile";
export const spatialSourceTypes=["Map","ImageAnnotation","Scene3D"];
export type SpatialImportBinding={migration:"original-spatial"|"";latitudeField?:string;longitudeField?:string;labelField?:string;fileVarId?:string;fileID?:string;sampleQuery?:Api.AssetBinding;sampleAssetField?:string;sampleTimeField?:string;mappingFields?:Record<string,string>};
export function spatialSampleQuery(b:SpatialImportBinding|undefined,target:ImportTarget):Api.NamedQuery|undefined{
 const binding=b?.sampleQuery;if(!binding||!binding.ref||binding.ref.kind!=="query"||typeof binding.sourceVersion!=="string")return;
 const d=target.definitions?.find(d=>d.ref.kind==="query"&&d.ref.app===binding.ref.app&&d.ref.name===binding.ref.name);return d?.queryVersions?.[binding.sourceVersion]??(d?.version===binding.sourceVersion?d.query:undefined);
}
