import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { useState } from "react";
import type { Api, Read } from "../host";
import { t } from "../i18n";
import { kindLabel, kindPlace, sourceMeta } from "./PackageCard";

function changeColor(kind: string): "green" | "blue" | "red" | "gray" {
  const normalized = kind.toLowerCase();
  if (normalized === "added") return "green";
  if (normalized === "changed") return "blue";
  if (normalized === "removed") return "red";
  return "gray";
}

function stepLabel(action: string, status: string) {
  if (action === "platform.package.install") return t("Install");
  if (action === "platform.package.upgrade") return t("Upgrade");
  if (action === "platform.package.downgrade") return t("Downgrade");
  if (action === "platform.package.rollback") return t("Roll back");
  return t(action || status);
}

export function PackageReviewContent({
  view,
  mode,
  review,
  preparation,
}: {
  view: Api.PackageView;
  mode: "install" | "manage";
  review: Read<Api.PackageReview>;
  preparation: Read<Api.PackagePreparation>;
}) {
  const [copied, setCopied] = useState(false);
  const { id, version, description, contributions } = view.descriptor;
  const source = sourceMeta(view.source);
  const installed = view.installed;
  const impact = review.status === "ready" ? review.data.impact : undefined;
  const changes = review.status === "ready" ? review.data.changes : [];
  const extensions = review.status === "ready" ? review.data.extensions : [];
  const steps = view.plan.steps ?? [];
  const digest = view.descriptor.artifactDigest || view.sourceDigest || installed?.artifact;
  const apps = preparation.status === "ready" && preparation.data.applications.length
    ? preparation.data.applications.map((app) => t(app.title || app.ref.name)).join(", ")
    : String(changes.filter((change) => change.ref.kind === "app").length);
  const prerequisites = [
    ...(view.descriptor.requires ?? []),
    ...steps.filter((step) => step.id !== id).map((step) => `${t(step.title || step.id)} (${step.id} · ${step.version})`),
  ];
  const uniquePrerequisites = [...new Set(prerequisites)];
  const roles = preparation.status === "ready" ? preparation.data.roles : [];

  const byKind = new Map<string, number>();
  for (const change of changes) {
    byKind.set(change.ref.kind, (byKind.get(change.ref.kind) ?? 0) + 1);
  }
  const changeSummary = [...byKind.entries()].map(([kind, count]) => `${t(kind)} ${count}`).join(" · ");
  const notableChanges = changes.filter((change) => (change.fields?.length ?? 0) > 0 || change.change.toLowerCase() === "removed");

  const copyDigest = () => {
    if (!digest) return;
    void navigator.clipboard?.writeText(digest).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    });
  };

  return (
    <VStack gap={4}>
      {view.source === "imported" && (
        <Banner status="warning" title={t("Local library")} description={t("Self-declared metadata from an external source")} />
      )}
      {description && <Text type="supporting" color="secondary">{t(description)}</Text>}

      <MetadataList columns="single" label={{ position: "start", width: 140 }}>
        <MetadataListItem label={t("Version")}>
          <Text type="body" hasTabularNumbers>{id} · {version}</Text>
        </MetadataListItem>
        {view.kind && (
          <MetadataListItem label={t("Package kind")}>
            <VStack gap={1}>
              <Token label={kindLabel(view.kind)} color="gray" size="sm" />
              <Text type="supporting" color="secondary">{kindPlace(view.kind)}</Text>
            </VStack>
          </MetadataListItem>
        )}
        <MetadataListItem label={t("Source")}>
          <Token label={source.label} color={source.color} size="sm" />
        </MetadataListItem>
        <MetadataListItem label={t("Namespace")}>
          <Text type="code" color="secondary">{installed?.space || view.space || view.descriptor.namespace}</Text>
        </MetadataListItem>
        {view.origin && (
          <MetadataListItem label={t("Origin")}>
            <Text type="code" color="secondary" maxLines={1}>{view.origin}</Text>
          </MetadataListItem>
        )}
        {digest && (
          <MetadataListItem label={t("Digest")}>
            <HStack gap={2} vAlign="center">
              <Tooltip content={digest}>
                <Text type="code" color="secondary" maxLines={1}>{digest.length > 26 ? `${digest.slice(0, 18)}…${digest.slice(-8)}` : digest}</Text>
              </Tooltip>
              <Button label={copied ? t("Copied") : t("Copy")} variant="ghost" size="sm" onClick={copyDigest} />
            </HStack>
          </MetadataListItem>
        )}
      </MetadataList>

      {mode === "install" && review.status === "ready" && (
        <>
          <Divider />
          <VStack gap={2}>
            <Heading level={4}>{t("Installation scope")}</Heading>
            <MetadataList columns="single" label={{ position: "start", width: 140 }}>
              <MetadataListItem label={t("Applications")}>
                <Text type="body">{apps}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("Dependencies")}>
                <Text type="body">{uniquePrerequisites.length ? uniquePrerequisites.join(", ") : t("No dependencies")}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("Roles")}>
                {roles.length > 0 ? (
                  <HStack gap={1.5} wrap="wrap" vAlign="center">
                    {roles.map((entry) => (
                      <Token
                        key={`${entry.app}:${entry.role}`}
                        label={entry.held ? `${entry.role} · ${t("Held")}` : entry.role}
                        color={entry.held ? "green" : "gray"}
                        size="sm"
                      />
                    ))}
                  </HStack>
                ) : (
                  <Text type="body">{t("No roles")}</Text>
                )}
              </MetadataListItem>
              <MetadataListItem label={t("Connections")}>
                <Text type="body" hasTabularNumbers>{preparation.status === "ready" ? preparation.data.connections.length : impact?.connections.length ?? 0}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("Existing records affected")}>
                <Text type="body" hasTabularNumbers>{impact?.outputs.reduce((sum, output) => sum + output.records, 0) ?? 0}</Text>
              </MetadataListItem>
            </MetadataList>
          </VStack>
        </>
      )}

      {mode === "install" && steps.length > 0 && (
        <>
          <Divider />
          <List header={t("Install plan")} hasDividers>
            {steps.map((step) => (
              <ListItem
                key={`${step.id}@${step.version}`}
                label={t(step.title || step.id)}
                description={`${step.id} · ${step.version}${step.problem ? ` · ${step.problem}` : ""}`}
                endContent={<Token label={stepLabel(step.action, step.status)} color={step.problem ? "red" : "blue"} size="sm" />}
              />
            ))}
          </List>
        </>
      )}

      {mode === "install" && (contributions?.length ?? 0) > 0 && (
        <>
          <Divider />
          <List header={t("Contributions")} hasDividers>
            {(contributions ?? []).map((item) => (
              <ListItem
                key={`${item.surface ?? "work"}:${item.view}`}
                label={item.title || item.view}
                description={`${item.kind || "view"} · ${item.surface || "work"}${item.app ? ` · ${item.app}` : ""}`}
              />
            ))}
          </List>
        </>
      )}

      {mode === "install" && preparation.status === "ready" && preparation.data.connections.length > 0 && (
        <>
          <Divider />
          <List header={t("Connections")} hasDividers>
            {preparation.data.connections.map((conn) => (
              <ListItem
                key={conn.ref.name}
                label={conn.title || conn.ref.name}
                description={conn.kind}
                endContent={<Token label={conn.ready ? t("Ready") : t("Not configured")} color={conn.ready ? "green" : "orange"} size="sm" />}
              />
            ))}
          </List>
        </>
      )}

      {mode === "install" && changes.length > 0 && (
        <>
          <Divider />
          <VStack gap={2}>
            <Heading level={4}>{`${t("Resource changes")} (${changes.length})`}</Heading>
            <Text type="supporting" color="secondary">{changeSummary}</Text>
            {notableChanges.length > 0 && (
              <List header={t("Field changes")} hasDividers>
                {notableChanges.map((change) => {
                  const fieldSummary = (change.fields ?? [])
                    .map((field) => `${field.change === "added" ? "+" : field.change === "removed" ? "-" : "~"} ${field.name}${field.before || field.after ? ` (${field.before ?? ""}${field.before && field.after ? " → " : ""}${field.after ?? ""})` : ""}`)
                    .join(", ");
                  const hasIncompatible = (change.fields ?? []).some((field) => field.incompatible);
                  return (
                    <ListItem
                      key={`${change.ref.kind}:${change.ref.name}`}
                      label={change.ref.name}
                      description={`${t(change.ref.kind)}${fieldSummary ? ` · ${fieldSummary}` : ""}`}
                      endContent={<Token label={t(change.change)} color={hasIncompatible ? "red" : changeColor(change.change)} size="sm" />}
                    />
                  );
                })}
              </List>
            )}
          </VStack>
        </>
      )}

      {mode === "install" && extensions.length > 0 && (
        <>
          <Divider />
          <List header={t("Extensions")} hasDividers>
            {extensions.map((ext) => (
              <ListItem
                key={ext.type}
                label={ext.type}
                description={ext.issue || ext.fields.join(", ")}
                endContent={<Token label={ext.issue ? t("Error") : t("OK")} color={ext.issue ? "red" : "green"} size="sm" />}
              />
            ))}
          </List>
        </>
      )}

      {mode === "manage" && (
        <>
          <Divider />
          <VStack gap={2}>
            <Heading level={4}>{t("Lifecycle")}</Heading>
            <MetadataList columns="single" label={{ position: "start", width: 140 }}>
              <MetadataListItem label={t("Installed version")}>
                <Text type="body" hasTabularNumbers>{installed?.version ?? t("Not installed")}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("State")}>
                <HStack gap={2} vAlign="center">
                  <Text type="body">{installed?.state || t("Unknown")}</Text>
                  {installed?.state && <Token label={t(installed.state)} color={installed.state === "active" ? "green" : installed.state === "draining" ? "orange" : "gray"} size="sm" />}
                </HStack>
              </MetadataListItem>
              <MetadataListItem label={t("Dependent packages")}>
                <Text type="body">{impact?.dependencies.length ? impact.dependencies.join(", ") : t("No dependents")}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("References retained")}>
                <Text type="body" hasTabularNumbers>{impact?.references.length ?? 0}</Text>
              </MetadataListItem>
              <MetadataListItem label={t("Business records retained")}>
                <Text type="body" hasTabularNumbers>{impact?.outputs.reduce((sum, output) => sum + (output.retained ? output.records : 0), 0) ?? 0}</Text>
              </MetadataListItem>
            </MetadataList>
          </VStack>
        </>
      )}
    </VStack>
  );
}
