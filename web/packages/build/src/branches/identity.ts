import { apiErrorMessage, type Api } from "@platform/kernel";
import type { Host } from "@platform/app";
import { t } from "@platform/ui";
import { BranchFailure, resourcePath } from "./api";
import { selectedBranch } from "./current";

type Identity = { id: string; name: string; title: string; archived?: boolean };

export async function readDraftIdentity(client: Host["client"], type: string, id: string): Promise<Identity | undefined> {
  const branch = selectedBranch(client);
  if (!branch) return (await client.get<{ record?: Identity }>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`)).record;
  const answer = await client.call<Api.BranchResourceView>("GET", resourcePath(branch, type, id));
  if (!answer.ok) throw new BranchFailure(apiErrorMessage(answer.body) ?? t("The branch request was refused."), answer.status);
  const spec = answer.body.branch as { name?: string; title?: string } | undefined;
  return spec?.name ? { id, name: spec.name, title: spec.title ?? spec.name, archived: answer.body.op === "delete" } : undefined;
}
