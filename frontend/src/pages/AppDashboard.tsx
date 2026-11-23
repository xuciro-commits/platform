import { Banner } from "@astryxdesign/core/Banner";
import { Card } from "@astryxdesign/core/Card";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Grid, GridSpan } from "@astryxdesign/core/Grid";
import { HStack, Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { navigate } from "../router";
import { describe } from "./describe";
import { PageTitle, useGutter } from "./frame";
import { useOptions } from "./options";
import { present } from "./values";
import { ErrorBoundary } from "../shell/ErrorBoundary";

type Hue = "blue" | "orange" | "green" | "purple" | "teal" | "pink" | "cyan" | "red";
type Point = { key: string; value: unknown; label: string; amount: number; href?: string; hue: Hue };
type Shown = { points: Point[]; money: boolean; format: (amount: number) => string };

const hues: Hue[] = ["blue", "orange", "green", "purple", "teal", "pink", "cyan", "red"];
const colour = (hue: Hue) => `var(--color-data-categorical-${hue})`;
const tick = { fontSize: "var(--font-size-sm)", fill: "var(--color-text-secondary)" };
const grid = "var(--color-border)";

const styles = stylex.create({
  swatch: (hue: Hue) => ({
    width: 8,
    height: 8,
    borderRadius: 2,
    flexShrink: 0,
    backgroundColor: `var(--color-data-categorical-${hue})`,
  }),
});

export function AppDashboard({ title, blocks, listOf }: { title: string; blocks: Api.DashboardBlock[]; listOf: (object: string) => string | undefined }) {
  const gutter = useGutter();
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const entityOf = (block: Api.DashboardBlock) => (entities.status === "ready" ? entities.data.find((entity) => entity.type === block.object) : undefined);
  const metrics = blocks.filter((block) => block.kind === "metric");
  const charts = blocks.filter((block) => block.kind !== "metric");
  return (
    <Layout
      height="fill"
      content={
        <LayoutContent role="main" padding={gutter}>
          <VStack gap={6}>
            <PageTitle title={t(title)} />
            <Grid columns={{ minWidth: 200, max: 4 }} gap={4}>
              {metrics.map((block) => <ErrorBoundary key={block.title} compact><Block block={block} entity={entityOf(block)} list={listOf(block.object)} /></ErrorBoundary>)}
            </Grid>
            <Grid columns={{ minWidth: 360, max: 2 }} gap={4}>
              {charts.map((block) => (
                <GridSpan key={block.title} columns={block.kind === "line" ? "full" : 1}>
                  <ErrorBoundary compact><Block block={block} entity={entityOf(block)} list={listOf(block.object)} /></ErrorBoundary>
                </GridSpan>
              ))}
            </Grid>
          </VStack>
        </LayoutContent>
      }
    />
  );
}

function Block({ block, entity, list }: { block: Api.DashboardBlock; entity?: Api.EntityInfo; list?: string }) {
  const measure = block.measure || "count";
  const base = Array.isArray(block.domain) ? (block.domain as unknown[]) : [];
  const read = useRead<Api.RecordsAggregate>("/v1/object-sets/aggregate", {
    set: base.length ? { object: block.object, domain: base } : { object: block.object },
    groups: block.group ? [block.group] : [],
    measures: [measure],
  });
  const [name, bucket] = (block.group ?? "").split(":");
  const field = entity?.fields.find((candidate) => candidate.name === name);
  const described = useMemo(() => (entity ? describe(entity, []) : undefined), [entity]);
  const options = useOptions(described, field && !bucket ? [field.name] : []);
  const href = (domain: unknown[]) => list && (domain.length ? `${list}?${new URLSearchParams({ domain: JSON.stringify(domain) })}` : list);
  const shown = useMemo<Shown | undefined>(() => {
    if (read.status !== "ready") return undefined;
    const money = Boolean(read.data.columns.find((column) => column.name === measure)?.money);
    const currencyKey = read.data.columns.find((column) => column.name.endsWith(".currency"))?.name;
    const currency = currencyKey ? String(read.data.rows.find((row) => row[currencyKey])?.[currencyKey] ?? "") : "";
    const rows = currencyKey ? read.data.rows.filter((row) => !row[currencyKey] || row[currencyKey] === currency) : read.data.rows;
    const format = (amount: number) => money
      ? `${(amount / 100).toLocaleString(undefined, { maximumFractionDigits: 0 })} ${currency}`.trim()
      : amount.toLocaleString(undefined, { maximumFractionDigits: 2 });
    const order = field && !bucket ? (options[field.name]?.map((option) => option.value) ?? field.choices ?? []) : [];
    const labelOf = (value: unknown) => {
      if (value == null || value === "") return t("Not set");
      if (bucket || !field) return String(value);
      return options[field.name]?.find((option) => option.value === String(value))?.label ?? (present(field, value) || String(value));
    };
    const points = rows.map((row) => {
      const value = block.group ? row[block.group] : undefined;
      return {
        key: String(value ?? ""),
        value,
        label: labelOf(value),
        amount: Number(row[measure] ?? 0),
        href: href(block.group && !bucket && value != null && value !== "" ? [...base, [name, "=", value]] : base),
      };
    });
    const ranked = order.length ? [...points].sort((a, b) => rank(order, a.key) - rank(order, b.key)) : points;
    return { format, money, points: ranked.map((point, index) => ({ ...point, hue: hues[index % hues.length] })) };
  }, [read, measure, field, bucket, options, block.group, base, name, list]);
  if (block.kind === "metric") {
    return (
      <ClickableCard label={t(block.title)} href={href(base)} isDisabled={!list} height="100%">
        <VStack gap={1}>
          <Text type="label" color="secondary">{t(block.title)}</Text>
          {shown ? <Text type="large" size="2xl" weight="semibold">{shown.points[0] ? shown.format(shown.points[0].amount) : "0"}</Text> : <Pending read={read} />}
        </VStack>
      </ClickableCard>
    );
  }
  return (
    <Card height="100%">
      <VStack gap={4}>
        <HStack hAlign="between" vAlign="center">
          <Heading level={4} accessibilityLevel={2}>{t(block.title)}</Heading>
          {list && <Link href={href(base) ?? list}>{t("View all")}</Link>}
        </HStack>
        {!shown ? <Pending read={read} /> : shown.points.length === 0 ? <Text type="supporting" color="secondary">{t("No data yet")}</Text> : block.kind === "share" ? <Share shown={shown} /> : <Chart kind={block.kind} shown={shown} />}
      </VStack>
    </Card>
  );
}

function rank(order: string[], key: string) {
  const at = order.indexOf(key);
  return at < 0 ? order.length : at;
}

function Pending({ read }: { read: Read<Api.RecordsAggregate> }) {
  if (read.status === "loading") return <Skeleton height={40} />;
  if (read.status === "refused") return <Text type="supporting" color="secondary">{read.reason || t("You do not have access to this data")}</Text>;
  return <Banner status="error" title={t("This page could not be loaded")} description={read.status === "error" ? read.reason : undefined} />;
}

function Swatch({ hue, label }: { hue: Hue; label: string }) {
  return (
    <HStack gap={2} vAlign="center">
      <HStack xstyle={styles.swatch(hue)} aria-hidden="true" />
      <Text type="supporting">{label}</Text>
    </HStack>
  );
}

function Share({ shown }: { shown: Shown }) {
  const total = shown.points.reduce((sum, point) => sum + point.amount, 0) || 1;
  const sorted = shown.points.length > 6 ? [...shown.points].sort((a, b) => b.amount - a.amount) : shown.points;
  const slices = sorted.length > 6
    ? [
        ...sorted.slice(0, 5).map((point, index) => ({ ...point, hue: hues[index % hues.length] })),
        { key: "__other", value: undefined, label: t("Other"), amount: sorted.slice(5).reduce((sum, point) => sum + point.amount, 0), hue: hues[5] },
      ]
    : sorted;
  return (
    <VStack gap={4}>
      <ResponsiveContainer width="100%" height={24}>
        <BarChart data={[Object.fromEntries(slices.map((point) => [point.key, point.amount]))]} layout="vertical" margin={{ top: 0, right: 0, bottom: 0, left: 0 }} barCategoryGap={0}>
          <XAxis type="number" hide />
          <YAxis type="category" hide />
          {slices.map((point, index) => (
            <Bar key={point.key} dataKey={point.key} name={point.label} stackId="share" fill={colour(point.hue)} isAnimationActive={false}
              radius={[index === 0 ? 4 : 0, index === slices.length - 1 ? 4 : 0, index === slices.length - 1 ? 4 : 0, index === 0 ? 4 : 0]}
              cursor={point.href ? "pointer" : undefined} onClick={() => point.href && void navigate(point.href)} />
          ))}
        </BarChart>
      </ResponsiveContainer>
      <HStack gap={4} wrap="wrap">
        {slices.map((point) => (
          <VStack key={point.key} gap={0}>
            {point.href ? <Link href={point.href}><Swatch hue={point.hue} label={point.label} /></Link> : <Swatch hue={point.hue} label={point.label} />}
            <Text type="supporting" color="secondary">{`${shown.format(point.amount)} · ${Math.round((point.amount / total) * 100)}%`}</Text>
          </VStack>
        ))}
      </HStack>
    </VStack>
  );
}

function Chart({ kind, shown }: { kind: string; shown: Shown }) {
  const compact = new Intl.NumberFormat(undefined, { notation: "compact" });
  const axis = (amount: number) => compact.format(shown.money ? amount / 100 : amount);
  const open = (point?: Point) => point?.href && void navigate(point.href);
  const tip = <Tooltip cursor={{ fill: grid, stroke: grid }} content={({ active, payload }) => (active && payload?.[0] ? <Tip point={payload[0].payload as Point} shown={shown} first={shown.points[0]?.amount} funnel={kind === "funnel"} /> : null)} />;
  if (kind === "line") {
    return (
      <ResponsiveContainer width="100%" height={240}>
        <LineChart data={shown.points} margin={{ top: 5, right: 10, left: 0, bottom: 5 }} onClick={(state) => open(shown.points[Number(state?.activeTooltipIndex)])}>
          <CartesianGrid horizontal vertical={false} stroke={grid} />
          <XAxis dataKey="label" tick={tick} axisLine={false} tickLine={false} />
          <YAxis tick={tick} axisLine={false} tickLine={false} width={48} tickFormatter={axis} allowDecimals={shown.money} />
          {tip}
          <Line type="linear" dataKey="amount" stroke={colour("blue")} strokeWidth={2} dot={shown.points.length < 3} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    );
  }
  const funnel = kind === "funnel";
  const trimTick = (value: string) => (value.length > 12 ? `${value.slice(0, 11)}…` : value);
  return (
    <ResponsiveContainer width="100%" height={funnel ? Math.max(160, shown.points.length * 40) : 240}>
      <BarChart data={shown.points} layout={funnel ? "vertical" : "horizontal"} margin={{ top: 0, right: 24, left: 8, bottom: 0 }} barCategoryGap={funnel ? 10 : "20%"} >
        <CartesianGrid horizontal={!funnel} vertical={funnel} stroke={grid} />
        {funnel ? <XAxis type="number" tick={tick} axisLine={false} tickLine={false} tickFormatter={axis} allowDecimals={shown.money} /> : <XAxis dataKey="label" interval={0} tickFormatter={trimTick} tick={tick} axisLine={false} tickLine={false} />}
        {funnel ? <YAxis type="category" dataKey="label" tick={tick} axisLine={false} tickLine={false} width={120} /> : <YAxis tick={tick} axisLine={false} tickLine={false} width={48} tickFormatter={axis} allowDecimals={shown.money} />}
        {tip}
        <Bar dataKey="amount" radius={4} isAnimationActive={false} cursor="pointer" onClick={(entry) => open(entry.payload as Point)}>
          {shown.points.map((point) => <Cell key={point.key} fill={colour(point.hue)} />)}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}

function Tip({ point, shown, first, funnel }: { point: Point; shown: Shown; first?: number; funnel: boolean }) {
  return (
    <Card padding={3}>
      <VStack gap={1}>
        <Text type="body" weight="semibold">{point.label}</Text>
        <Text type="supporting" color="secondary">{funnel && first ? `${shown.format(point.amount)} · ${Math.round((point.amount / first) * 100)}%` : shown.format(point.amount)}</Text>
      </VStack>
    </Card>
  );
}
