import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { Kbd } from "@astryxdesign/core/Kbd";
import { HStack } from "@astryxdesign/core/Layout";
import { SideNav, SideNavHeading, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { BookmarkSquareIcon, ChevronDoubleLeftIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type App, type NavItem, pathOf } from "../apps";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { navigate, useSearch } from "../router";

type Preference = { entity: string; pinned?: string[] };

const styles = stylex.create({
  sidebar: { backgroundColor: colorVars["--color-background-body"] },
});

export function AppSidebar({ app, item, apps, object, listPath, onCollapse }: { app: App; item: NavItem; apps: App[]; object?: string; listPath?: string; onCollapse?: () => void }) {
  const selected = new URLSearchParams(useSearch()).get("saved");
  const domain = object ? encodeURIComponent(JSON.stringify([["entity", "=", object]])) : "";
  const views = useRead<Api.RecordPage>(object ? `/v1/records/work.view?domain=${domain}&limit=200` : null);
  const preferences = useRead<Api.RecordPage>(object ? "/v1/records/work.viewpreference?limit=200" : null);
  const pinned = preferences.status === "ready"
    ? new Set((preferences.data.records as Preference[]).find((entry) => entry.entity === object)?.pinned ?? [])
    : new Set<string>();
  const saved = views.status === "ready"
    ? [...(views.data.records as Api.SavedView[])].sort((left, right) => Number(pinned.has(right.id)) - Number(pinned.has(left.id)))
    : [];
  const header = onCollapse ? (
    <SideNavHeading
      heading={t(app.label)}
      headerEndContent={
        <Tooltip content={<HStack gap={2} vAlign="center">{t("Hide sidebar")}<Kbd keys="mod+b" /></HStack>} placement="end">
          <IconButton label={t("Hide sidebar")} variant="ghost" size="sm" icon={<Icon icon={ChevronDoubleLeftIcon} />} onClick={onCollapse} />
        </Tooltip>
      }
    />
  ) : (
    <DropdownMenu
      button={{ label: t(app.label), variant: "ghost", width: "100%" }}
      items={apps.map((candidate) => ({ label: t(candidate.label), onClick: () => navigate(pathOf(candidate)) }))}
    />
  );
  return (
    <SideNav aria-label={t("Primary navigation")} header={header} xstyle={onCollapse && styles.sidebar}>
      {app.sections.map((section) => (
        <SideNavSection key={section.title} title={t(section.title)}>
          {section.items.map((entry) => (
            <SideNavItem
              key={entry.id}
              label={t(entry.label)}
              icon={entry.icon}
              href={pathOf(app, entry)}
              isSelected={entry.id === item.id}
            />
          ))}
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
  );
}
