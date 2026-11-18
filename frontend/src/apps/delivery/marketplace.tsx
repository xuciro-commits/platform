import { Button } from "@astryxdesign/core/Button";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useState } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { navigate } from "../../router";
import { PackageReview } from "../../pages/PackageReview";

export default function Marketplace() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const [selected, setSelected] = useState<Api.PackageView>();
  const rows = packages.status === "ready" ? packages.data : [];
  return (
    <>
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={6} hasDivider>
            <VStack gap={2}>
              <Heading level={1}>{t("Marketplace")}</Heading>
              <Text type="body" color="secondary">{t("Review versions, dependencies and their installation scope before adding them to this workspace.")}</Text>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={6} role="main">
            {packages.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
            {packages.status !== "loading" && rows.length === 0 && <Text type="body">{t("No package versions are available.")}</Text>}
            {rows.length > 0 && (
              <List header={t("Available versions")} hasDividers>
                {rows.map((view) => {
                  const current = view.installed?.version === view.descriptor.version && !view.update && !view.downgrade && !view.rollback;
                  return (
                  <ListItem
                    key={`${view.descriptor.id}@${view.descriptor.version}`}
                    label={view.descriptor.title || view.descriptor.id}
                    description={`${view.descriptor.id} · ${view.descriptor.version}${view.installed ? ` · ${t("Installed")} ${view.installed.version}` : ""}`}
                    endContent={
                      <HStack gap={2}>
                        <Button label={current ? t("Manage") : t("Review and install")} variant={current ? "secondary" : "primary"} onClick={() => setSelected(view)} />
                      </HStack>
                    }
                  />
                  );
                })}
              </List>
            )}
          </LayoutContent>
        }
      />
      {selected && (
        <PackageReview
          view={selected}
          mode={selected.installed?.version === selected.descriptor.version && !selected.update && !selected.downgrade && !selected.rollback ? "manage" : "install"}
          close={() => setSelected(undefined)}
          done={() => {
            setSelected(undefined);
            navigate("/delivery/installations");
          }}
        />
      )}
    </>
  );
}
