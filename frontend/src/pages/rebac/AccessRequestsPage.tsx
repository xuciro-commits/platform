import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useMemo, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../../host";
import { t } from "../../i18n";
import { useIsDark } from "../../shell/appearance";
import { PageTitle, useGutter } from "../frame";
import { PersonLabel } from "../../semantic";
import { AccessRequestDecisionCard } from "./AccessRequestDecisionCard";
import { AnatomyPlate, MetricStrip } from "./RebacVisuals";
import { RequestAccessDialog } from "./RequestAccessDialog";
import {
  daysUntil,
  grantSourceSummary,
  roleTitle,
  todayISO,
  useMemberDirectoryMap,
  useUnitChoices,
} from "./shared";

type TabId = "requests" | "temporary";
type StateFilter = "all" | "pending" | "approved" | "rejected" | "withdrawn";

type TemporaryGrantRow = {
  id: string;
  subjectKind: "member" | "team" | "project";
  subjectId: string;
  subjectLabel: string;
  targetLabel: string;
  originLabel: string;
  from: string;
  until: string;
  status: "active" | "scheduled" | "expiring";
  daysLeft: number | null;
  revokeMember?: string;
  revokeApp?: string;
  revokeRole?: string;
  revokeUnit?: string;
  revokeBy?: string;
};

