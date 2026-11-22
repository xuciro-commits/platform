import { Button } from "@astryxdesign/core/Button";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { CubeIcon, PuzzlePieceIcon, Squares2X2Icon } from "@heroicons/react/24/outline";
import type { Api } from "../host";
import { t } from "../i18n";

export function sourceMeta(source: string): { label: string; color: "green" | "blue" | "purple" | "gray" } {
  if (source === "index") return { label: t("Built-in index"), color: "green" };
  if (source === "product") return { label: t("Product version"), color: "blue" };
  if (source === "imported") return { label: t("Local library"), color: "purple" };
  if (source === "installed") return { label: t("Installed"), color: "green" };
  return { label: t("Retained"), color: "gray" };
}

export function packageCounts(view: Api.PackageView) {
  const apps = (view.descriptor.blueprint?.roots ?? []).filter((root) => root.kind === "app").length;
  const plugins = (view.descriptor.contributions ?? []).length + (view.descriptor.capabilities ?? []).length;
  return { apps, plugins };
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
  const display = title || id;
  const source = sourceMeta(view.source);
  const counts = packageCounts(view);
  const installed = view.installed;
  const retired = installed?.state === "retired";
  const current = Boolean(installed && !retired && installed.version === version && !view.update && !view.downgrade && !view.rollback);
  const actionLabel = current
    ? t("Manage")
    : view.update
      ? t("Review upgrade")
      : retired && installed?.version === version
        ? t("Reinstall")
        : t("Review and install");
  const icon = counts.apps > 0 && counts.plugins > 0 ? Squares2X2Icon : counts.plugins > 0 ? PuzzlePieceIcon : CubeIcon;

  return (
    <VStack role="listitem" height="100%">
      <ClickableCard label={display} elevation="low" padding={4} height="100%" onClick={onInspect}>
        <VStack gap={3} height="100%">
          <HStack gap={3} vAlign="start">
            <Icon icon={icon} size="lg" />
            <StackItem size="fill">
              <VStack gap={1}>
                <Text type="body" weight="bold" maxLines={1}>{display}</Text>
                <HStack gap={1} vAlign="center" wrap="wrap">
                  <Token label={source.label} color={source.color} size="sm" />
                  {installed && !retired && <Token label={`${t("Installed")} ${installed.version}`} color="green" size="sm" />}
                  {retired && <Token label={t("Retired")} color="gray" size="sm" />}
                </HStack>
              </VStack>
            </StackItem>
            <Button
              label={actionLabel}
              variant={current ? "secondary" : "primary"}
              size="sm"
              onClick={onAction}
            />
          </HStack>

          <StackItem size="fill">
            <Text type="body" color="secondary" maxLines={2}>
              {description || `${display} (${id})`}
            </Text>
          </StackItem>

          <HStack gap={2} vAlign="center" wrap="wrap">
            {counts.apps > 0 && (
              <Token label={`${counts.apps} ${counts.apps === 1 ? t("app root") : t("app roots")}`} color="blue" size="sm" />
            )}
            {counts.plugins > 0 && (
              <Token label={`${counts.plugins} ${counts.plugins === 1 ? t("plugin capability") : t("plugin capabilities")}`} color="purple" size="sm" />
            )}
            <StackItem size="fill" />
            <Text type="supporting" color="secondary" maxLines={1}>
              {id} · {version}
            </Text>
          </HStack>
        </VStack>
      </ClickableCard>
    </VStack>
  );
}
