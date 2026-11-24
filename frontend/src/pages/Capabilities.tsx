import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { useEffect, useMemo, useState } from "react";
import { apps } from "../apps";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { PageTitle, useGutter } from "./frame";
import { ListView } from "./ListPage";
import type { Dot } from "./Transitions";

const bindSchema = "platform.protocol.bind";

function useAppNames() {
  const applications = useRead<Api.ApplicationView[]>("/v1/applications");
  return useMemo(() => {
    const names = new Map<string, string>();
    for (const app of apps) names.set(app.id, t(app.label));
    if (applications.status === "ready") {
      for (const view of applications.data) {
        if (view.app) names.set(view.app, view.title || view.app);
        if (view.name) names.set(view.name, view.title || view.name);
      }
    }
    return (id: string) => names.get(id) ?? id;
  }, [applications]);
}

function statusOf(protocol: Api.ProtocolInfo, nameOf: (id: string) => string): { label: string; dot: Dot } {
  if (protocol.bound) return { label: `${t("Bound")}: ${nameOf(protocol.bound)}`, dot: "success" };
  if (protocol.providers.length === 0) return { label: t("No implementations available"), dot: "neutral" };
  if (protocol.declared) return { label: t("Unbound — entries hidden"), dot: "warning" };
  return { label: t("Automatic"), dot: "accent" };
}

export function CapabilitiesList() {
  const protocols = useRead<Api.ProtocolInfo[]>("/v1/protocols");
  const nameOf = useAppNames();
  const empty = protocols.status === "ready" && protocols.data.length === 0;
  return (
    <ListView
      read={protocols}
      primary={empty ? <Button label={t("Browse marketplace")} variant="primary" href="/delivery/marketplace" /> : undefined}
      definition={{
        title: "Capabilities",
        about: "Capability contracts come from installed capability packages. Choose which implementation serves each contract in this workspace.",
        rows: (items) => items,
        id: (item) => item.id,
        link: (item) => `/admin/capabilities/${encodeURIComponent(item.id)}`,
        columns: [
          { key: "title", header: "Capability", text: (item) => item.title || item.id },
          { key: "id", header: "Contract", kind: "code", width: 220, text: (item) => item.id },
          {
            key: "status",
            header: "Status",
            width: 240,
            text: (item) => statusOf(item, nameOf).label,
            dot: (item) => statusOf(item, nameOf).dot,
          },
          {
            key: "consumers",
            header: "Consumers",
            kind: "number",
            width: 120,
            text: (item) => String(item.consumers.length),
            value: (item) => item.consumers.length,
          },
        ],
      }}
    />
  );
}

