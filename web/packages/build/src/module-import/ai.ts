import type {Api} from "@platform/kernel";
import type {ImportTarget} from "./compile";
export const aiSourceTypes=["AIPAnalyst","AIPGenerated","AIPChatbot"];
export type AIImportBinding={migration:"record-scoped-functions"|"";function:Api.AssetBinding;recordVarId?:string;replyField?:string;historyMigration?:"original-call-history";outputMigration?:"display-only"};
export function originalImportFunction(b:AIImportBinding|undefined,target:ImportTarget):Api.Definition|undefined{
 if(!b||b.migration!=="record-scoped-functions"||Object.keys(b).some(k=>!["migration","function","recordVarId","replyField","historyMigration","outputMigration"].includes(k))||!b.function||Object.keys(b.function).some(k=>!["ref","sourceVersion"].includes(k))||!b.function.ref||Object.keys(b.function.ref).some(k=>!["app","kind","name"].includes(k))||b.function.ref.kind!=="function")return;
 return target.definitions?.find(d=>d.ref.kind==="function"&&d.ref.app===b.function.ref.app&&d.ref.name===b.function.ref.name&&d.version===b.function.sourceVersion&&d.function);
}
