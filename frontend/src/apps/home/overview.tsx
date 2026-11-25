import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Heading, Text } from "@astryxdesign/core/Text";
import { RectangleGroupIcon } from "@heroicons/react/24/outline";
import type { Api } from "../../host";
import { useRead } from "../../host";
import { WorkspaceWelcome } from "../../pages/WorkspaceWelcome";
import { dashboardPage } from "../../pages/DashboardPage";
import { PageTitle, useGutter } from "../../pages/frame";
import { iconOf } from "../../pages/icons";
import { dateText, t } from "../../i18n";

type Directory = { resources: Api.ResourceEntry[] };

const ofKind = (answer: Directory, kind: string) => answer.resources.filter((resource) => resource.ref.kind === kind);
const latest = (answer: Directory, kind: string) =>
  ofKind(answer, kind)
    .sort((a, b) => (b.changed.at ?? "").localeCompare(a.changed.at ?? ""))
    .slice(0, 8)
    .map((resource) => ({
      id: resource.id,
      title: resource.title,
      owner: resource.ref.app,
      changed: resource.changed.at ? dateText(resource.changed.at) : "",
    }));
const columns = [
  { key: "title", header: "Name" },
  { key: "owner", header: "Owner", width: 120 },
  { key: "changed", header: "Updated", width: 180 },
];

const Dashboard = dashboardPage<Directory>({
  title: "Overview",
  read: "/v1/resources",
  metrics: [
    { title: "Object types", href: "/ontology/object-types", value: (answer) => ofKind(answer, "object").length },
    { title: "Action types", href: "/ontology/action-types", value: (answer) => ofKind(answer, "action").length },
    { title: "Modules", href: "/apps/modules", value: (answer) => ofKind(answer, "app").length },
    { title: "Pages", href: "/apps/modules", value: (answer) => ofKind(answer, "page").length },
  ],
  tables: [
    { title: "Recently updated object types", label: "All object types", href: "/ontology/object-types", columns, rows: (answer) => latest(answer, "object") },
    { title: "Recently updated modules", label: "All modules", href: "/apps/modules", columns, rows: (answer) => latest(answer, "app") },
  ],
});

function BusinessOverview({ apps }: { apps: Api.ApplicationView[] }) {
  const gutter = useGutter();
  return (
    <Layout
      height="fill"
      content={
        <LayoutContent role="main" padding={0}>
          <VStack gap={6} padding={gutter} paddingBlockStart={3}>
            <PageTitle title={t("Overview")} />
            <Grid columns={{ minWidth: 280, max: 3 }} gap={4}>
              {apps.map((app) => {
                const id = app.name || app.app;
                const first = app.dashboard?.length ? "dashboard" : app.menu.flatMap((group) => group.entries)[0]?.name ?? "";
                return (
                  <ClickableCard key={id} label={t(app.title)} href={first ? `/${id}/${first}` : `/${id}`} height="100%">
                    <VStack gap={2}>
                      <HStack gap={2} vAlign="center">
                        <Icon icon={iconOf(app.icon, RectangleGroupIcon)} size="md" />
                        <Heading level={4} accessibilityLevel={2}>{t(app.title)}</Heading>
                      </HStack>
                      {app.description && <Text type="supporting" color="secondary" maxLines={2}>{t(app.description)}</Text>}
                    </VStack>
                  </ClickableCard>
                );
              })}
            </Grid>
          </VStack>
        </LayoutContent>
      }
    />
  );
}

function HomeOverview() {
  const gutteredMe = useRead<Api.MeView>("/v1/me");
  const resources = useRead<Directory>("/v1/resources");
  const installed = useRead<Api.ApplicationView[]>("/v1/applications");
  const builder = gutteredMe.status !== "ready"
    || Boolean(gutteredMe.data.hostAdmin)
    || gutteredMe.data.apps.some((entry) => entry.id === "platform" || (entry.id === "build" && entry.role !== "user"));
  if (!builder && installed.status === "ready") {
    const visible = installed.data.filter((app) => app.surface !== "master-data");
    return visible.length > 0 ? <BusinessOverview apps={visible} /> : <WorkspaceWelcome canInstall={false} />;
  }
  if (resources.status === "ready" && installed.status === "ready" && resources.data.resources.length === 0 && installed.data.length === 0) {
    return <WorkspaceWelcome canInstall={builder} />;
  }
  return <Dashboard />;
}

export default HomeOverview;
