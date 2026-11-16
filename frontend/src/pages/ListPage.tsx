import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { Layout, LayoutContent, LayoutHeader } from "@astryxdesign/core/Layout";
import { PlusIcon } from "@heroicons/react/24/outline";
import type { NavItem } from "../apps";
import { t } from "../i18n";

export function ListPage({ item }: { item: NavItem }) {
  return (
    <Layout
      height="fill"
      header={
        <LayoutHeader hasDivider>
          <Heading level={1}>{t(item.label)}</Heading>
        </LayoutHeader>
      }
      content={
        <LayoutContent>
          <EmptyState
            headingLevel={2}
            icon={<Icon icon={item.icon} size="lg" />}
            title={t("Nothing here yet")}
            description={t("This list is not connected to the platform yet.")}
            actions={<Button label={t("Create")} variant="primary" icon={<Icon icon={PlusIcon} />} isDisabled />}
          />
        </LayoutContent>
      }
    />
  );
}
