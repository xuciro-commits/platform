import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, endSession, reasonOf, request, startSession, useRead, useSession } from "../host";
import { currentSession, requireSession } from "../host/session";
import { t } from "../i18n";
import { navigate } from "../router";

export function WorkspaceEntry() {
  const session = useSession();
  const identity = useRead<Api.IdentityEntry>("/v1/auth/me");
  const [name, setName] = useState("");
  const [id, setId] = useState("");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const enter = (tenant: string) => {
    if (!session) return;
    startSession({ token: session.token, tenant });
    navigate("/home/overview");
  };
  const accept = async (tenant: string) => {
    if (busy) return;
    setBusy(true);
    setFailure("");
    try {
      const workspace = await request<Api.MeTenant>(`/v1/auth/invitations/${encodeURIComponent(tenant)}/accept`, { body: {} });
      enter(workspace.id);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally { setBusy(false); }
  };
  const create = async () => {
    const active = currentSession();
    if (!active || busy || !/^[a-z][a-z0-9-]{1,31}$/.test(id) || !name.trim()) return;
    setBusy(true);
    setFailure("");
    try {
      const workspace = await request<Api.MeTenant>("/v1/auth/workspaces", { body: { id, name: name.trim() } satisfies Api.WorkspaceCreate });
      requireSession(active);
      enter(workspace.id);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  return <Center axis="both" minHeight="100dvh" padding={6}>
    <Card width="100%" maxWidth={480} padding={5}>
      <VStack gap={4} hAlign="stretch">
        <Text type="display-1" as="h1">{t("Your workspaces")}</Text>
        {identity.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
        {(identity.status === "error" || identity.status === "refused") && <Banner status="error" title={identity.reason} />}
        {identity.status === "ready" && <>
          <Text type="supporting" color="secondary">{identity.data.subject.replace(/^user:/, "")}</Text>
          {identity.data.invitations.map((invitation) => <Button key={invitation.id} label={t("Accept invitation to {workspace}").replace("{workspace}", invitation.name || invitation.id)} variant="primary" isDisabled={busy} onClick={() => void accept(invitation.id)} />)}
          {identity.data.workspaces.map((workspace) => <Button key={workspace.id} label={workspace.name || workspace.id} variant="secondary" isDisabled={busy} onClick={() => enter(workspace.id)} />)}
          {!identity.data.workspaces.length && <Text type="body">{t("You do not belong to a workspace yet.")}</Text>}
          {identity.data.canCreate && <>
            <Text type="body" weight="bold" as="h2">{t("Create your workspace")}</Text>
            <TextInput label={t("Name")} value={name} onChange={setName} isRequired isDisabled={busy} />
            <TextInput label={t("Workspace ID")} value={id} onChange={(value) => setId(value.toLowerCase().trim())} isRequired isDisabled={busy} />
            <Button label={t("Create")} variant="primary" isDisabled={busy || !name.trim() || !/^[a-z][a-z0-9-]{1,31}$/.test(id)} isLoading={busy} onClick={() => void create()} />
          </>}
          {!identity.data.canCreate && !identity.data.workspaces.length && <Text type="supporting">{t("Ask your workspace administrator for an invitation.")}</Text>}
        </>}
        {failure && <Banner status="error" title={failure} />}
        <HStack hAlign="end"><Button label={t("Sign out")} variant="secondary" isDisabled={busy} onClick={endSession} /></HStack>
      </VStack>
    </Card>
  </Center>;
}
