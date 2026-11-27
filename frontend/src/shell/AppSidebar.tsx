import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { Kbd } from "@astryxdesign/core/Kbd";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { useToast } from "@astryxdesign/core/Toast";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { BookmarkSquareIcon, ChevronDoubleLeftIcon, FolderIcon, TrashIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useState } from "react";
import { type App, type NavItem, pathOf } from "../apps";
import { type Api, reread, useRead, reasonOf, send } from "../host";
import { t } from "../i18n";
import { navigate, useSearch } from "../router";
import { TenantSwitcher } from "./TenantSwitcher";

type Preference = { entity: string; pinned?: string[] };

const styles = stylex.create({
  sidebar: { backgroundColor: colorVars["--color-background-body"] },
});

export function AppSidebar({ app, item, apps, object, listPath, onCollapse, inDrawer }: { app: App; item?: NavItem; apps: App[]; object?: string; listPath?: string; onCollapse?: () => void; inDrawer?: boolean }) {
  const selected = new URLSearchParams(useSearch()).get("saved");
  const domain = object ? encodeURIComponent(JSON.stringify([["entity", "=", object]])) : "";
  const views = useRead<Api.RecordPage>(object ? `/v1/records/work.view?domain=${domain}&limit=200` : null);
  const preferences = useRead<Api.RecordPage>(object ? "/v1/records/work.viewpreference?limit=200" : null);
  const me = useRead<Api.MeView>("/v1/me");
  const storageKey = me.status === "ready" ? `platform:nav-collapsed:${me.data.tenantId}:${me.data.principalId}:${app.id}` : "";
  const [collapsedMap, setCollapsedMap] = useState<Record<string, boolean>>({});
  useEffect(() => {
    if (!storageKey) return;
    try {
      const savedState = localStorage.getItem(storageKey);
      setCollapsedMap(savedState ? (JSON.parse(savedState) as Record<string, boolean>) : {});
    } catch {
      setCollapsedMap({});
    }
  }, [storageKey]);
  const setGroupCollapsed = (groupId: string, next: boolean) => {
    setCollapsedMap((prev) => {
      const updated = { ...prev, [groupId]: next };
      if (storageKey) {
        try {
          localStorage.setItem(storageKey, JSON.stringify(updated));
        } catch {
          return updated;
        }
      }
      return updated;
    });
  };
  const catalog = useRead<Api.Action[]>(app.packageId && app.hasDemo ? "/v1/actions" : null);
  const clearDemo = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.package.clear-demo") : undefined;
  const [confirmClear, setConfirmClear] = useState(false);
  const [clearing, setClearing] = useState(false);
  const toast = useToast();
  const pinned = preferences.status === "ready"
    ? new Set((preferences.data.records as Preference[]).find((entry) => entry.entity === object)?.pinned ?? [])
    : new Set<string>();
  const saved = views.status === "ready"
    ? [...(views.data.records as Api.SavedView[])].sort((left, right) => Number(pinned.has(right.id)) - Number(pinned.has(left.id)))
    : [];
  const runClearDemo = async () => {
    if (!clearDemo || !app.packageId) return;
    setClearing(true);
    try {
      await send(clearDemo, app.packageId, { id: app.packageId });
      reread();
      setConfirmClear(false);
      toast({ body: t("Demo data cleared") });
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setClearing(false);
    }
  };
  const header = onCollapse ? (
    <HStack gap={1} vAlign="center" minHeight={32} paddingBlockStart={1}>
      <StackItem size="fill">
        <TenantSwitcher />
      </StackItem>
      <Tooltip content={<HStack gap={2} vAlign="center">{t("Hide sidebar")}<Kbd keys="mod+b" /></HStack>} placement="end">
        <IconButton label={t("Hide sidebar")} variant="ghost" size="sm" icon={<Icon icon={ChevronDoubleLeftIcon} />} onClick={onCollapse} />
      </Tooltip>
    </HStack>
  ) : inDrawer ? undefined : (
    <DropdownMenu
      button={{ label: t(app.label), variant: "ghost", width: "100%" }}
      items={apps.map((candidate) => ({ label: t(candidate.label), onClick: () => navigate(pathOf(candidate)) }))}
    />
  );
  const footer = app.packageId && app.hasDemo && clearDemo ? (
    <VStack gap={1} padding={2}>
      <Divider />
      <Button
        label={t("Clear demo data")}
        icon={<Icon icon={TrashIcon} size="sm" />}
        variant="ghost"
        size="sm"
        width="100%"
        onClick={() => setConfirmClear(true)}
      />
    </VStack>
  ) : undefined;
  return (
    <>
      <SideNav aria-label={t("Primary navigation")} header={header} footer={footer} xstyle={onCollapse && styles.sidebar}>
        {app.sections.map((section) => (
          <SideNavSection key={section.id ?? section.title} title={t(section.title)}>
            {section.items.map((entry) => (
              <SideNavItem
                key={entry.id}
                label={t(entry.label)}
                icon={entry.icon}
                href={pathOf(app, entry)}
                isSelected={entry.id === item?.id}
              />
            ))}
            {(section.groups ?? []).map((group) => {
              const containsActive = group.items.some((entry) => entry.id === item?.id);
              const isCollapsed = containsActive ? false : (collapsedMap[group.id] ?? Boolean(group.collapsed));
              return (
                <SideNavItem
                  key={group.id}
                  label={t(group.title)}
                  icon={FolderIcon}
                  collapsible={{ isCollapsed, onCollapsedChange: (next) => setGroupCollapsed(group.id, next) }}
                >
                  {group.items.map((entry) => (
                    <SideNavItem
                      key={entry.id}
                      label={t(entry.label)}
                      icon={entry.icon}
                      href={pathOf(app, entry)}
                      isSelected={entry.id === item?.id}
                    />
                  ))}
                </SideNavItem>
              );
            })}
          </SideNavSection>
        ))}
        {object && listPath && saved.length > 0 && (
          <SideNavSection title={t("Saved views")}>
            {saved.map((view) => (
              <SideNavItem
                key={view.id}
                label={view.title}
                icon={BookmarkSquareIcon}
                href={`${listPath}?view=${encodeURIComponent(view.state)}&saved=${encodeURIComponent(view.id)}`}
                isSelected={view.id === selected}
              />
            ))}
          </SideNavSection>
        )}
      </SideNav>
      {confirmClear && (
        <AlertDialog
          isOpen
          onOpenChange={(open) => !open && !clearing && setConfirmClear(false)}
          title={t("Clear demo data for {app}?").replace("{app}", t(app.label))}
          description={t("This archives the sample records installed with this app. Your settings, reference data, and any records you created stay.")}
          cancelLabel={t("Cancel")}
          actionLabel={t("Clear demo data")}
          actionVariant="destructive"
          isActionLoading={clearing}
          onAction={() => void runClearDemo()}
        />
      )}
    </>
  );
}
