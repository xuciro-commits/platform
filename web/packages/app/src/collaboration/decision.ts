import type {Entry} from "@platform/kernel";
export function confirmedDecision(entries:readonly Entry[],tenant:string,key:string):boolean {
 return entries.some(entry=>entry.submission.tenantId===tenant&&entry.submission.idempotencyKey===key&&entry.state==="SUBMISSION_STATE_CONFIRMED");
}
