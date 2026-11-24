import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ChevronDownIcon } from "@heroicons/react/24/outline";
import { useEffect, useMemo, useState } from "react";
import { apps, pathOf } from "../apps";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { PageTitle, useGutter } from "./frame";
import { ListView } from "./ListPage";
import { motionStyles } from "./motion";
import type { Dot } from "./Transitions";

const bindSchema = "platform.protocol.bind";

function useAppDirectory() {
  const applications = useRead<Api.ApplicationView[]>("/v1/applications");
  return useMemo(() => {
    const names = new Map<string, string>([["build", t("Build")]]);
    const links = new Map<string, string>();
    for (const app of apps) {
      names.set(app.id, t(app.label));
      links.set(app.id, pathOf(app));
    }
    if (applications.status === "ready") {
      for (const view of applications.data) {
        const key = view.name || view.app;
        const firstEntry = view.menu?.flatMap((group) => group.entries).find((entry) => entry.object);
        const href = key ? (view.dashboard?.length ? `/${key}/dashboard` : firstEntry ? `/${key}/${firstEntry.name}` : undefined) : undefined;
        if (view.name) {
          names.set(view.name, view.title || view.name);
          if (href) links.set(view.name, href);
        }
        if (view.app && (!view.name || view.name === view.app)) {
          names.set(view.app, view.title || view.app);
          if (href) links.set(view.app, href);
        }
      }
    }
    return {
      nameOf: (id: string) => names.get(id) ?? id,
      hrefOf: (id: string) => links.get(id),
    };
  }, [applications]);
}

function statusOf(protocol: Api.ProtocolInfo, nameOf: (id: string) => string): { label: string; dot: Dot } {
  if (protocol.bound) return { label: `${t("Active")}: ${nameOf(protocol.bound)}`, dot: "success" };
  if (protocol.providers.length === 0) return { label: t("No providers installed"), dot: "neutral" };
  if (protocol.declared) return { label: t("Not enabled — entries hidden"), dot: "warning" };
  return { label: t("Automatic"), dot: "accent" };
}

