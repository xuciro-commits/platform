import { Card } from "@astryxdesign/core/Card";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { PageTitle, useGutter } from "./frame";
import { ListView } from "./ListPage";
import { MemberRoles } from "./MemberRoles";

export function MemberDirectoryEntry({ id }: { id: string }) {
  const gutter = useGutter();
  const members = useRead<Api.MemberView[]>("/v1/members");
  const directory = useRead<Api.MemberOption[]>("/v1/member-directory");

  if (members.status === "loading") {
    return (
      <VStack gap={3} padding={gutter}>
        <Skeleton height={40} />
        <Skeleton height={180} />
      </VStack>
    );
  }

  if (members.status === "ready") {
    const member = members.data.find((candidate) => candidate.id === id);
    if (!member) {
      return (
        <EmptyState
          headingLevel={1}
          title={t("Member not found")}
          description={t("This member is not registered in this workspace.")}
        />
      );
    }
    return (
      <Layout
        height="fill"
        contentWidth={960}
        header={
          <LayoutHeader padding={0} hasDivider>
            <VStack padding={gutter} paddingBlockStart={3} paddingBlockEnd={4}>
              <PageTitle
                title={member.profile?.displayName || member.id}
                back={{ href: "/admin/members", label: t("Members") }}
              />
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <Card padding={4}>
              <MemberRoles member={member} />
            </Card>
          </LayoutContent>
        }
      />
    );
  }

  return (
    <ListView
      read={directory}
      definition={{
        title: "Members",
        about: "A member is a person or agent who can sign in to this workspace.",
        rows: (rows) => rows.filter((member) => member.id === id),
        id: (member) => member.id,
        columns: [{ key: "name", header: "Member", text: (member) => member.name }],
      }}
    />
  );
}
