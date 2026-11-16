import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { type App, type NavItem, apps, pathOf } from "../apps";
import { t } from "../i18n";
import { navigate } from "../router";

export function AppSidebar({ app, item }: { app: App; item: NavItem }) {
  return (
    <SideNav
      aria-label={t("Primary navigation")}
      collapsible
      header={
        <DropdownMenu
          button={{ label: t(app.label), variant: "ghost", width: "100%" }}
          items={apps.map((candidate) => ({ label: t(candidate.label), onClick: () => navigate(pathOf(candidate)) }))}
        />
      }>
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
    </SideNav>
  );
}
