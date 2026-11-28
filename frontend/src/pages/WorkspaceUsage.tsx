import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Card } from "@astryxdesign/core/Card";
import { Collapsible } from "@astryxdesign/core/Collapsible";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import {
  ArchiveBoxIcon,
  BoltIcon,
  ChartBarIcon,
  CircleStackIcon,
  CubeIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import type { IconType } from "@astryxdesign/core/Icon";
import * as stylex from "@stylexjs/stylex";
import { useState } from "react";
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis } from "recharts";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { useRecordHref } from "../recordRoutes";
import { formatTenantNumber } from "../semantic";

type MetricMeta = { label: string; icon: IconType; hue: string; fallbackHref: string };

const metricMeta: Record<string, MetricMeta> = {
  records: { label: "Active records", icon: ArchiveBoxIcon, hue: "blue", fallbackHref: "/ontology/objects" },
  storage: { label: "Storage", icon: CircleStackIcon, hue: "teal", fallbackHref: "/ontology/objects" },
  writes: { label: "Daily writes", icon: BoltIcon, hue: "orange", fallbackHref: "/operations/audit" },
  ai: { label: "AI tokens", icon: SparklesIcon, hue: "purple", fallbackHref: "/admin/models" },
  packages: { label: "Installed packages", icon: CubeIcon, hue: "green", fallbackHref: "/delivery/installations" },
};

const planTitles: Record<string, string> = {
  starter: "Starter",
  team: "Team",
  enterprise: "Enterprise",
};

const tick = { fontSize: "var(--font-size-xs)", fill: "var(--color-text-secondary)" };

