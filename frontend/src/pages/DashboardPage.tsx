import { Banner } from "@astryxdesign/core/Banner";
import { Card } from "@astryxdesign/core/Card";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Grid } from "@astryxdesign/core/Grid";
import { HStack, Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useRead } from "../host";
import { t } from "../i18n";
import { PageTitle, useGutter } from "./frame";

type Cells = Record<string, string>;
export type DashboardMetric<Answer> = { title: string; href: string; value: (answer: Answer) => number };
export type DashboardColumn = { key: string; header: string; width?: number };
export type DashboardTable<Answer> = {
  title: string;
  label: string;
  href: string;
  columns: DashboardColumn[];
  rows: (answer: Answer) => Cells[];
};
export type DashboardDefinition<Answer> = {
  title: string;
  read: string;
  metrics: DashboardMetric<Answer>[];
  tables: DashboardTable<Answer>[];
};

export function dashboardPage<Answer>(definition: DashboardDefinition<Answer>) {
  return function DashboardEntry() {
    const read = useRead<Answer>(definition.read);
    const gutter = useGutter();
    return (
      <Layout
        height="fill"
        content={
          <LayoutContent role="main" padding={gutter}>
            <VStack gap={6}>
              <PageTitle title={t(definition.title)} />
              {read.status === "loading" && (
                <Grid columns={{ minWidth: 200, max: 4 }} gap={4}>
                  {definition.metrics.map((metric, index) => (
                    <Skeleton key={metric.title} height={120} index={index} />
                  ))}
                </Grid>
              )}
              {read.status === "error" && (
                <Banner status="error" title={t("This page could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
              )}
              {read.status === "refused" && (
                <EmptyState
                  headingLevel={2}
                  title={t("You cannot see this page")}
                  description={read.reason || t("Ask an administrator of this workspace for access.")}
                />
              )}
              {read.status === "ready" && (
                <Grid columns={{ minWidth: 200, max: 4 }} gap={4}>
                  {definition.metrics.map((metric) => (
                    <Card key={metric.title}>
                      <VStack gap={2}>
                        <Heading level={4} accessibilityLevel={2}>{t(metric.title)}</Heading>
                        <Text type="large" size="2xl" weight="semibold">{metric.value(read.data).toLocaleString()}</Text>
                        <Link href={metric.href}>{t("View all")}</Link>
                      </VStack>
                    </Card>
                  ))}
                </Grid>
              )}
              {read.status === "ready" && (
                <Grid columns={{ minWidth: 320, repeat: "fit" }} gap={4}>
                  {definition.tables.map((table) => {
                    const columns: TableColumn<Cells>[] = table.columns.map((column) => ({
                      key: column.key,
                      header: t(column.header),
                      width: column.width ? pixel(column.width) : proportional(1),
                      renderCell: (cells) => (
                        <Text type="body" maxLines={1}>
                          {cells[column.key] ?? ""}
                        </Text>
                      ),
                    }));
                    return (
                      <Card key={table.title}>
                        <VStack gap={6}>
                          <HStack hAlign="between" vAlign="center">
                            <Heading level={4} accessibilityLevel={2}>{t(table.title)}</Heading>
                            <Link href={table.href}>{t(table.label)}</Link>
                          </HStack>
                          <Table<Cells> data={table.rows(read.data)} columns={columns} idKey="id" density="compact" dividers="rows" hasHover />
                        </VStack>
                      </Card>
                    );
                  })}
                </Grid>
              )}
            </VStack>
          </LayoutContent>
        }
      />
    );
  };
}
