import {useState} from "react";
import {Button,Dialog,Panel,t} from "@platform/ui";
import {pageCompatibility,type ProfileUpgrade} from "./compatibility";
import type {PageDraft} from "./draft";

export function CompatibilityReview({draft,busy,onClose,onApply,onLocate}:{draft:PageDraft;busy:boolean;onClose:()=>void;onApply:(review:ProfileUpgrade)=>void;onLocate:(id:string)=>void}){
 const [review,setReview]=useState(()=>pageCompatibility(draft));
 const [baseline,setBaseline]=useState(()=>JSON.stringify(draft));
 const stale=baseline!==JSON.stringify(draft);
 return <Dialog open onOpenChange={open=>!open&&!busy&&onClose()} title={t("Review page compatibility")} wide>
  <div className="grid gap-3">
   <p>{t("Draft UI profile")}: <code>{review.from}</code> → <code>{review.to}</code></p>
   <p className="text-sm text-muted">{t("An upgrade changes this draft's UI profile. Widget configurations, bindings and layout remain intact. Save and review a new release candidate to deliver it; existing releases retain their original bytes.")}</p>
   {!review.supported&&<Panel role="alert">{t("This page format or UI profile is unsupported. No automatic migration is available.")}</Panel>}
   {review.issues.map((issue,n)=><Panel key={n} role="alert"><code>{issue.section} · {issue.widget} · {issue.version??"—"}</code><p>{t(issue.code==="unknown-widget"?"The widget is not registered. Preserve its configuration and resolve it before upgrading.":"The widget configuration version is unsupported. A declared configuration migration is required.")}</p>{issue.section&&<Button onClick={()=>onLocate(issue.section)}>{t("Locate widget")}</Button>}</Panel>)}
   <p>{t("Registered widget configurations")}: {draft.sections.length-review.issues.length} / {draft.sections.length}</p>
   <Panel className="grid gap-2 text-sm"><strong>{t("Query activation and budgets")}</strong>
    <p>{t("Active query plans")}: {review.current.active.size} → {review.target.active.size}</p>
    <p>{t("Active record window budget")}: {review.current.total} → {review.target.total}</p>
    <p>{t("Declared record window budget")}: {review.current.declared} → {review.target.declared}</p>
    <p className="text-muted">{t("The current profile defers plans used only by stored widgets. Other query declarations remain active; permissions and host validation still apply.")}</p>
    {!review.target.valid&&<p role="alert">{t("The target profile exceeds the page query budgets. Adjust the declarations before upgrading.")}</p>}
   </Panel>
   {stale&&<Panel role="alert">{t("The draft changed after this review. Refresh the review before applying it.")} <Button onClick={()=>{setReview(pageCompatibility(draft));setBaseline(JSON.stringify(draft));}}>{t("Refresh compatibility review")}</Button></Panel>}
   {review.from===review.to&&<p role="status">{t("This draft already uses the current UI profile.")}</p>}
   <div className="flex justify-end gap-2"><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button><Button variant="primary" disabled={busy||stale||!review.upgrade} onClick={()=>review.upgrade&&onApply(review.upgrade)}>{t("Apply profile upgrade to draft")}</Button></div>
  </div>
 </Dialog>;
}
