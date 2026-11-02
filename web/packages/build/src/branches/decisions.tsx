import { resourceSaveEdit, useWorkingState } from "../session/WorkingState";
import { HostContext, type Host } from "@platform/app";
import { apiErrorMessage, type Api, type Entry } from "@platform/kernel";
import { notify, t } from "@platform/ui";
import { useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { resourceKinds } from "../projects/resources";
import { BranchFailure, resourcePath, useBranchActions } from "./api";
import { selectedBranch } from "./current";
import { ConflictDialog, ConflictSettlers, editConflict, type EditConflict } from "./conflict";

const draftTypes = new Set([...resourceKinds.map(kind => kind.type), "build.app"]);
const onMain = new Set(["publish", "migrate"]);

export function branchRefusal(verb: string) {
  return verb === "publish" ? t("A branch is published by merging it. Use Save in the branch bar to review, propose and merge.")
    : t("This runs against main. Merge the branch first, then run it there.");
}

export function BranchDecisions({ children, project }: { children: ReactNode; project?: string }) {
  const host = useContext(HostContext), working=useWorkingState();
  const actions = useBranchActions(), latest = useRef(actions);
  latest.current = actions;
  const [settlers] = useState(() => new Map<string, () => void>()), [conflict, setConflict] = useState<EditConflict>();
  const value = useMemo<Host | null>(() => {
    if (!host) return null;
    const mainDecide: Host["decide"] = async (schema,target,payload,options) => {
      const cut=schema.lastIndexOf("."),type=schema.slice(0,cut),verb=schema.slice(cut+1);
      if (draftTypes.has(type) && type===target.type && (verb==="create" || verb==="edit")) {
        const edit=resourceSaveEdit({schema,target,payload,expectedRevision:options?.expectedRevision},project,options?.schemaVersion);
        const accepted=await host.decide(edit.schema,target,edit.payload,{...options,schemaVersion:host.action(edit.schema)?.schemaVersion});
        if(accepted)await working?.clear();
        return accepted;
      }
      return host.decide(schema,target,payload,options);
    };
    const decide: Host["decide"] = async (schema, target, payload, options) => {
      const branch = host.holds("build", "builder") ? selectedBranch(host.client) : undefined;
      const cut = schema.lastIndexOf("."), type = schema.slice(0, cut), verb = schema.slice(cut + 1);
      const settle = settlers.get(`${target.type}/${target.id}`);
      if (!branch && verb === "edit" && type === target.type && draftTypes.has(type) && options?.expectedRevision !== undefined && settle) {
        let refusal: string | undefined, outcome: Entry | undefined;
        if (await mainDecide(schema, target, payload, { quiet: true, ...options, onRefused: reason => { refusal = reason; }, onOutcome: entry => { outcome = entry; options.onOutcome?.(entry); } })) return true;
        const found = outcome?.outcome === "ERROR_CODE_CONFLICT" ? await editConflict(host, type, target.id, options.expectedRevision, payload) : undefined;
        if (!found) { const reason = refusal ?? t("The host refused this edit."); if (options.onRefused) options.onRefused(reason); else notify.error(reason); return false; }
        const keep = await new Promise<"mine" | "theirs">(resolve => setConflict({ ...found, resolve }));
        const saved = keep === "mine" && await mainDecide(schema, target, payload, { quiet: true, ...options, expectedRevision: found.revision });
        setConflict(undefined);
        if (keep === "mine" && !saved) return false;
        if (saved) notify.success(t("Your version is saved over theirs."));
        settle();
        return false;
      }
      if (!branch || type !== target.type || !draftTypes.has(type)) return mainDecide(schema, target, payload, verb === "edit" && type === target.type && draftTypes.has(type) ? { quiet: true, ...options } : options);
      if (onMain.has(verb)) { const reason = branchRefusal(verb); if (options?.onRefused) options.onRefused(reason); else notify.error(reason); return false; }
      if (verb !== "create" && verb !== "edit" && verb !== "archive" && verb !== "compile") return host.decide(schema, target, payload, options);
      const actions = latest.current;
      try {
        if (verb === "compile") await actions.compile(branch, type, target.id);
        else if (verb === "archive") await actions.save(branch, type, target.id, { op: "delete" });
        else if (verb === "create") await actions.save(branch, type, target.id, { op: "create", spec: Object.fromEntries(Object.entries((payload ?? {}) as Record<string, unknown>).filter(([, value]) => value !== null && value !== undefined)) });
        else {
          const answer = await host.client.call<Api.BranchResourceView>("GET", resourcePath(branch, type, target.id));
          if (!answer.ok) throw new BranchFailure(apiErrorMessage(answer.body) ?? t("The branch request was refused."), answer.status);
          const spec: Record<string, unknown> = { ...(answer.body.branch as Record<string, unknown> | undefined) };
          for (const [name, change] of Object.entries((payload ?? {}) as Record<string, unknown>)) { if (change === null || change === undefined) delete spec[name]; else spec[name] = change; }
          await actions.save(branch, type, target.id, { op: "update", spec });
        }
        await working?.clear();
        if (!options?.quiet && verb !== "edit" && verb !== "create") notify.success(t("Saved to the branch."));
        return true;
      } catch (problem) {
        const reason = problem instanceof Error ? problem.message : String(problem);
        if (options?.onRefused) options.onRefused(reason); else notify.error(reason);
        return false;
      }
    };
    return { ...host, decide, simulationBranch: () => host.holds("build", "builder") ? selectedBranch(host.client) : undefined };
  }, [host, settlers, project, working]);
  return <HostContext.Provider value={value}><ConflictSettlers settlers={settlers}>{children}</ConflictSettlers><ConflictDialog conflict={conflict} /></HostContext.Provider>;
}