const styles = stylex.create({
  chartBox: { width: "100%", height: 68 },
  progressBox: { width: "100%" },
  sourceRow: {
    width: "100%",
    borderTopWidth: 1,
    borderTopStyle: "solid",
    borderTopColor: "var(--color-border-subtle)",
  },
});

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${formatTenantNumber(bytes, { maximumFractionDigits: 0 })} B`;
  if (bytes < 1024 * 1024) return `${formatTenantNumber(bytes / 1024, { maximumFractionDigits: 1 })} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${formatTenantNumber(bytes / (1024 * 1024), { maximumFractionDigits: 1 })} MB`;
  return `${formatTenantNumber(bytes / (1024 * 1024 * 1024), { maximumFractionDigits: 2 })} GB`;
}

function formatMetricAmount(unit: string, amount: number, cost?: number): string {
  const base = unit === "bytes"
    ? formatBytes(amount)
    : formatTenantNumber(amount, { maximumFractionDigits: 0 });
  if (cost && cost > 0) {
    return `${base} ($${formatTenantNumber(cost, { minimumFractionDigits: 2, maximumFractionDigits: 4 })})`;
  }
  return base;
}

function statusOf(metric: Api.UsageMetric): { variant: "neutral" | "success" | "warning" | "error"; bar?: "success" | "warning" | "error"; label: string } {
  const hardReached = Boolean((metric.hardLimit && metric.used >= metric.hardLimit) || (metric.hardCost && (metric.cost ?? 0) >= metric.hardCost));
  if (hardReached) return { variant: "error", bar: "error", label: "Hard limit reached" };
  const softReached = Boolean((metric.softLimit && metric.used >= metric.softLimit) || (metric.softCost && (metric.cost ?? 0) >= metric.softCost));
  if (softReached) return { variant: "warning", bar: "warning", label: "Over soft limit" };
  if (metric.hardLimit || metric.softLimit || metric.hardCost || metric.softCost) {
    return { variant: "success", bar: undefined, label: "Within quota" };
  }
  return { variant: "neutral", bar: undefined, label: "Metered" };
}

function sourceHref(source: Api.UsageSource, recordHref: (type: string, id?: string) => string, fallback: string): string {
  if (source.path && !source.path.startsWith("/ontology/objects/")) {
    return source.path;
  }
  if (source.objectSet?.object) {
    return recordHref(source.objectSet.object);
  }
  return source.path || fallback;
}

function MetricCard({ metric, recordHref }: { metric: Api.UsageMetric; recordHref: (type: string, id?: string) => string }) {
  const [open, setOpen] = useState(false);
  const meta = metricMeta[metric.kind] ?? { label: metric.kind, icon: ChartBarIcon, hue: "blue", fallbackHref: "/home/overview" };
  const status = statusOf(metric);
  const ceiling = metric.hardLimit || metric.softLimit || Math.max(1, metric.used);
  const usedLabel = formatMetricAmount(metric.unit, metric.used, metric.cost);
  const quotaLabel = metric.hardLimit
    ? t("{used} of {limit}").replace("{used}", usedLabel).replace("{limit}", formatMetricAmount(metric.unit, metric.hardLimit, metric.hardCost))
    : metric.softLimit
      ? t("{used} (soft limit {limit})").replace("{used}", usedLabel).replace("{limit}", formatMetricAmount(metric.unit, metric.softLimit, metric.softCost))
      : usedLabel;
  const points = metric.trend.map((day) => ({
    day: day.date.slice(8),
    date: day.date,
    value: day.value,
  }));

  return (
    <Card padding={3}>
      <VStack gap={3}>
        <Collapsible
          isOpen={open}
          onOpenChange={setOpen}
          trigger={
            <HStack gap={3} vAlign="center" hAlign="between" paddingBlock={1}>
              <HStack gap={2} vAlign="center" wrap="wrap">
                <Icon icon={meta.icon} size="sm" />
                <Text weight="semibold">{t(meta.label)}</Text>
                <Badge variant={status.variant} label={t(status.label)} />
                <Badge variant="neutral" label={t("{count} sources").replace("{count}", String(metric.sources.length))} />
              </HStack>
              <Text type="code" hasTabularNumbers>{quotaLabel}</Text>
            </HStack>
          }
        >
          <VStack gap={2} paddingBlockStart={3}>
            {metric.sources.length === 0 ? (
              <HStack gap={2} vAlign="center" hAlign="between" paddingBlock={2}>
                <Text type="supporting" color="secondary">{t("No usage recorded in this category yet.")}</Text>
                <Link href={meta.fallbackHref}>{t("Open section")}</Link>
              </HStack>
            ) : (
              metric.sources.map((source) => {
                const pct = metric.used > 0 ? Math.round((source.value / metric.used) * 100) : 0;
                const href = sourceHref(source, recordHref, meta.fallbackHref);
                return (
                  <HStack key={source.key} gap={3} vAlign="center" hAlign="between" paddingBlock={2} xstyle={styles.sourceRow}>
                    <HStack gap={2} vAlign="center" wrap="wrap">
                      <Link href={href}>{source.title}</Link>
                      {source.app && <Badge variant="neutral" label={source.app} />}
                    </HStack>
                    <HStack gap={2} vAlign="center">
                      <Text type="code" hasTabularNumbers>{formatMetricAmount(metric.unit, source.value, source.cost)}</Text>
                      <Text type="supporting" color="secondary" hasTabularNumbers>{`${pct}%`}</Text>
                    </HStack>
                  </HStack>
                );
              })
            )}
          </VStack>
        </Collapsible>

        <VStack gap={1} xstyle={styles.progressBox}>
          <ProgressBar
            value={Math.min(metric.used, ceiling)}
            max={Math.max(1, ceiling)}
            variant={status.bar}
            label={t(meta.label)}
            isLabelHidden
          />
          {metric.softLimit ? (
            <HStack hAlign="between" vAlign="center">
              <Text type="supporting" color="secondary">
                {t("Soft limit: {limit}").replace("{limit}", formatMetricAmount(metric.unit, metric.softLimit, metric.softCost))}
              </Text>
              {metric.hardLimit ? (
                <Text type="supporting" color="secondary">
                  {t("Hard limit: {limit}").replace("{limit}", formatMetricAmount(metric.unit, metric.hardLimit, metric.hardCost))}
                </Text>
              ) : null}
            </HStack>
          ) : null}
        </VStack>

        <VStack gap={1}>
          <HStack hAlign="between" vAlign="center">
            <Text type="supporting" color="secondary">{t("This month's trend")}</Text>
            <Text type="supporting" color="secondary" hasTabularNumbers>
              {metric.trend.length > 0 ? `${metric.trend[0].date} – ${metric.trend[metric.trend.length - 1].date}` : ""}
            </Text>
          </HStack>
          <HStack xstyle={styles.chartBox}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={points} margin={{ top: 4, right: 4, bottom: 0, left: 4 }}>
                <XAxis dataKey="day" tick={tick} axisLine={false} tickLine={false} minTickGap={16} />
                <Tooltip
                  formatter={(val) => [formatMetricAmount(metric.unit, Number(val ?? 0)), t(meta.label)]}
                  labelFormatter={(_, payload) => String(payload?.[0]?.payload?.date ?? "")}
                />
                <Bar dataKey="value" fill={`var(--color-data-categorical-${meta.hue})`} radius={[2, 2, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </HStack>
        </VStack>
      </VStack>
    </Card>
  );
}

export function WorkspaceUsage({ readPath }: { readPath: string }) {
  const read = useRead<Api.TenantUsage>(readPath);
  const recordHref = useRecordHref();

  if (read.status === "loading") {
    return (
      <VStack gap={3}>
        {[0, 1, 2, 3, 4].map((index) => <Skeleton key={index} height={132} index={index} />)}
      </VStack>
    );
  }
  if (read.status === "error") {
    return <Banner status="error" title={t("Usage could not be loaded")} description={read.reason || t("Reload the page to try again.")} />;
  }
  if (read.status === "refused") {
    return <EmptyState headingLevel={3} title={t("You cannot see workspace usage")} description={read.reason || t("Ask an administrator of this workspace for access.")} />;
  }

  const usage = read.data;
  const planTitle = usage.plan ? (planTitles[usage.plan] ?? usage.plan) : "";

  return (
    <VStack gap={4}>
      <Card padding={3}>
        <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
          <HStack gap={2} vAlign="center" wrap="wrap">
            <Text weight="semibold">{t("Current plan")}</Text>
            <Badge variant={planTitle ? "success" : "neutral"} label={planTitle ? t(planTitle) : t("Unassigned")} />
            <Text type="supporting" color="secondary">{t("Month {month}").replace("{month}", usage.month)}</Text>
          </HStack>
          <Text type="supporting" color="secondary">
            {t("Click any metric to inspect the records, packages or models behind its total.")}
          </Text>
        </HStack>
      </Card>
      <VStack gap={3}>
        {usage.metrics.map((metric) => (
          <MetricCard key={metric.kind} metric={metric} recordHref={recordHref} />
        ))}
      </VStack>
    </VStack>
  );
}
