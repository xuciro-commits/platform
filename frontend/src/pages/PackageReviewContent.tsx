import { Banner } from "@astryxdesign/core/Banner";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import type { Api, Read } from "../host";
import { t } from "../i18n";
import { sourceMeta } from "./PackageCard";

function changeColor(kind: string): "green" | "blue" | "red" | "gray" {
  if (kind === "added") return "green";
  if (kind === "changed") return "blue";
  if (kind === "removed") return "red";
  return "gray";
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
  const { id, version, description, contributions } = view.descriptor;
  const source = sourceMeta(view.source);
  const installed = view.installed;
  const impact = review.status === "ready" ? review.data.impact : undefined;
  const changes = review.status === "ready" ? review.data.changes : [];
  const extensions = review.status === "ready" ? review.data.extensions : [];
  const steps = view.plan.steps ?? [];
  const digest = view.descriptor.artifactDigest || view.sourceDigest || installed?.artifact;
  const apps = preparation.status === "ready" && preparation.data.applications.length
    ? preparation.data.applications.map((app) => app.title || app.ref.name).join(", ")
    : String(changes.filter((change) => change.ref.kind === "app").length);
  const roles = preparation.status === "ready" && preparation.data.roles.length
    ? preparation.data.roles.map((entry) => entry.role).join(", ")
    : t("None");

  return (
    <VStack gap={4}>
      {view.source === "imported" && (
        <Banner status="warning" title={t("Local library")} description={t("Self-declared metadata from an external source")} />
      )}
      {description && <Text type="body" color="secondary">{description}</Text>}

      <MetadataList columns="single" label={{ position: "start", width: 120 }}>
        <MetadataListItem label={t("Version")}>
          <Text type="body">{id} · {version}</Text>
        </MetadataListItem>
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
            <Text type="code" color="secondary" maxLines={1}>{digest}</Text>
          </MetadataListItem>
        )}
      </MetadataList>

      {mode === "install" && review.status === "ready" && (
        <VStack gap={2}>
          <Text type="body" weight="bold">{t("Installation scope")}</Text>
          <Text type="body">{t("Applications")}: {apps}</Text>
          <Text type="body">{t("Dependencies")}: {impact?.dependencies.length ? impact.dependencies.join(", ") : t("None")}</Text>
          <Text type="body">{t("Roles")}: {roles}</Text>
          <Text type="body">{t("Connections")}: {preparation.status === "ready" ? preparation.data.connections.length : impact?.connections.length ?? 0}</Text>
          <Text type="body">{t("Existing records affected")}: {impact?.outputs.reduce((sum, output) => sum + output.records, 0) ?? 0}</Text>
        </VStack>
      )}

      {mode === "install" && steps.length > 0 && (
        <List header={t("Install plan")} hasDividers>
          {steps.map((step) => (
            <ListItem
              key={`${step.id}@${step.version}`}
              label={step.title || step.id}
              description={`${step.id} · ${step.version}${step.problem ? ` · ${step.problem}` : ""}`}
              endContent={<Token label={t(step.action || step.status)} color={step.problem ? "red" : "blue"} size="sm" />}
            />
          ))}
        </List>
      )}

      {mode === "install" && (contributions?.length ?? 0) > 0 && (
        <List header={t("Contributions")} hasDividers>
          {(contributions ?? []).map((item) => (
            <ListItem
              key={`${item.surface ?? "work"}:${item.view}`}
              label={item.title || item.view}
              description={`${item.kind || "view"} · ${item.surface || "work"}${item.app ? ` · ${item.app}` : ""}`}
            />
          ))}
        </List>
      )}

      {mode === "install" && preparation.status === "ready" && preparation.data.connections.length > 0 && (
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
      )}

      {mode === "install" && changes.length > 0 && (
        <List header={`${t("Resource changes")} (${changes.length})`} hasDividers>
          {changes.slice(0, 12).map((change) => (
            <ListItem
              key={`${change.ref.kind}:${change.ref.name}`}
              label={change.ref.name}
              description={`${t(change.ref.kind)}${change.before || change.after ? ` · ${change.before ?? "—"} → ${change.after ?? "—"}` : ""}`}
              endContent={<Token label={t(change.change)} color={changeColor(change.change)} size="sm" />}
            />
          ))}
        </List>
      )}

      {mode === "install" && extensions.length > 0 && (
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
      )}

      {mode === "manage" && (
        <VStack gap={2}>
          <Text type="body" weight="bold">{t("Lifecycle")}</Text>
          <HStack gap={2} vAlign="center">
            <Text type="body">{t("Installed version")}: {installed?.version ?? t("Not installed")}</Text>
            {installed?.state && <Token label={t(installed.state)} color={installed.state === "active" ? "green" : "gray"} size="sm" />}
          </HStack>
          <Text type="body">{t("State")}: {installed?.state || t("Unknown")}</Text>
          <Text type="body">{t("References retained")}: {impact?.references.length ?? 0}</Text>
          <Text type="body">{t("Business records retained")}: {impact?.outputs.reduce((sum, output) => sum + (output.retained ? output.records : 0), 0) ?? 0}</Text>
        </VStack>
      )}
    </VStack>
  );
}