export function AccessRequestsPage() {
  const gutter = useGutter();
  const dark = useIsDark();
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const requests = useRead<Api.AccessRequest[]>("/v1/access-requests");
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const members = useRead<Api.MemberView[]>("/v1/members");
  const access = useRead<Api.AccessConfig>("/v1/access");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { nameOf: memberName } = useMemberDirectoryMap();
  const { unitName } = useUnitChoices();

  const [tab, setTab] = useState<TabId>("requests");
  const [stateFilter, setStateFilter] = useState<StateFilter>("pending");
  const [search, setSearch] = useState("");
  const [requestOpen, setRequestOpen] = useState(false);
  const [revokeBusy, setRevokeBusy] = useState("");
  const [failure, setFailure] = useState("");

  const today = todayISO(me.status === "ready" ? me.data.account.effective.timezone : undefined);
  const appTitle = useMemo(() => {
    const map = new Map<string, string>();
    if (apps.status === "ready") {
      for (const entry of apps.data) map.set(entry.id, entry.title || entry.id);
    }
    return (id?: string) => (id ? map.get(id) ?? id : "");
  }, [apps]);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const revokeAction = actions.find((item) => item.schema === "platform.member.revoke");

  const allRequests = requests.status === "ready" ? requests.data : [];
  const pendingCount = allRequests.filter((item) => item.state === "pending").length;
  const awaitingMeCount = allRequests.filter((item) => item.state === "pending" && item.canDecide).length;

  const filteredRequests = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return allRequests.filter((item) => {
      if (stateFilter !== "all" && item.state !== stateFilter) return false;
      if (!needle) return true;
      const hay = [
        item.requester,
        memberName(item.requester),
        item.app,
        appTitle(item.app),
        item.project,
        item.role,
        item.reason,
        item.unit ? unitName(item.unit) : "",
      ].filter(Boolean).join(" ").toLowerCase();
      return hay.includes(needle);
    });
  }, [allRequests, stateFilter, search, memberName, appTitle, unitName]);

  const temporaryRows = useMemo<TemporaryGrantRow[]>(() => {
    const rows: TemporaryGrantRow[] = [];
    if (members.status === "ready") {
      for (const member of members.data) {
        const displayName = member.profile?.displayName || memberName(member.id) || member.id;
        for (const grant of member.grants ?? []) {
          if (!grant.until && !grant.from) continue;
          const origin = grantSourceSummary(grant, memberName);
          if (origin.kind === "team" || origin.kind === "project") continue;
          const left = daysUntil(grant.until, today);
          const target = `${t(appTitle(grant.app))} · ${roleTitle(grant.role)}${grant.unit ? ` (${unitName(grant.unit)})` : ""}`;
          rows.push({
            id: `member:${member.id}:${grant.app}:${grant.role}:${grant.unit ?? ""}:${grant.by ?? ""}:${grant.until ?? ""}`,
            subjectKind: "member",
            subjectId: member.id,
            subjectLabel: displayName !== member.id ? `${displayName} (${member.id})` : member.id,
            targetLabel: target,
            originLabel: origin.label,
            from: grant.from ?? "",
            until: grant.until ?? "",
            status: left !== null && left <= 7 ? "expiring" : "active",
            daysLeft: left,
            revokeMember: origin.revocable ? member.id : undefined,
            revokeApp: grant.app,
            revokeRole: grant.role,
            revokeUnit: grant.unit,
            revokeBy: grant.by,
          });
        }
        for (const grant of member.scheduledGrants ?? []) {
          const origin = grantSourceSummary(grant, memberName);
          const target = `${t(appTitle(grant.app))} · ${roleTitle(grant.role)}${grant.unit ? ` (${unitName(grant.unit)})` : ""}`;
          rows.push({
            id: `scheduled:${member.id}:${grant.app}:${grant.role}:${grant.unit ?? ""}:${grant.from ?? ""}`,
            subjectKind: "member",
            subjectId: member.id,
            subjectLabel: displayName !== member.id ? `${displayName} (${member.id})` : member.id,
            targetLabel: target,
            originLabel: origin.label,
            from: grant.from ?? "",
            until: grant.until ?? "",
            status: "scheduled",
            daysLeft: daysUntil(grant.until, today),
            revokeMember: member.id,
            revokeApp: grant.app,
            revokeRole: grant.role,
            revokeUnit: grant.unit,
            revokeBy: grant.by,
          });
        }
      }
    }
    if (access.status === "ready") {
      for (const team of access.data.teams) {
        for (const grant of team.grants ?? []) {
          if (!grant.until && !grant.from) continue;
          const left = daysUntil(grant.until, today);
          rows.push({
            id: `team:${team.id}:${grant.app}:${grant.role}:${grant.unit ?? ""}:${grant.until ?? ""}`,
            subjectKind: "team",
            subjectId: team.id,
            subjectLabel: team.name,
            targetLabel: `${t(appTitle(grant.app))} · ${roleTitle(grant.role)}${grant.unit ? ` (${unitName(grant.unit)})` : ""}`,
            originLabel: `${t("Team")} (${team.members.length})`,
            from: grant.from ?? "",
            until: grant.until ?? "",
            status: grant.from && grant.from > today ? "scheduled" : left !== null && left <= 7 ? "expiring" : "active",
            daysLeft: left,
          });
        }
      }
    }
    return rows.sort((a, b) => (a.until || "9999-12-31").localeCompare(b.until || "9999-12-31"));
  }, [members, access, memberName, appTitle, unitName, today]);

  const expiringSoonCount = temporaryRows.filter((row) => row.status === "expiring").length;
  const pulseMetrics = useMemo(
    () => [
      {
        key: "awaiting",
        label: t("Awaiting your decision"),
        value: awaitingMeCount,
        caption: t("Ready for one-click approval"),
        dot: awaitingMeCount > 0 ? ("warning" as const) : ("success" as const),
        pulse: awaitingMeCount > 0,
      },
      {
        key: "pending",
        label: t("Pending requests"),
        value: pendingCount,
        caption: t("Open across workspace"),
        dot: pendingCount > 0 ? ("accent" as const) : ("neutral" as const),
      },
      {
        key: "temp",
        label: t("Time-bounded grants"),
        value: temporaryRows.length,
        caption: t("Auto-expiring & delegated"),
        dot: "accent" as const,
      },
      {
        key: "expiring",
        label: t("Expiring within 7 days"),
        value: expiringSoonCount,
        caption: t("Automatic zero-standing cleanup"),
        dot: expiringSoonCount > 0 ? ("warning" as const) : ("neutral" as const),
      },
    ],
    [awaitingMeCount, pendingCount, temporaryRows.length, expiringSoonCount],
  );

  const revokeRow = async (row: TemporaryGrantRow) => {
    if (!revokeAction || !row.revokeMember || !row.revokeApp) return;
    setRevokeBusy(row.id);
    setFailure("");
    try {
      await send(revokeAction, row.revokeMember, {
        app: row.revokeApp,
        role: row.revokeRole ?? "",
        unit: row.revokeUnit ?? "",
        by: row.revokeBy ?? "",
      });
      reread();
      toast({ body: t("Temporary grant revoked.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setRevokeBusy("");
    }
  };

  const temporaryColumns: TableColumn<TemporaryGrantRow>[] = [
    {
      key: "subject",
      header: t("Holder"),
      width: proportional(1.3),
      renderCell: (row) =>
        row.subjectKind === "member" ? (
          <PersonLabel id={row.subjectId} name={row.subjectLabel} weight="medium" />
        ) : (
          <HStack gap={1.5} vAlign="center">
            <Token size="sm" color="purple" label={t("Team")} />
            <Text weight="medium" maxLines={1}>{row.subjectLabel}</Text>
          </HStack>
        ),
    },
    {
      key: "target",
      header: t("Role & scope"),
      width: proportional(1.6),
      renderCell: (row) => <Text maxLines={1}>{row.targetLabel}</Text>,
    },
    {
      key: "origin",
      header: t("Origin"),
      width: proportional(1.1),
      renderCell: (row) => <Text color="secondary" maxLines={1}>{row.originLabel}</Text>,
    },
    {
      key: "window",
      header: t("Validity window"),
      width: proportional(1.2),
      renderCell: (row) => (
        <HStack gap={1.5} vAlign="center" wrap="wrap">
          <StatusDot
            variant={row.status === "expiring" ? "warning" : row.status === "scheduled" ? "accent" : "success"}
            label={t(row.status)}
          />
          {row.status === "scheduled" ? (
            <Token size="sm" color="blue" label={t("Starts {date}").replace("{date}", row.from)} />
          ) : row.until ? (
            <Token
              size="sm"
              color={row.status === "expiring" ? "orange" : "green"}
              label={row.daysLeft !== null && row.daysLeft >= 0
                ? t("Until {date} ({days}d)").replace("{date}", row.until).replace("{days}", String(row.daysLeft))
                : t("Until {date}").replace("{date}", row.until)}
            />
          ) : (
            <Token size="sm" color="gray" label={t("No end")} />
          )}
        </HStack>
      ),
    },
    {
      key: "actions",
      header: t("Actions"),
      width: pixel(110),
      align: "end",
      renderCell: (row) =>
        revokeAction && row.revokeMember ? (
          <Button
            label={t("Revoke")}
            variant="secondary"
            size="sm"
            isLoading={revokeBusy === row.id}
            isDisabled={Boolean(revokeBusy)}
            onClick={() => void revokeRow(row)}
          />
        ) : null,
    },
  ];

  return (
    <>
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={0} hasDivider>
            <VStack gap={3} padding={gutter} paddingBlockStart={3} paddingBlockEnd={0}>
              <PageTitle
                title={t("Access requests")}
                actions={
                  <HStack gap={2} vAlign="center">
                    <IconButton
                      label={t("Refresh")}
                      tooltip={t("Refresh")}
                      variant="ghost"
                      size="sm"
                      icon={<Icon icon={ArrowPathIcon} size="sm" />}
                      onClick={reread}
                    />
                    <Button label={t("Request access")} variant="primary" onClick={() => setRequestOpen(true)} />
                  </HStack>
                }
              >
                <Text type="supporting" color="secondary">
                  {t("Review role and project access requests, adjust expiration dates before approving, and audit time-bounded grants across the workspace.")}
                </Text>
              </PageTitle>

              <TabList value={tab} onChange={(next) => setTab(next as TabId)} role="tablist" isFullBleed>
                <Tab
                  value="requests"
                  label={t("Request queue")}
                  endContent={pendingCount > 0 ? <Badge variant="neutral" label={String(pendingCount)} /> : undefined}
                />
                <Tab
                  value="temporary"
                  label={t("Temporary grants")}
                  endContent={temporaryRows.length > 0 ? <Badge variant="neutral" label={String(temporaryRows.length)} /> : undefined}
                />
              </TabList>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={4}>
              {failure && <Banner status="error" title={t("Action failed")} description={failure} />}
              <MetricStrip items={pulseMetrics} />

              {tab === "requests" && (
                <VStack gap={4}>
                  <HStack gap={3} vAlign="center" wrap="wrap">
                    <SegmentedControl
                      label={t("Status")}
                      size="sm"
                      value={stateFilter}
                      onChange={(next) => setStateFilter(next as StateFilter)}
                    >
                      <SegmentedControlItem value="pending" label={t("Pending")} />
                      <SegmentedControlItem value="approved" label={t("Approved")} />
                      <SegmentedControlItem value="rejected" label={t("Rejected")} />
                      <SegmentedControlItem value="withdrawn" label={t("Withdrawn")} />
                      <SegmentedControlItem value="all" label={t("All")} />
                    </SegmentedControl>
                    <StackItem size="fill">
                      <TextInput
                        label={t("Search requests")}
                        isLabelHidden
                        size="sm"
                        value={search}
                        placeholder={t("Search by member, app, project, role or reason")}
                        onChange={setSearch}
                      />
                    </StackItem>
                  </HStack>

                  {requests.status === "loading" && (
                    <VStack gap={2}>
                      <Skeleton height={96} />
                      <Skeleton height={96} />
                    </VStack>
                  )}
                  {(requests.status === "error" || requests.status === "refused") && (
                    <Banner status="error" title={t("Could not load access requests")} description={requests.reason} />
                  )}
                  {requests.status === "ready" && filteredRequests.length === 0 && (
                    <Card variant="muted" padding={6}>
                      <VStack gap={3} hAlign="center">
                        <AnatomyPlate kind="gate" dark={dark} />
                        <Heading level={2}>
                          {stateFilter === "pending" ? t("No pending access requests") : t("No matching access requests")}
                        </Heading>
                        <Text type="supporting" color="secondary">
                          {t("Members can request application or project roles with an automatic expiration date.")}
                        </Text>
                        <Button label={t("Request access")} variant="secondary" onClick={() => setRequestOpen(true)} />
                      </VStack>
                    </Card>
                  )}
                  {requests.status === "ready" && filteredRequests.map((item) => (
                    <AccessRequestDecisionCard
                      key={item.id}
                      request={item}
                      appTitle={appTitle}
                      unitLabel={unitName}
                      memberName={memberName}
                      today={today}
                      actions={actions}
                    />
                  ))}
                </VStack>
              )}

              {tab === "temporary" && (
                <VStack gap={4}>
                  {members.status === "loading" && <Skeleton height={160} />}
                  {members.status === "ready" && temporaryRows.length === 0 && (
                    <Card variant="muted" padding={6}>
                      <VStack gap={3} hAlign="center">
                        <AnatomyPlate kind="gate" dark={dark} />
                        <Heading level={2}>{t("No temporary or scheduled grants")}</Heading>
                        <Text type="supporting" color="secondary">
                          {t("Grants with a start or expiration date, including peer delegations and time-bounded approvals, appear here.")}
                        </Text>
                      </VStack>
                    </Card>
                  )}
                  {temporaryRows.length > 0 && (
                    <Table
                      data={temporaryRows}
                      columns={temporaryColumns}
                      idKey="id"
                      dividers="rows"
                      hasHover
                      textOverflow="truncate"
                    />
                  )}
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
      />

      <RequestAccessDialog isOpen={requestOpen} onClose={() => setRequestOpen(false)} />
    </>
  );
}
