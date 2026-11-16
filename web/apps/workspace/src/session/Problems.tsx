import type { EdgeClient, Api } from "@platform/api";
import { Button, Notice, t } from "@platform/ui";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { Heading } from "@astryxdesign/core/Heading";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
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
  return <Center height="100dvh" padding={4}>
    <Card padding={5} width="100%" maxWidth={576}>
      <VStack gap={3}>
        <Heading level={1}>{t(maintenance ? "Tenant maintenance" : "Tenant recovery")}</Heading>
        <Text as="p">{t(maintenance ? "This tenant is paused for maintenance. A host administrator can restore its required implementations and resume access in Host Console. Your saved records and history remain." : "This tenant is quarantined. Business inputs and work are stopped; other tenants can continue.")}</Text>
        {health.data?.recoveryError && <Notice tone="danger" message={health.data.recoveryError} />}
        {health.error && <Notice tone="warning" message={t("Only a tenant administrator can view recovery diagnostics and retry.")} />}
        {!maintenance && <Text as="p">{t("Repair the underlying journal or restore a valid backup before retrying. Recovery rebuilds this tenant from its durable history without restarting healthy tenants.")}</Text>}
        {failure && <Notice tone="danger" message={failure} />}
        {!maintenance && !health.error && <Button loading={retrying} disabled={!health.data} onClick={() => void retry()}>
          {t("Retry recovery")}
        </Button>}
      </VStack>
    </Card>
  </Center>;
}

export function SignInProblem({ problem, email, signingOut, error, onSignOut }: { problem: string; email?: string; signingOut: boolean; error: string; onSignOut: () => void }) {
  return <Center height="100dvh" padding={4}>
    <Card padding={5} width="100%" maxWidth={448}>
      <VStack gap={4}>
        <Heading level={1}>{problem}</Heading>
        {email && <>
          <Text as="p" type="supporting">{t("Sign out and sign in with an account that belongs to this host.")}</Text>
          {error && <Notice tone="danger" message={error} />}
          <Button loading={signingOut} onClick={onSignOut}>{t("Sign out and switch account")}</Button>
        </>}
      </VStack>
    </Card>
  </Center>;
}

export { ReleaseInformation } from "./ReleaseInformation";