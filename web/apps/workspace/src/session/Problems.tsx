import type { EdgeClient, Api } from "@platform/kernel";
import { Button, Card, t } from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

export function Recovery({ client, token, tenant }: { client: EdgeClient; token: string; tenant: string }) {
  const queries = useQueryClient();
  const health = useQuery({ queryKey: [token, tenant, "recovery-health"],
    queryFn: () => client.get<Api.TenantHealth>("/v1/health", true), refetchInterval: 5000, retry: false });
  const [retrying, setRetrying] = useState(false);
  const [failure, setFailure] = useState("");
  const retry = async () => {
    setRetrying(true);
    setFailure("");
    try {
      const result = await client.call<{ error?: string }>("POST", "/v1/recovery/retry");
      if (!result.ok) {
        setFailure(result.body.error ?? t("Recovery is not available for this tenant."));
      } else {
        client.refreshLiveReads();
        await queries.invalidateQueries({ queryKey: [token, tenant, "me"] });
      }
      await health.refetch();
    } catch {
      setFailure(t("The host is unreachable."));
    } finally {
      setRetrying(false);
    }
  };
  const maintenance = health.data?.status === "suspended";
  return <main className="grid min-h-dvh place-items-center p-4">
    <Card className="w-full max-w-xl space-y-3 p-5">
      <h1 className="text-lg font-semibold">{t(maintenance ? "Tenant maintenance" : "Tenant recovery")}</h1>
      <p>{t(maintenance ? "This tenant is paused for maintenance. A host administrator can restore its required implementations and resume access in Host Console. Your saved records and history remain." : "This tenant is quarantined. Business inputs and work are stopped; other tenants can continue.")}</p>
      {health.data?.recoveryError && <p role="alert" className="break-all text-danger">{health.data.recoveryError}</p>}
      {health.error && <p role="alert">{t("Only a tenant administrator can view recovery diagnostics and retry.")}</p>}
      {!maintenance && <p>{t("Repair the underlying journal or restore a valid backup before retrying. Recovery rebuilds this tenant from its durable history without restarting healthy tenants.")}</p>}
      {failure && <p role="alert" className="break-all text-danger">{failure}</p>}
      {!maintenance && !health.error && <Button loading={retrying} disabled={!health.data} onClick={() => void retry()}>
        {t("Retry recovery")}
      </Button>}
    </Card>
  </main>;
}

export function SignInProblem({ problem, email, signingOut, error, onSignOut }: { problem: string; email?: string; signingOut: boolean; error: string; onSignOut: () => void }) {
  return <main className="grid min-h-dvh place-items-center p-4">
    <Card className="w-full max-w-md space-y-4 p-5">
      <h1 className="text-lg font-semibold">{problem}</h1>
      {email && <>
        <p className="text-sm text-muted">{t("Sign out and sign in with an account that belongs to this host.")}</p>
        {error && <p role="alert" className="text-sm text-danger">{error}</p>}
        <Button loading={signingOut} onClick={onSignOut}>{t("Sign out and switch account")}</Button>
      </>}
    </Card>
  </main>;
}

export { ReleaseInformation } from "./ReleaseInformation";
