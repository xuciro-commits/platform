import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { Button, EmptyState, Input, PageHeader, useWorkspace, t, type PlatformApplication } from "@platform/ui";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { Star } from "lucide-react";
import { useMemo, useState } from "react";
import { applicationGroup, categories, projections, type Projection } from "./registry";

function ApplicationTile({ app, role }: { app: PlatformApplication; role?: string }) {
  const { applications, favorites, toggleFavorite } = useWorkspace();
  const pinned = favorites.includes(app.id);
  return <HStack gap={1} vAlign="start">
    <StackItem size="fill"><ClickableCard label={app.title} onClick={() => applications?.onSelect(app.id)} padding={3} width="100%">
      <HStack gap={3} vAlign="start">
        <Text color="accent">{app.icon}</Text>
        <StackItem size="fill"><VStack gap={0.5}>
          <Text weight="medium" maxLines={1}>{app.title}</Text>
          {(app.description || role) && <Text type="supporting" maxLines={2}>{app.description ?? role}</Text>}
        </VStack></StackItem>
      </HStack>
    </ClickableCard></StackItem>
    <Button variant="ghost" size="icon" aria-label={pinned ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={pinned}
      title={pinned ? t("Remove from favorites") : t("Add to favorites")} onClick={() => toggleFavorite(app.id)}>
      <Icon icon={Star} color={pinned ? "warning" : "inherit"} />
    </Button>
  </HStack>;
}

export function ApplicationsPortal({ projection }: { projection?: Projection }) {
  const { applications } = useWorkspace();
  const { me } = useHost();
  const directory = useReadQuery<Api.ResourceDirectory>("/v1/resources");
  const [query, setQuery] = useState("");
  const lead = projections.find((p) => p.id === projection)?.categories;
  const apps = useMemo(() => {
    const text = query.trim().toLowerCase();
    return (applications?.apps ?? []).filter(a => !text || `${a.title} ${a.description ?? ""} ${a.id}`.toLowerCase().includes(text)).sort((a, b) => a.title.localeCompare(b.title));
  }, [applications?.apps, query]);
  const ordered = [...categories].sort((a, b) => Number(!!lead && !lead.includes(a.id)) - Number(!!lead && !lead.includes(b.id)));
  const role = (id: string) => me.apps.find((e) => e.id === id)?.role;
  return <VStack gap={4} maxWidth={1152}>
    <PageHeader title={t("Applications")} meta={t("{count} applications", { count: applications?.apps.length ?? 0 })}
      help={t("Every application you may open in {tenant}: the ones your organisation runs, and the platform's own tools your role allows.", { tenant: me.tenantId })}
      actions={<HStack width={288}><Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Find an application…")} aria-label={t("Find an application…")} /></HStack>} />
    {ordered.map((category) => {
      const inCategory = apps.filter((a) => a.category === category.id);
      if (!inCategory.length) return null;
      const groups = new Map<string, { label: string; apps: PlatformApplication[] }>();
      if (category.id === "business") {
        for (const app of inCategory) {
          const group = applicationGroup(app, directory.data?.resources ?? []) ?? { id: "_other", label: t("Other applications") };
          const held = groups.get(group.id) ?? { label: group.label, apps: [] };
          held.apps.push(app);
          groups.set(group.id, held);
        }
      }
      return <VStack key={category.id} gap={2} role="group" aria-label={category.label()}>
        <Text as="p" role="heading" aria-level={2} weight="semibold" color="secondary">{category.label()}</Text>
        {category.id === "business" ? <VStack gap={3}>{[...groups.entries()].sort(([, a], [, b]) => a.label.localeCompare(b.label)).map(([id, group]) => <VStack key={id} gap={2} role="group" aria-label={group.label}>
          <Text as="p" role="heading" aria-level={3} weight="medium">{group.label}</Text>
          <Grid columns={{ minWidth: 240, max: 6 }} gap={2}>{group.apps.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}</Grid>
        </VStack>)}</VStack>
          : <Grid columns={{ minWidth: 240, max: 6 }} gap={2}>{inCategory.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}</Grid>}
      </VStack>;
    })}
    {!apps.length && <EmptyState title={t("No application matches.")} action={query.trim() ? <Button onClick={() => setQuery("")}>{t("Clear search")}</Button> : undefined} />}
  </VStack>;
}