import type {Entry} from "@platform/kernel";
/** One invocation's confirmation; another pending decision cannot confirm or refuse this one. */
export function confirmedDecision(entries:readonly Entry[],tenant:string,key:string):boolean {
 return entries.some(entry=>entry.submission.tenantId===tenant&&entry.submission.idempotencyKey===key&&entry.state==="SUBMISSION_STATE_CONFIRMED");
}
