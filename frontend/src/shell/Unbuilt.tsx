import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { Layout, LayoutContent, LayoutHeader } from "@astryxdesign/core/Layout";
import { Heading } from "@astryxdesign/core/Text";
import type { NavItem } from "../apps";
import { t } from "../i18n";

export function Unbuilt({ item }: { item: NavItem }) {
  return (
    <Layout
      height="fill"
      header={
        <LayoutHeader hasDivider padding={4}>
          <Heading level={1}>{t(item.label)}</Heading>
        </LayoutHeader>
      }
      content={
        <LayoutContent>
          <EmptyState
            headingLevel={2}
            icon={<Icon icon={item.icon} size="lg" />}
            title={t("Not built yet")}
            description={t("This entry has not been rebuilt in the new frontend.")}
          />
        </LayoutContent>
      }
    />
  );
}
