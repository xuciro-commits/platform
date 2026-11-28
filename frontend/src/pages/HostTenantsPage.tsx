import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { ArrowsUpDownIcon, PlusIcon } from "@heroicons/react/24/outline";
import { useMemo, useState } from "react";
import { type Api, reasonOf, reread, request, useRead } from "../host";
import { t } from "../i18n";
import { formatTenantNumber, NumberValue } from "../semantic";
import { PageTitle, useGutter } from "./frame";
import { NewWorkspaceDialog } from "./NewWorkspaceDialog";

type SortKey = "tenant" | "health" | "records" | "storage" | "lane" | "errorRate" | "backup";
type StatusFilter = "all" | "active" | "suspended" | "attention";
type LifecycleTarget = { tenant: string; action: "suspend" | "resume" };

const planLabels: Record<string, string> = {
  starter: "Starter",
  team: "Team",
  enterprise: "Enterprise",
};

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${formatTenantNumber(bytes, { maximumFractionDigits: 0 })} B`;
  if (bytes < 1024 * 1024) return `${formatTenantNumber(bytes / 1024, { maximumFractionDigits: 1 })} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${formatTenantNumber(bytes / (1024 * 1024), { maximumFractionDigits: 1 })} MB`;
  return `${formatTenantNumber(bytes / (1024 * 1024 * 1024), { maximumFractionDigits: 2 })} GB`;
}

function isSuspended(tenant: Api.HostTenantView): boolean {
  return Boolean(tenant.lifecycle && tenant.lifecycle !== "open" && tenant.lifecycle !== "active");
}

function healthInfo(tenant: Api.HostTenantView): { rank: number; label: string; variant: "success" | "warning" | "error" | "neutral" } {
  if (tenant.quarantined) return { rank: 3, label: "Quarantined", variant: "error" };
  if (isSuspended(tenant)) return { rank: 2, label: "Suspended", variant: "warning" };
  if (tenant.fault || tenant.healthStatus === "degraded" || tenant.failedWork > 0) {
    return { rank: 1, label: "Degraded", variant: "warning" };
  }
  return { rank: 0, label: "Healthy", variant: "success" };
}

function backupTimestamp(tenant: Api.HostTenantView): number {
  if (!tenant.lastBackup?.at) return 0;
  const parsed = Date.parse(tenant.lastBackup.at);
  return Number.isNaN(parsed) ? 0 : parsed;
}

function LifecycleDialog({ target, onClose }: { target: LifecycleTarget; onClose: () => void }) {
  const toast = useToast();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const suspending = target.action === "suspend";
  const heading = suspending
    ? t("Suspend {id}").replace("{id}", target.tenant)
    : t("Resume {id}").replace("{id}", target.tenant);

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    setFailure("");
    try {
      await request<Api.HostTenantView>(`/v1/host/tenants/${encodeURIComponent(target.tenant)}/lifecycle`, {
        body: { action: target.action, reason: reason.trim() || target.action },
      });
      reread();
      toast({
        body: suspending
          ? t("Workspace {id} suspended.").replace("{id}", target.tenant)
          : t("Workspace {id} resumed.").replace("{id}", target.tenant),
      });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={520} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={heading} onOpenChange={(open) => !open && onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {suspending
                  ? t("Suspending blocks sign-in and new writes immediately while preserving all accepted data. Recorded in audit.")
                  : t("Resuming restores sign-in and write access for this workspace. Recorded in audit.")}
              </Text>
              {failure && <Banner status="error" title={t("Action failed")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <TextInput label={t("Reason")} value={reason} onChange={setReason} placeholder={t("Operational reason recorded in audit")} />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button
                label={suspending ? t("Suspend workspace") : t("Resume workspace")}
                variant={suspending ? "destructive" : "primary"}
                isLoading={busy}
                onClick={() => void confirm()}
              />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

function PlanDialog({ tenant, currentPlan, onClose }: { tenant: string; currentPlan?: string; onClose: () => void }) {
  const toast = useToast();
  const plansRead = useRead<Api.QuotaPlan[]>("/v1/host/plans");
  const plans = plansRead.status === "ready" ? plansRead.data : [];
  const [plan, setPlan] = useState(currentPlan || "starter");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const assign = async () => {
    if (busy || !plan) return;
    setBusy(true);
    setFailure("");
    try {
      await request<Api.HostTenantView>(`/v1/host/tenants/${encodeURIComponent(tenant)}/quota`, {
        body: { plan, reason: reason.trim() || "console plan update" },
      });
      reread();
      toast({ body: t("Quota updated for {id}.").replace("{id}", tenant) });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={520} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("Assign plan to {id}").replace("{id}", tenant)} onOpenChange={(open) => !open && onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              {failure && <Banner status="error" title={t("Action failed")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <Selector
                  label={t("Plan")}
                  value={plan}
                  options={(plans.length > 0 ? plans : [{ id: "starter", title: "Starter" }, { id: "team", title: "Team" }, { id: "enterprise", title: "Enterprise" }]).map((item) => ({
                    value: item.id,
                    label: t(planLabels[item.id] ?? item.title ?? item.id),
                  }))}
                  onChange={setPlan}
                />
                <TextInput label={t("Reason")} value={reason} onChange={setReason} placeholder={t("Operational reason recorded in audit")} />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Assign plan")} variant="primary" isLoading={busy} onClick={() => void assign()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

export function HostTenantsPage() {
  const read = useRead<Api.HostOverview>("/v1/host/overview");
  const toast = useToast();
  const gutter = useGutter();
  const [creating, setCreating] = useState(false);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [sortKey, setSortKey] = useState<SortKey>("tenant");
  const [descending, setDescending] = useState(true);
  const [lifecycleTarget, setLifecycleTarget] = useState<LifecycleTarget | null>(null);
  const [planTenant, setPlanTenant] = useState<Api.HostTenantView | null>(null);
  const [backingUp, setBackingUp] = useState("");

  const tenants = read.status === "ready" ? read.data.tenants : [];

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = tenants.filter((tenant) => {
      if (q) {
        const hay = `${tenant.id} ${tenant.plan ?? ""} ${tenant.activeRelease ?? ""} ${tenant.fault ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (statusFilter === "active") return !isSuspended(tenant) && !tenant.quarantined;
      if (statusFilter === "suspended") return isSuspended(tenant);
      if (statusFilter === "attention") {
        return Boolean(tenant.quarantined || tenant.fault || tenant.failedWork > 0 || tenant.errorRate > 0 || tenant.lastBackup?.status === "failed" || (tenant.slowHoldMillis ?? 0) >= 10);
      }
      return true;
    });
    const sorted = [...filtered].sort((a, b) => {
      let cmp = 0;
      if (sortKey === "tenant") return a.id.localeCompare(b.id) * (descending ? 1 : -1);
      if (sortKey === "health") cmp = healthInfo(a).rank - healthInfo(b).rank;
      else if (sortKey === "records") cmp = (a.usage?.records ?? 0) - (b.usage?.records ?? 0);
      else if (sortKey === "storage") cmp = (a.usage?.storageBytes ?? 0) - (b.usage?.storageBytes ?? 0);
      else if (sortKey === "lane") cmp = Math.max(a.laneHeldMillis ?? 0, a.slowHoldMillis ?? 0) - Math.max(b.laneHeldMillis ?? 0, b.slowHoldMillis ?? 0);
      else if (sortKey === "errorRate") cmp = (a.errorRate ?? 0) - (b.errorRate ?? 0);
      else if (sortKey === "backup") cmp = backupTimestamp(a) - backupTimestamp(b);
      if (cmp === 0) return a.id.localeCompare(b.id);
      return descending ? -cmp : cmp;
    });
    return sorted;
  }, [tenants, search, statusFilter, sortKey, descending]);

  const runBackup = async (tenantId: string) => {
    if (backingUp) return;
    setBackingUp(tenantId);
    try {
      await request<Api.HostTenantView>(`/v1/host/tenants/${encodeURIComponent(tenantId)}/backup`, {
        body: { reason: "console backup" },
      });
      reread();
      toast({ body: t("Backup completed for {id}.").replace("{id}", tenantId) });
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setBackingUp("");
    }
  };

  const healthyCount = tenants.filter((item) => healthInfo(item).rank === 0).length;
  const suspendedCount = tenants.filter((item) => isSuspended(item)).length;
  const totalRecords = tenants.reduce((sum, item) => sum + (item.usage?.records ?? 0), 0);

  return (
    <Layout
      height="fill"
      content={
        <LayoutContent role="main" padding={0} label={t("Tenants")}>
          <VStack gap={5} padding={gutter} paddingBlockStart={3}>
            <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
              <PageTitle title={t("Tenants")} />
              <Button label={t("New workspace")} variant="primary" icon={<Icon icon={PlusIcon} size="sm" />} onClick={() => setCreating(true)} />
            </HStack>

            {read.status === "loading" && (
              <VStack gap={3}>
                {[0, 1, 2].map((index) => <Skeleton key={index} height={112} index={index} />)}
              </VStack>
            )}
            {read.status === "error" && (
              <Banner status="error" title={t("Host console could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
            )}
            {read.status === "refused" && (
              <EmptyState headingLevel={2} title={t("Host administrator access required")} description={read.reason || t("Sign in with a host operator account.")} />
            )}

            {read.status === "ready" && (
              <>
                <Grid columns={{ minWidth: 180, max: 4 }} gap={3}>
                  <Card padding={3}>
                    <VStack gap={1}>
                      <Text type="supporting" color="secondary">{t("Total tenants")}</Text>
                      <NumberValue value={tenants.length} size="lg" />
                    </VStack>
                  </Card>
                  <Card padding={3}>
                    <VStack gap={1}>
                      <Text type="supporting" color="secondary">{t("Healthy")}</Text>
                      <NumberValue value={healthyCount} size="lg" />
                    </VStack>
                  </Card>
                  <Card padding={3}>
                    <VStack gap={1}>
                      <Text type="supporting" color="secondary">{t("Suspended")}</Text>
                      <NumberValue value={suspendedCount} size="lg" />
                    </VStack>
                  </Card>
                  <Card padding={3}>
                    <VStack gap={1}>
                      <Text type="supporting" color="secondary">{t("Active records")}</Text>
                      <NumberValue value={totalRecords} size="lg" />
                    </VStack>
                  </Card>
                </Grid>

                <Card padding={3}>
                  <HStack gap={3} vAlign="end" wrap="wrap">
                    <StackItem size="fill">
                      <TextInput
                        label={t("Filter tenants")}
                        value={search}
                        onChange={setSearch}
                        placeholder={t("Search tenant ID, plan or release")}
                      />
                    </StackItem>
                    <Selector
                      label={t("Status")}
                      value={statusFilter}
                      options={[
                        { value: "all", label: t("All tenants") },
                        { value: "active", label: t("Active") },
                        { value: "suspended", label: t("Suspended") },
                        { value: "attention", label: t("Needs attention") },
                      ]}
                      onChange={(val) => setStatusFilter(val as StatusFilter)}
                    />
                    <Selector
                      label={t("Sort by")}
                      value={sortKey}
                      options={[
                        { value: "tenant", label: t("Tenant") },
                        { value: "health", label: t("Health") },
                        { value: "records", label: t("Records") },
                        { value: "storage", label: t("Storage") },
                        { value: "lane", label: t("Lane occupancy") },
                        { value: "errorRate", label: t("Error rate") },
                        { value: "backup", label: t("Last backup") },
                      ]}
                      onChange={(val) => setSortKey(val as SortKey)}
                    />
                    <Button
                      label={descending ? t("High to low") : t("Low to high")}
                      variant="secondary"
                      icon={<Icon icon={ArrowsUpDownIcon} size="sm" />}
                      onClick={() => setDescending((prev) => !prev)}
                    />
                  </HStack>
                </Card>

                {visible.length === 0 ? (
                  <EmptyState headingLevel={2} title={t("No matching tenants")} description={t("Adjust the search text or status filter.")} />
                ) : (
                  <VStack gap={3}>
                    {visible.map((tenant) => {
                      const health = healthInfo(tenant);
                      const suspended = isSuspended(tenant);
                      const records = tenant.usage?.records ?? 0;
                      const storage = formatBytes(tenant.usage?.storageBytes ?? 0);
                      const writes = tenant.usage?.writesToday ?? 0;
                      const aiTokens = tenant.usage?.aiTokens ?? 0;
                      const packages = tenant.usage?.installedPackages ?? 0;
                      const laneHeld = formatTenantNumber(tenant.laneHeldMillis ?? 0, { maximumFractionDigits: 1 });
                      const slowHold = tenant.slowHoldMillis ?? 0;
                      const errPct = formatTenantNumber((tenant.errorRate ?? 0) * 100, { maximumFractionDigits: 1 });
                      const planTitle = tenant.plan ? (planLabels[tenant.plan] ?? tenant.plan) : "";
                      const backup = tenant.lastBackup;
                      const backupText = !backup
                        ? t("No backup yet")
                        : backup.status === "failed"
                          ? t("Backup failed: {error}").replace("{error}", backup.error || t("Unknown error"))
                          : t("Backup OK · {size} ({at})").replace("{size}", formatBytes(backup.bytes)).replace("{at}", backup.at.slice(0, 19).replace("T", " "));

                      return (
                        <Card key={tenant.id} padding={3}>
                          <VStack gap={3}>
                            <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
                              <HStack gap={2} vAlign="center" wrap="wrap">
                                <Heading level={3}>{tenant.id}</Heading>
                                <Badge variant={health.variant} label={t(health.label)} />
                                <Badge variant={planTitle ? "neutral" : "neutral"} label={planTitle ? t(planTitle) : t("Unassigned")} />
                                {slowHold >= 10 && (
                                  <Badge
                                    variant="warning"
                                    label={t("Lane slow hold {ms} ms").replace("{ms}", formatTenantNumber(slowHold, { maximumFractionDigits: 1 }))}
                                  />
                                )}
                              </HStack>
                              <HStack gap={2} vAlign="center" wrap="wrap">
                                <Button
                                  label={t("Plan for {id}").replace("{id}", tenant.id)}
                                  variant="secondary"
                                  size="sm"
                                  onClick={() => setPlanTenant(tenant)}
                                />
                                <Button
                                  label={t("Backup {id}").replace("{id}", tenant.id)}
                                  variant="secondary"
                                  size="sm"
                                  isLoading={backingUp === tenant.id}
                                  onClick={() => void runBackup(tenant.id)}
                                />
                                {suspended ? (
                                  <Button
                                    label={t("Resume {id}").replace("{id}", tenant.id)}
                                    variant="primary"
                                    size="sm"
                                    onClick={() => setLifecycleTarget({ tenant: tenant.id, action: "resume" })}
                                  />
                                ) : (
                                  <Button
                                    label={t("Suspend {id}").replace("{id}", tenant.id)}
                                    variant="secondary"
                                    size="sm"
                                    onClick={() => setLifecycleTarget({ tenant: tenant.id, action: "suspend" })}
                                  />
                                )}
                              </HStack>
                            </HStack>

                            <Grid columns={{ minWidth: 180, max: 4 }} gap={3}>
                              <VStack gap={1}>
                                <Text type="supporting" color="secondary">{t("Usage")}</Text>
                                <Text type="code" hasTabularNumbers>
                                  {t("{records} records · {storage} · {writes} writes/d · {tokens} tok · {packages} pkg")
                                    .replace("{records}", formatTenantNumber(records, { maximumFractionDigits: 0 }))
                                    .replace("{storage}", storage)
                                    .replace("{writes}", formatTenantNumber(writes, { maximumFractionDigits: 0 }))
                                    .replace("{tokens}", formatTenantNumber(aiTokens, { maximumFractionDigits: 0 }))
                                    .replace("{packages}", formatTenantNumber(packages, { maximumFractionDigits: 0 }))}
                                </Text>
                              </VStack>
                              <VStack gap={1}>
                                <Text type="supporting" color="secondary">{t("Lane occupancy")}</Text>
                                <Text type="code" hasTabularNumbers>
                                  {t("{held} ms held · {waiters} waiting")
                                    .replace("{held}", laneHeld)
                                    .replace("{waiters}", String(tenant.laneWaiters ?? 0))}
                                </Text>
                              </VStack>
                              <VStack gap={1}>
                                <Text type="supporting" color="secondary">{t("Error rate")}</Text>
                                <Text type="code" hasTabularNumbers>
                                  {t("{rate}% ({errors}/{requests})")
                                    .replace("{rate}", errPct)
                                    .replace("{errors}", String(tenant.errors ?? 0))
                                    .replace("{requests}", String(tenant.requests ?? 0))}
                                </Text>
                              </VStack>
                              <VStack gap={1}>
                                <Text type="supporting" color="secondary">{t("Last backup")}</Text>
                                <Text type="code" hasTabularNumbers>{backupText}</Text>
                              </VStack>
                            </Grid>
                          </VStack>
                        </Card>
                      );
                    })}
                  </VStack>
                )}
              </>
            )}
          </VStack>
          {creating && <NewWorkspaceDialog onClose={() => setCreating(false)} />}
          {lifecycleTarget && <LifecycleDialog target={lifecycleTarget} onClose={() => setLifecycleTarget(null)} />}
          {planTenant && <PlanDialog tenant={planTenant.id} currentPlan={planTenant.plan} onClose={() => setPlanTenant(null)} />}
        </LayoutContent>
      }
    />
  );
}
