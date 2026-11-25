import { Button } from "@astryxdesign/core/Button";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Card } from "@astryxdesign/core/Card";
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
import { useIsDark } from "../shell/appearance";

const styles = stylex.create({
  action: { flexShrink: 0 },
  cover: (src: string, vector: boolean) => ({
    width: "32%",
    minWidth: 88,
    maxWidth: 124,
    aspectRatio: "4 / 3",
    flexShrink: 0,
    backgroundColor: vector ? colorVars["--color-background-muted"] : undefined,
    backgroundImage: "url(" + src + ")",
    backgroundSize: vector ? "contain" : "cover",
    backgroundRepeat: "no-repeat",
    backgroundPosition: "center",
    borderRadius: radiusVars["--radius-element"],
  }),
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
  knowledge: "Knowledge management", lending: "Lending", operations: "Business operations", "master-data": "Master data",
};

const officialCovers = new Set(["crm", "helpdesk", "hr-core", "party", "uom"]);
const coverBase = "https://raw.githubusercontent.com/xuciro-commits/platform-packages/3a13d89d9183add8a5d5e2faa248bc67c43dd039/";
const anatomyCovers = new Set(["crm", "helpdesk", "party"]);
function coverFor(id: string, source: "index" | "official", dark: boolean) {
  if (anatomyCovers.has(id)) return { src: "/media/anatomy/cover-" + id + "." + (dark ? "dark" : "light") + ".svg", vector: true };
  if (source === "index") {
    if (id === "application.production-provider") return { src: coverBase + "marketplace/covers/application.production-provider.avif", vector: false };
    return undefined;
  }
  return officialCovers.has(id) ? { src: coverBase + "packages/" + id + "/assets/cover.avif", vector: false } : undefined;
}

export const packageKinds = ["application", "master-data", "capability", "extension"];
const kindLabels: Record<string, string> = {
  application: "Application package", "master-data": "Master data package", capability: "Capability package", extension: "Extension package",
};
const kindPlaces: Record<string, string> = {
  application: "Adds an application to the rail.",
  "master-data": "Listed under Admin → Master data; it adds no application to the rail.",
  capability: "Bind an implementation under Admin → Capabilities; applications that use it then show its actions.",
  extension: "Adds fields, actions or views inside the application it extends.",
};

export function kindLabel(kind: string) {
  return t(kindLabels[kind] ?? kind);
}

export function kindPlace(kind: string) {
  return kindPlaces[kind] ? t(kindPlaces[kind]) : "";
}

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

export function compareVersions(left: string, right: string) {
  const a = left.split(/[^0-9]+/).filter(Boolean).map(Number);
  const b = right.split(/[^0-9]+/).filter(Boolean).map(Number);
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const diff = (a[index] ?? 0) - (b[index] ?? 0);
    if (diff !== 0) return diff;
  }
  return left.localeCompare(right);
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
  otherVersions = 0,
  onInspect,
  onAction,
}: {
  view: Api.PackageView;
  otherVersions?: number;
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
  const dark = useIsDark();
  const icon = packageIcon(id, counts);
  const cover = view.source === "index" ? coverFor(id, "index", dark) : undefined;
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
          <HStack gap={3} vAlign="start">
            {cover && <Card variant="transparent" padding={0} xstyle={styles.cover(cover.src, cover.vector)} />}
            <StackItem size="fill">
              <Text type="supporting" color="secondary" maxLines={2}>
                {summary}
              </Text>
            </StackItem>
          </HStack>
          <StackItem size="fill" />
          <HStack gap={2} vAlign="center">
            {view.kind && <Token label={kindLabel(view.kind)} color="gray" size="sm" />}
            {parts.length > 0 && (
              <Text type="supporting" color="secondary" maxLines={1}>{parts.join(" · ")}</Text>
            )}
            <StackItem size="fill" />
            <Text type="supporting" color="secondary" hasTabularNumbers maxLines={1}>
              {id} · {version}{otherVersions > 0 ? ` (+${otherVersions})` : ""}
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
  official = false,
  otherVersions = 0,
  installed,
  onSelect,
}: {
  entry: Api.PackageCatalogEntry;
  libraryTitle: string;
  official?: boolean;
  otherVersions?: number;
  installed?: Api.InstalledPackage;
  onSelect: () => void;
}) {
  const display = t(entry.title || entry.id);
  const summary = entry.description ? t(entry.description) : `${display} (${entry.id})`;
  const dark = useIsDark();
  const icon = packageIcon(entry.id, { apps: 1, plugins: 0 });
  const cover = official ? coverFor(entry.id, "official", dark) : undefined;
  const activeInstalled = installed && installed.state !== "retired" ? installed : undefined;
  const upgradable = Boolean(activeInstalled && compareVersions(entry.version, activeInstalled.version) > 0);
  const actionLabel = upgradable ? t("Upgrade") : t("Install");

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
                  <Icon icon={official ? CheckBadgeIcon : CloudArrowDownIcon} size="xsm" color={official ? "accent" : "secondary"} />
                  <Text type="supporting" color="secondary" maxLines={1}>{libraryTitle || t("Market library")}</Text>
                  {activeInstalled && (
                    <>
                      <StatusDot variant="success" label={t("Installed")} />
                      <Text type="supporting" color="secondary" maxLines={1}>{activeInstalled.version}</Text>
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
              onClick={onSelect}
            />
          </HStack>
          <HStack gap={3} vAlign="start">
            {cover && <Card variant="transparent" padding={0} xstyle={styles.cover(cover.src, cover.vector)} />}
            <StackItem size="fill">
              <Text type="supporting" color="secondary" maxLines={2}>
                {summary}
              </Text>
            </StackItem>
          </HStack>
          <StackItem size="fill" />
          <VStack gap={2}>
            {(entry.kind || entry.listing?.stage === "test" || entry.listing?.stage === "preview" || (entry.listing?.industries?.length ?? 0) > 0 || (entry.listing?.domains?.length ?? 0) > 0) && (
              <HStack gap={1.5} vAlign="center" wrap="wrap">
                {entry.kind && <Token label={kindLabel(entry.kind)} color="gray" size="sm" />}
                {entry.listing?.stage === "test" && <Token label={t("Test")} color="orange" size="sm" />}
                {entry.listing?.stage === "preview" && <Token label={t("Preview")} color="blue" size="sm" />}
                {[...(entry.listing?.industries ?? []), ...(entry.listing?.domains ?? [])].slice(0, 3).map((value) => (
                  <Token key={value} label={listingLabel(value)} color="gray" size="sm" />
                ))}
              </HStack>
            )}
            <HStack gap={2} vAlign="center">
              {entry.listing?.source?.relation === "redesign" && (
                <Text type="supporting" color="secondary" maxLines={1}>{t("Redesigned after {app}").replace("{app}", entry.listing.source.app)}</Text>
              )}
              <StackItem size="fill" />
              <Text type="supporting" color="secondary" hasTabularNumbers maxLines={1}>
                {entry.id} · {entry.version}{otherVersions > 0 ? ` (+${otherVersions})` : ""}
              </Text>
            </HStack>
          </VStack>
        </VStack>
      </ClickableCard>
    </VStack>
  );
}
