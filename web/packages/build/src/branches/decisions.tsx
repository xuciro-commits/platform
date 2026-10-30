import { HostContext, type Host } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { notify, t } from "@platform/ui";
import { useContext, useMemo, useRef, type ReactNode } from "react";
import { resourceKinds } from "../projects/resources";
import { BranchFailure, resourcePath, useBranchActions } from "./api";
import { selectedBranch } from "./current";

const draftTypes = new Set([...resourceKinds.map(kind => kind.type), "build.app"]);
const onMain = new Set(["publish", "migrate"]);

export function branchRefusal(verb: string) {
  return verb === "publish" ? t("A branch is published by merging it. Use Save in the branch bar to review, propose and merge.")
    : t("This runs against main. Merge the branch first, then run it there.");
}

export function BranchDecisions({ children }: { children: ReactNode }) {
  const host = useContext(HostContext);
  const actions = useBranchActions(), latest = useRef(actions);
  latest.current = actions;
  const value = useMemo<Host | null>(() => {
    if (!host) return null;
    const decide: Host["decide"] = async (schema, target, payload, options) => {
      const branch = host.holds("build", "builder") ? selectedBranch(host.client) : undefined;
      const cut = schema.lastIndexOf("."), type = schema.slice(0, cut), verb = schema.slice(cut + 1);
      if (!branch || type !== target.type || !draftTypes.has(type)) return host.decide(schema, target, payload, options);
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
        if (!options?.quiet) notify.success(t("Saved to the branch."));
        return true;
      } catch (problem) {
        const reason = problem instanceof Error ? problem.message : String(problem);
        if (options?.onRefused) options.onRefused(reason); else notify.error(reason);
        return false;
      }
    };
    return { ...host, decide, simulationBranch: () => host.holds("build", "builder") ? selectedBranch(host.client) : undefined };
  }, [host]);
  return <HostContext.Provider value={value}>{children}</HostContext.Provider>;
}
