import { Button } from "@astryxdesign/core/Button";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Token } from "@astryxdesign/core/Token";
import { Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import {
  ArchiveBoxIcon,
  BuildingOffice2Icon,
  BuildingStorefrontIcon,
  CheckBadgeIcon,
  CloudArrowDownIcon,
  Cog6ToothIcon,
  CpuChipIcon,
  CubeIcon,
  FireIcon,
  PuzzlePieceIcon,
  Squares2X2Icon,
  UsersIcon,
} from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import type { Api } from "../host";
import { t } from "../i18n";

const styles = stylex.create({
  action: { flexShrink: 0 },
  iconBox: {
    width: 36,
    height: 36,
    flexShrink: 0,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colorVars["--color-background-muted"],
    borderRadius: radiusVars["--radius-element"],
  },
});

const listingLabels: Record<string, string> = {
  general: "General", manufacturing: "Manufacturing", retail: "Retail", distribution: "Distribution", hospitality: "Hospitality",
  healthcare: "Healthcare", education: "Education", finance: "Finance", "professional-services": "Professional services",
  construction: "Construction", "real-estate": "Real estate", energy: "Energy", logistics: "Logistics", agriculture: "Agriculture",
  nonprofit: "Nonprofit", "public-sector": "Public sector", sales: "Sales", marketing: "Marketing", service: "Service", hr: "HR",
  payroll: "Payroll", accounting: "Accounting", inventory: "Inventory", purchasing: "Purchasing", production: "Production",
  projects: "Project management", assets: "Assets", maintenance: "Maintenance", quality: "Quality", learning: "Learning",
  knowledge: "Knowledge management", lending: "Lending", operations: "Business operations",
};

export function listingLabel(value: string) {
  return t(listingLabels[value] ?? value);
}

export function sourceMeta(source: string): { label: string; color: "green" | "blue" | "purple" | "gray"; icon: typeof CheckBadgeIcon } {
  if (source === "index") return { label: t("Built-in index"), color: "green", icon: CheckBadgeIcon };
  if (source === "product") return { label: t("Product version"), color: "blue", icon: CubeIcon };
  if (source === "imported") return { label: t("Local library"), color: "purple", icon: ArchiveBoxIcon };
  if (source === "installed") return { label: t("Installed"), color: "green", icon: CheckBadgeIcon };
  return { label: t("Retained"), color: "gray", icon: ArchiveBoxIcon };
}

export function packageCounts(view: Api.PackageView) {
  const apps = (view.descriptor.blueprint?.roots ?? []).filter((root) => root.kind === "app").length;
  const plugins = (view.descriptor.contributions ?? []).length + (view.descriptor.capabilities ?? []).length;
  return { apps, plugins };
}

function packageIcon(id: string, counts: { apps: number; plugins: number }) {
  const key = id.toLowerCase();
  if (key.includes("crm")) return UsersIcon;
  if (key.includes("erp")) return BuildingOffice2Icon;
  if (key.includes("mes")) return CpuChipIcon;
  if (key.includes("hospitality")) return BuildingStorefrontIcon;
  if (key.includes("heating")) return FireIcon;
  if (key.includes("production")) return Cog6ToothIcon;
  if (key.includes("core") || (counts.apps > 0 && counts.plugins > 0)) return Squares2X2Icon;
  if (counts.plugins > 0) return PuzzlePieceIcon;
  return CubeIcon;
}

export function PackageCard({
  view,
  onInspect,
  onAction,
}: {
  view: Api.PackageView;
  onInspect: () => void;
  onAction: () => void;
}) {
  const { id, version, title, description } = view.descriptor;
  const display = t(title || id);
  const summary = description ? t(description) : `${display} (${id})`;
  const source = sourceMeta(view.source);
  const counts = packageCounts(view);
  const installed = view.installed;
  const retired = installed?.state === "retired";
  const current = Boolean(installed && !retired && installed.version === version && !view.update && !view.downgrade && !view.rollback);
  const actionLabel = current
    ? t("Manage")
    : view.update
      ? t("Upgrade")
      : retired && installed?.version === version
        ? t("Reinstall")
        : t("Install");
  const icon = packageIcon(id, counts);
  const parts = [
    counts.apps > 0 ? `${counts.apps} ${counts.apps === 1 ? t("app root") : t("app roots")}` : "",
    counts.plugins > 0 ? `${counts.plugins} ${counts.plugins === 1 ? t("plugin capability") : t("plugin capabilities")}` : "",
  ].filter(Boolean);

  return (
    <VStack role="listitem" height="100%">
      <ClickableCard label={display} elevation="none" padding={4} height="100%" onClick={onInspect}>
        <VStack gap={3} height="100%">
          <HStack gap={3} vAlign="start">
            <VStack xstyle={styles.iconBox}>
              <Icon icon={icon} size="md" color="secondary" />
            </VStack>
            <StackItem size="fill">
              <VStack gap={0.5}>
                <Text type="body" weight="medium" maxLines={1}>{display}</Text>
                <HStack gap={1.5} vAlign="center">
                  <Icon icon={source.icon} size="xsm" color={source.color === "green" ? "success" : "secondary"} />
                  <Text type="supporting" color="secondary" maxLines={1}>{source.label}</Text>
                  {installed && !retired && (
                    <>
                      <StatusDot variant="success" label={t("Installed")} />
                      <Text type="supporting" color="secondary" maxLines={1}>{installed.version}</Text>
                    </>
                  )}
                  {retired && (
                    <>
                      <StatusDot variant="neutral" label={t("Retired")} />
                      <Text type="supporting" color="secondary" maxLines={1}>{t("Retired")}</Text>
                    </>
                  )}
                </HStack>
              </VStack>
            </StackItem>
            <Button
              label={actionLabel}
              variant="secondary"
              size="sm"
              xstyle={styles.action}
              onClick={onAction}
            />
          </HStack>

          <StackItem size="fill">
            <Text type="supporting" color="secondary" maxLines={2}>
              {summary}
            </Text>
          </StackItem>

          <HStack gap={2} vAlign="center">
            {parts.length > 0 && (
              <Text type="supporting" color="secondary" maxLines={1}>{parts.join(" · ")}</Text>
            )}
            <StackItem size="fill" />
            <Text type="supporting" color="secondary" hasTabularNumbers maxLines={1}>
              {id} · {version}
            </Text>
          </HStack>
        </VStack>
      </ClickableCard>
    </VStack>
  );
}

export function CatalogEntryCard({
  entry,
  libraryTitle,
  onSelect,
}: {
  entry: Api.PackageCatalogEntry;
  libraryTitle: string;
  onSelect: () => void;
}) {
  const display = t(entry.title || entry.id);
  const summary = entry.description ? t(entry.description) : `${display} (${entry.id})`;
  const icon = packageIcon(entry.id, { apps: 1, plugins: 0 });

  return (
    <VStack role="listitem" height="100%">
      <ClickableCard label={display} elevation="none" padding={4} height="100%" onClick={onSelect}>
        <VStack gap={3} height="100%">
          <HStack gap={3} vAlign="start">
            <VStack xstyle={styles.iconBox}>
              <Icon icon={icon} size="md" color="secondary" />
            </VStack>
            <StackItem size="fill">
              <VStack gap={0.5}>
                <Text type="body" weight="medium" maxLines={1}>{display}</Text>
                <HStack gap={1.5} vAlign="center">
                  <Icon icon={CloudArrowDownIcon} size="xsm" color="secondary" />
                  <Text type="supporting" color="secondary" maxLines={1}>{libraryTitle || t("Market library")}</Text>
                </HStack>
              </VStack>
            </StackItem>
            <Button
              label={t("Install")}
              variant="secondary"
              size="sm"
              xstyle={styles.action}
              onClick={onSelect}
            />
          </HStack>
          <StackItem size="fill">
            <Text type="supporting" color="secondary" maxLines={2}>
              {summary}
            </Text>
          </StackItem>
          <HStack gap={2} vAlign="center" wrap="wrap">
            {entry.listing?.stage === "test" && <Token label={t("Test")} color="orange" size="sm" />}
            {entry.listing?.stage === "preview" && <Token label={t("Preview")} color="blue" size="sm" />}
            {[...(entry.listing?.industries ?? []), ...(entry.listing?.capabilities ?? [])].slice(0, 3).map((value) => (
              <Token key={value} label={listingLabel(value)} color="gray" size="sm" />
            ))}
            {entry.listing?.source?.relation === "redesign" && (
              <Text type="supporting" color="secondary" maxLines={1}>{t("Redesigned after {app}").replace("{app}", entry.listing.source.app)}</Text>
            )}
            <StackItem size="fill" />
            <Text type="supporting" color="secondary" hasTabularNumbers maxLines={1}>
              {entry.id} · {entry.version}
            </Text>
          </HStack>
        </VStack>
      </ClickableCard>
    </VStack>
  );
}
