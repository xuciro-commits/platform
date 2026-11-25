import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useState } from "react";
import { type Api, reread } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";

export type ConformanceCase = { name: string; ok: boolean; detail?: string };
export type ConformanceRun = { at?: string; passed?: boolean; fingerprint?: string; cases?: ConformanceCase[] };
export type ConnectorSetupRecord = {
  id: string;
  revision: number;
  implementation: string;
  address?: string;
  secret?: string;
  state?: string;
  requested?: boolean;
  last?: ConformanceRun;
};

const styles = stylex.create({
  caseRow: {
    padding: "8px 10px",
    borderRadius: radiusVars["--radius-element"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    backgroundColor: colorVars["--color-background-surface"],
  },
  configBox: {
    padding: 12,
    borderRadius: radiusVars["--radius-element"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    backgroundColor: colorVars["--color-background-muted"],
  },
});

function stateLabel(state?: string) {
  if (state === "passed") return { label: t("Tests passed"), color: "green" as const };
  if (state === "unconfigured") return { label: t("Not set up"), color: "gray" as const };
  if (state === "stale") return { label: t("Changed since the last test"), color: "orange" as const };
  return { label: t("Not tested or failed"), color: "gray" as const };
}

export function ConnectorSetupCard({
  connector,
  setup,
  credentials,
  actions,
  active,
  busy: parentBusy,
  canBind,
  onEnable,
}: {
  connector: Api.ImplementationInfo;
  setup?: ConnectorSetupRecord;
  credentials: Api.CredentialInfo[];
  actions: Api.Action[];
  active: boolean;
  busy: boolean;
  canBind: boolean;
  onEnable: () => void;
}) {
  const createAction = actions.find((a) => a.schema === "build.connectorsetup.create");
  const editAction = actions.find((a) => a.schema === "build.connectorsetup.edit");
  const testAction = actions.find((a) => a.schema === "build.connectorsetup.test");
  const canConfigure = Boolean(setup ? editAction : createAction);

  const [expanded, setExpanded] = useState(!setup || connector.state !== "passed");
  const [address, setAddress] = useState(setup?.address || connector.origins[0] || "");
  const [secret, setSecret] = useState(setup?.secret || "");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    setAddress(setup?.address || connector.origins[0] || "");
    setSecret(setup?.secret || "");
  }, [setup?.id, setup?.revision, setup?.address, setup?.secret, connector.origins.join(",")]);

  const badge = active ? { label: t("Active"), color: "green" as const } : stateLabel(connector.state);
  const dirty = !setup || address !== (setup.address || "") || secret !== (setup.secret || "");
  const matchingCredentials = credentials.filter((cred) => !connector.credential || cred.kind === connector.credential);
  const credentialOptions = (matchingCredentials.length > 0 ? matchingCredentials : credentials).map((cred) => ({
    value: cred.id,
    label: cred.title ? `${cred.title} (${cred.id})` : cred.id,
  }));

  const saveSetup = async (): Promise<{ id: string; revision: number } | undefined> => {
    const action = setup ? editAction : createAction;
    if (!action) return undefined;
    const id = setup ? setup.id : crypto.randomUUID();
    const payload: Record<string, unknown> = { implementation: connector.id, address };
    if (secret) payload.secret = secret;
    await send(action, id, payload, setup ? setup.revision : 0);
    return { id, revision: setup ? setup.revision + 1 : 1 };
  };

  const handleSave = async () => {
    setBusy(true);
    setFailure("");
    setNotice("");
    try {
      await saveSetup();
      reread();
      setNotice(t("Connector setup saved. Run conformance tests to verify before enabling."));
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const handleTest = async () => {
    if (!testAction) return;
    setBusy(true);
    setFailure("");
    setNotice("");
    try {
      let target = setup ? { id: setup.id, revision: setup.revision } : undefined;
      if (dirty) target = await saveSetup();
      if (!target) return;
      await send(testAction, target.id, {}, target.revision);
      reread();
      setNotice(t("Conformance tests requested. Refresh in a moment to view the latest results."));
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const cases = setup?.last?.cases ?? [];
  const passedCount = cases.filter((item) => item.ok).length;

  return (
    <Card padding={4}>
      <VStack gap={3}>
        <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
          <VStack gap={0.5}>
            <Text type="body" weight="medium">{connector.title || connector.id}</Text>
            <Text type="supporting" color="secondary">{connector.origins.join(", ")}</Text>
          </VStack>
          <HStack gap={2} vAlign="center" wrap="wrap">
            <Token label={badge.label} color={badge.color} size="sm" />
            {canConfigure && (
              <Button
                label={expanded ? t("Hide setup") : t("Configure")}
                variant="ghost"
                size="sm"
                onClick={() => setExpanded((open) => !open)}
              />
            )}
            {canBind && !active && (
              <Button
                label={t("Enable")}
                variant="primary"
                size="sm"
                isDisabled={parentBusy || busy || connector.state !== "passed"}
                onClick={onEnable}
              />
            )}
          </HStack>
        </HStack>

        {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
        {notice && <Banner status="info" title={notice} />}
        {setup?.requested && (
          <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
            <Text type="supporting" color="secondary">{t("Conformance tests are running in the background…")}</Text>
            <Button label={t("Refresh")} variant="ghost" size="sm" onClick={() => reread()} />
          </HStack>
        )}

        {expanded && canConfigure && (
          <VStack gap={3} xstyle={styles.configBox}>
            <HStack gap={3} vAlign="end" wrap="wrap">
              <Selector
                label={t("Outbound origin")}
                value={address}
                onChange={setAddress}
                options={connector.origins.map((origin) => ({ value: origin, label: origin }))}
              />
              {connector.credential && (
                <Selector
                  label={t("Credential")}
                  value={secret}
                  onChange={setSecret}
                  options={credentialOptions}
                />
              )}
            </HStack>
            {connector.credential && (
              <HStack gap={2} vAlign="center" wrap="wrap">
                <Text type="supporting" color="secondary">
                  {t("Requires a {kind} credential allowing consumer implementation/{id}.").replace("{kind}", connector.credential).replace("{id}", connector.id)}
                </Text>
                <Link href="/admin/credentials">{t("Manage credentials")}</Link>
              </HStack>
            )}
            <HStack gap={2} vAlign="center" wrap="wrap">
              <Button
                label={t("Save setup")}
                variant="secondary"
                size="sm"
                isLoading={busy}
                isDisabled={!address || !dirty}
                onClick={() => void handleSave()}
              />
              {testAction && (
                <Button
                  label={dirty ? t("Save and run tests") : t("Run conformance tests")}
                  variant="primary"
                  size="sm"
                  isLoading={busy}
                  isDisabled={!address || (Boolean(connector.credential) && !secret)}
                  onClick={() => void handleTest()}
                />
              )}
            </HStack>
          </VStack>
        )}

        {cases.length > 0 && (
          <VStack gap={2}>
            <HStack gap={2} vAlign="center">
              <Text type="supporting" weight="medium">
                {t("Last conformance test: {passed}/{total} passed").replace("{passed}", String(passedCount)).replace("{total}", String(cases.length))}
              </Text>
              <StackItem size="fill" />
              <Button label={t("Refresh")} variant="ghost" size="sm" onClick={() => reread()} />
            </HStack>
            {cases.map((item) => (
              <HStack key={item.name} gap={2} vAlign="center" xstyle={styles.caseRow} wrap="wrap">
                <Token label={item.ok ? t("Passed") : t("Failed")} color={item.ok ? "green" : "red"} size="sm" />
                <Text type="body" weight="medium">{item.name}</Text>
                {item.detail && <Text type="supporting" color="secondary">{item.detail}</Text>}
              </HStack>
            ))}
          </VStack>
        )}
      </VStack>
    </Card>
  );
}