export function CapabilitiesList() {
  const protocols = useRead<Api.ProtocolInfo[]>("/v1/protocols");
  const { nameOf } = useAppDirectory();
  const empty = protocols.status === "ready" && protocols.data.length === 0;
  return (
    <ListView
      read={protocols}
      primary={empty ? <Button label={t("Browse marketplace")} variant="primary" href="/delivery/marketplace" /> : undefined}
      definition={{
        title: "Capabilities",
        about: "Manage shared capabilities for this workspace. Choose a provider to enable its features across connected applications.",
        rows: (items) => items,
        id: (item) => item.id,
        link: (item) => `/admin/capabilities/${encodeURIComponent(item.id)}`,
        columns: [
          { key: "title", header: "Capability", text: (item) => item.title || item.id },
          {
            key: "status",
            header: "Status",
            width: 240,
            text: (item) => statusOf(item, nameOf).label,
            dot: (item) => statusOf(item, nameOf).dot,
          },
          {
            key: "consumers",
            header: "Used by",
            width: 240,
            text: (item) => (item.consumers.length ? item.consumers.map(nameOf).join(", ") : t("None")),
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
  const { nameOf, hrefOf } = useAppDirectory();
  const toast = useToast();
  const protocol = protocols.status === "ready" ? protocols.data.find((candidate) => candidate.id === id) : undefined;
  const bindAction = actions.status === "ready" ? actions.data.find((action) => action.schema === bindSchema) : undefined;
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [showTechnical, setShowTechnical] = useState(false);
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
        title={t("Capability not found")}
        description={protocols.status === "ready" ? t("This capability is not installed in this workspace.") : protocols.reason}
        actions={<Button label={t("Back to capabilities")} variant="secondary" href="/admin/capabilities" />}
      />
    );
  }

  const chosen = selected && protocol.providers.includes(selected) ? selected : protocol.bound || protocol.providers[0] || "";
  const singleProvider = protocol.providers.length === 1 ? protocol.providers[0] : undefined;
  const consumerNames = protocol.consumers.map(nameOf).join(", ");
  const title = protocol.title || protocol.id;

  const runBind = async (provider: string) => {
    if (!bindAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(bindAction, protocol.id, { provider });
      reread();
      setConfirmUnbind(false);
      toast({
        body: provider
          ? t("Enabled {provider} for {capability}").replace("{provider}", nameOf(provider)).replace("{capability}", title)
          : t("Disabled {capability}").replace("{capability}", title),
      });
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
              <PageTitle title={title} back={{ href: "/admin/capabilities", label: t("Capabilities") }}>
                <HStack gap={3} vAlign="center" wrap="wrap">
                  <Token label={statusOf(protocol, nameOf).label} color={protocol.bound ? "green" : unboundWarning ? "orange" : "gray"} size="sm" />
                </HStack>
              </PageTitle>
              {protocol.description && <Text type="body" color="secondary">{protocol.description}</Text>}
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter}>
            <VStack gap={6}>
              {unboundWarning && (
                <VStack xstyle={motionStyles.bannerEnter}>
                  <Banner
                    status="warning"
                    title={t("This capability is not enabled yet")}
                    description={consumerNames
                      ? t("Features in {apps} that rely on this capability stay hidden until you enable a provider.").replace("{apps}", consumerNames)
                      : t("Entries that depend on this capability stay hidden until you enable a provider.")}
                  />
                </VStack>
              )}
              {failure && <VStack xstyle={[motionStyles.bannerEnter, motionStyles.shakeError]}><Banner status="error" title={t("Not saved")} description={failure} /></VStack>}

              <VStack gap={3}>
                <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
                  <Heading level={2}>{t("Provider")}</Heading>
                  {protocol.bound && bindAction && (
                    <Button label={t("Disable")} variant="secondary" size="sm" isDisabled={busy} onClick={() => setConfirmUnbind(true)} />
                  )}
                </HStack>
                {protocol.bound ? (
                  <Text type="body">{t("Currently provided by {provider}.").replace("{provider}", nameOf(protocol.bound))}</Text>
                ) : (
                  <Text type="supporting" color="secondary">{t("No provider is enabled yet.")}</Text>
                )}
                {protocol.providers.length === 0 ? (
                  <VStack gap={2}>
                    <Text type="supporting" color="secondary">{t("No providers are installed for this capability yet.")}</Text>
                    <HStack>
                      <Link href="/delivery/marketplace">{t("Browse marketplace")}</Link>
                    </HStack>
                  </VStack>
                ) : singleProvider && !protocol.bound ? (
                  <Card padding={4}>
                    <VStack gap={3}>
                      <Text type="body">
                        {consumerNames
                          ? t("Only one provider is installed: {provider}. Enable it to turn on related features in {apps}.").replace("{provider}", nameOf(singleProvider)).replace("{apps}", consumerNames)
                          : t("Only one provider is installed: {provider}. Enable it to activate this capability.").replace("{provider}", nameOf(singleProvider))}
                      </Text>
                      {bindAction && (
                        <HStack gap={2}>
                          <Button
                            label={t("Enable {provider}").replace("{provider}", nameOf(singleProvider))}
                            variant="primary"
                            isLoading={busy}
                            onClick={() => void runBind(singleProvider)}
                          />
                        </HStack>
                      )}
                    </VStack>
                  </Card>
                ) : protocol.providers.length > 1 ? (
                  <VStack gap={3}>
                    <RadioList
                      label={t("Available providers")}
                      value={chosen}
                      onChange={setSelected}
                      isDisabled={!bindAction || busy}
                    >
                      {protocol.providers.map((provider) => (
                        <RadioListItem
                          key={provider}
                          value={provider}
                          label={nameOf(provider)}
                          description={provider !== nameOf(provider) ? provider : undefined}
                        />
                      ))}
                    </RadioList>
                    {bindAction && (
                      <HStack gap={2}>
                        <Button
                          label={protocol.bound ? t("Switch provider") : t("Enable provider")}
                          variant="primary"
                          isLoading={busy}
                          isDisabled={!chosen || chosen === (protocol.bound ?? "")}
                          onClick={() => void runBind(chosen)}
                        />
                      </HStack>
                    )}
                  </VStack>
                ) : null}
              </VStack>

              <VStack gap={3}>
                <Heading level={2}>{t("Used by")}</Heading>
                {protocol.consumers.length > 0 ? (
                  <HStack gap={3} wrap="wrap" vAlign="center">
                    {protocol.consumers.map((consumer) => {
                      const target = hrefOf(consumer);
                      return target ? (
                        <Link key={consumer} href={target}>{nameOf(consumer)}</Link>
                      ) : (
                        <Token key={consumer} label={nameOf(consumer)} size="sm" />
                      );
                    })}
                  </HStack>
                ) : (
                  <Text type="supporting" color="secondary">{t("No applications use this capability yet.")}</Text>
                )}
              </VStack>

              <VStack gap={3}>
                <HStack hAlign="start">
                  <Button
                    icon={<HStack xstyle={[motionStyles.chevron, showTechnical && motionStyles.chevronOpen]}><Icon icon={ChevronDownIcon} size="sm" /></HStack>}
                    label={showTechnical ? t("Hide technical details") : t("Show technical details")}
                    variant="ghost"
                    size="sm"
                    onClick={() => setShowTechnical((open) => !open)}
                  />
                </HStack>
                <VStack
                  ref={(node) => { if (node) node.inert = !showTechnical; }}
                  aria-hidden={!showTechnical || undefined}
                  xstyle={[motionStyles.collapseGrid, showTechnical && motionStyles.collapseOpen]}
                >
                  <VStack xstyle={motionStyles.collapseInner}>
                    <MetadataList>
                      <MetadataListItem label={t("Contract")}>
                        <Text type="code" color="secondary">{protocol.id}</Text>
                      </MetadataListItem>
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
                </VStack>
              </VStack>
            </VStack>
          </LayoutContent>
        }
      />
      <AlertDialog
        isOpen={confirmUnbind}
        title={t("Disable {capability}?").replace("{capability}", title)}
        description={consumerNames
          ? t("Features in {apps} that depend on this capability will be hidden until enabled again. Existing data is not deleted.").replace("{apps}", consumerNames)
          : t("Entries in applications that depend on this capability will be hidden until enabled again. Existing data is not deleted.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Disable")}
        actionVariant="destructive"
        isActionLoading={busy}
        onOpenChange={(open) => !open && !busy && setConfirmUnbind(false)}
        onAction={() => void runBind("")}
      />
    </>
  );
}