export function CapabilityPage({ id }: { id: string }) {
  const gutter = useGutter();
  const protocols = useRead<Api.ProtocolInfo[]>("/v1/protocols");
  const actions = useRead<Api.Action[]>("/v1/actions");
  const nameOf = useAppNames();
  const protocol = protocols.status === "ready" ? protocols.data.find((candidate) => candidate.id === id) : undefined;
  const bindAction = actions.status === "ready" ? actions.data.find((action) => action.schema === bindSchema) : undefined;
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!protocol) return;
    setSelected(protocol.bound || protocol.providers[0] || "");
  }, [protocol?.id, protocol?.bound, protocol?.providers.join(",")]);

  if (protocols.status === "loading") {
    return (
      <VStack gap={3} padding={gutter}>
        <Skeleton height={40} />
        <Skeleton height={180} />
      </VStack>
    );
  }
  if (!protocol) {
    return (
      <EmptyState
        headingLevel={1}
        title={t("Capability contract not found")}
        description={protocols.status === "ready" ? t("This capability contract is not installed in this workspace.") : protocols.reason}
        actions={<Button label={t("Back to capabilities")} variant="secondary" href="/admin/capabilities" />}
      />
    );
  }

  const runBind = async (provider: string) => {
    if (!bindAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(bindAction, protocol.id, { provider });
      setConfirmUnbind(false);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const contractActions = protocol.contract?.Actions ?? [];
  const reads = protocol.contract?.Reads ?? protocol.reads ?? [];
  const events = protocol.contract?.Events ?? protocol.events ?? [];
  const unboundWarning = Boolean(protocol.declared && !protocol.bound);

  return (
    <>
      <Layout
        height="fill"
        contentWidth={1260}
        header={
          <LayoutHeader padding={0} hasDivider>
            <VStack gap={2} padding={gutter} paddingBlockStart={3} paddingBlockEnd={4}>
              <PageTitle title={protocol.title || protocol.id} back={{ href: "/admin/capabilities", label: t("Capabilities") }}>
                <HStack gap={3} vAlign="center" wrap="wrap">
                  <Text type="code" color="secondary">{protocol.id}</Text>
                  <Token label={statusOf(protocol, nameOf).label} color={protocol.bound ? "green" : unboundWarning ? "orange" : "gray"} size="sm" />
                </HStack>
              </PageTitle>
              {protocol.description && <Text type="body" color="secondary">{protocol.description}</Text>}
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={6}>
              {unboundWarning && (
                <Banner
                  status="warning"
                  title={t("This capability is unbound")}
                  description={t("Entries that depend on it are hidden until an implementation is bound.")}
                />
              )}
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}

              <VStack gap={3}>
                <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
                  <Heading level={2}>{t("Implementation binding")}</Heading>
                  {protocol.bound && bindAction && (
                    <Button label={t("Unbind")} variant="secondary" size="sm" isDisabled={busy} onClick={() => setConfirmUnbind(true)} />
                  )}
                </HStack>
                {protocol.bound ? (
                  <Text type="body">{t("Currently bound to")} {nameOf(protocol.bound)} ({protocol.bound})</Text>
                ) : (
                  <Text type="supporting" color="secondary">{t("No implementation is currently bound.")}</Text>
                )}
                {protocol.providers.length === 0 ? (
                  <VStack gap={2}>
                    <Text type="supporting" color="secondary">{t("No implementations are installed for this contract yet.")}</Text>
                    <HStack>
                      <Link href="/delivery/marketplace">{t("Browse marketplace")}</Link>
                    </HStack>
                  </VStack>
                ) : (
                  <VStack gap={3}>
                    <RadioList
                      label={t("Available implementations")}
                      value={selected}
                      onChange={setSelected}
                      isDisabled={!bindAction || busy}
                    >
                      {protocol.providers.map((provider) => (
                        <RadioListItem
                          key={provider}
                          value={provider}
                          label={nameOf(provider)}
                          description={provider}
                        />
                      ))}
                    </RadioList>
                    {bindAction && (
                      <HStack gap={2}>
                        <Button
                          label={t("Bind")}
                          variant="primary"
                          isLoading={busy}
                          isDisabled={!selected || selected === (protocol.bound ?? "")}
                          onClick={() => void runBind(selected)}
                        />
                      </HStack>
                    )}
                  </VStack>
                )}
              </VStack>

              <VStack gap={3}>
                <Heading level={2}>{t("Contract")}</Heading>
                <MetadataList>
                  <MetadataListItem label={t("Actions")}>
                    {contractActions.length > 0 ? (
                      <VStack gap={1}>
                        {contractActions.map((action) => (
                          <Text key={action.schema} type="body">
                            {action.title ? `${action.title} (${action.schema})` : action.schema}
                          </Text>
                        ))}
                      </VStack>
                    ) : protocol.actions.length > 0 ? (
                      <Text type="code">{protocol.actions.join(", ")}</Text>
                    ) : (
                      <Text type="supporting" color="secondary">{t("None")}</Text>
                    )}
                  </MetadataListItem>
                  <MetadataListItem label={t("Reads")}>
                    {reads.length > 0 ? (
                      <Text type="code">{reads.join(", ")}</Text>
                    ) : (
                      <Text type="supporting" color="secondary">{t("None")}</Text>
                    )}
                  </MetadataListItem>
                  <MetadataListItem label={t("Events")}>
                    {events.length > 0 ? (
                      <VStack gap={1}>
                        {events.map((event) => (
                          <Text key={event.name} type="body">
                            {event.title ? `${event.title} (${event.name})` : event.name}
                          </Text>
                        ))}
                      </VStack>
                    ) : (
                      <Text type="supporting" color="secondary">{t("None")}</Text>
                    )}
                  </MetadataListItem>
                </MetadataList>
              </VStack>

              <VStack gap={3}>
                <Heading level={2}>{t("Consumers")}</Heading>
                {protocol.consumers.length > 0 ? (
                  <HStack gap={2} wrap="wrap">
                    {protocol.consumers.map((consumer) => (
                      <Token key={consumer} label={nameOf(consumer)} size="sm" />
                    ))}
                  </HStack>
                ) : (
                  <Text type="supporting" color="secondary">{t("No applications use this capability yet.")}</Text>
                )}
              </VStack>
            </VStack>
          </LayoutContent>
        }
      />
      <AlertDialog
        isOpen={confirmUnbind}
        title={t("Unbind capability?")}
        description={t("After unbinding, entries in applications that depend on this capability will disappear until an implementation is bound again.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Unbind")}
        onOpenChange={(open) => !open && setConfirmUnbind(false)}
        onAction={() => void runBind("")}
      />
    </>
  );
}
