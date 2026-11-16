import { FileInput } from "@astryxdesign/core/FileInput";
import { Text } from "@astryxdesign/core/Text";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {AttachedFile} from "./Records";

export type RecordUploaderProps={label:string;file?:File;currentFile?:Pick<AttachedFile,"id"|"name"|"size"|"contentType">;busy?:boolean;error?:string;enabled?:boolean;onFile?:(file:File)=>void;onUpload?:()=>void|Promise<void>;onClear?:()=>void};

export function RecordUploader({label,file,currentFile,busy=false,error,enabled=true,onFile,onUpload,onClear}:RecordUploaderProps) {
 const writable=enabled&&!busy&&!!onFile&&!!onUpload;
 return <section aria-label={label} className="grid min-w-0 gap-2">
  <FileInput label={label} mode="dropzone" value={file??null} onChange={(selection)=>{const next=Array.isArray(selection)?selection[0]:selection;if(next&&writable)onFile?.(next);else if(!next&&onClear)onClear();}} isDisabled={!writable} isLoading={busy} placeholder={t("Choose file")} description={t("Drop one file here or choose a file to attach to the confirmed record.")} status={error?{type:"error",message:error}:undefined} />
  {file&&<div className="grid min-w-0 gap-1 text-xs"><Text type="supporting">{t("Selected file: {name}",{name:file.name})}</Text><Text type="supporting">{file.type||t("Unknown media type")} · {t("{size} bytes",{size:file.size})}</Text><div className="flex flex-wrap gap-2"><Button size="sm" variant="primary" disabled={!writable} onClick={()=>{if(writable)void onUpload!();}}>{error?t("Retry attachment"):t("Upload and attach")}</Button>{onClear&&<Button size="sm" variant="ghost" disabled={!enabled||busy} onClick={()=>{if(enabled&&!busy)onClear();}}>{t("Clear selected file")}</Button>}</div></div>}
  {busy&&<Text type="supporting" role="status">{t("Uploading and confirming the attachment…")}</Text>}
  {currentFile&&<div className="grid min-w-0 gap-1 rounded-md border border-border p-2 text-xs"><Text type="supporting">{t("Confirmed attachment: {name}",{name:currentFile.name})}</Text><Text type="supporting">{currentFile.id} · {currentFile.contentType||t("Unknown media type")} · {t("{size} bytes",{size:currentFile.size})}</Text></div>}
 </section>;
}
