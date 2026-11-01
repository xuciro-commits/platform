import type { Host } from "@platform/app";
import { t } from "@platform/ui";
import { readDraftIdentity } from "../branches/identity";

export async function createdPageMembership(client: Host["client"], id: string, pages: string[]) {
  const record = await readDraftIdentity(client, "build.page", id);
  if (!record || record.id !== id || record.archived) throw new Error(t("The created page is unavailable. Its record was not replaced or recreated."));
  return { record, pages: [...new Set([...pages, record.name])] };
}
