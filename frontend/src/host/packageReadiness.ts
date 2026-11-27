import type { Api } from "./contract";
import { request } from "./request";
import { currentSession } from "./session";
import { t } from "../i18n";

export async function waitPackageReferences(id: string, version: string) {
  const session = currentSession();
  const path = `/v1/packages/${encodeURIComponent(id)}/readiness?version=${encodeURIComponent(version)}`;
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const current = currentSession();
    if (!session || current?.tenant !== session.tenant || current.token !== session.token) {
      throw new Error(t("The workspace changed"));
    }
    const status = await request<Api.PackageReadiness>(path, { signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())) });
    if (status.ready) return;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(t("The package is installed, but its reference records are still being prepared."));
}
