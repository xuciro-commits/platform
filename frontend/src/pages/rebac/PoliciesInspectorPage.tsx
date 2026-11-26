import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useMemo, useState } from "react";
import { type Api, request, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { useIsDark } from "../../shell/appearance";
import { reasonOf, send } from "../act";
import { PageTitle, useGutter } from "../frame";
import { PolicyDialog } from "./PolicyDialog";
import { AnatomyPlate, GatePipelineTrace, ProvenanceFlowCard } from "./RebacVisuals";
import { spanAlive, todayISO, useMemberDirectoryMap, useUnitChoices } from "./shared";

type TabId = "inspector" | "policies";
type InspectMode = "record" | "permission";

export function PoliciesInspectorPage() {
  const gutter = useGutter();
  const dark = useIsDark();
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const access = useRead<Api.AccessConfig>("/v1/access");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const members = useRead<Api.MemberView[]>("/v1/members");
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const enterprise = useRead<Api.EnterpriseModel>("/v1/enterprise");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { options: memberOptions, nameOf: memberName } = useMemberDirectoryMap();
  const { unitName } = useUnitChoices();

  const [tab, setTab] = useState<TabId>("inspector");
  const [mode, setMode] = useState<InspectMode>("record");
  const [subject, setSubject] = useState("");
  const [kind, setKind] = useState<Api.AuthzRequest["kind"]>("read");
  const [objectType, setObjectType] = useState("");
  const [recordId, setRecordId] = useState("");
  const [actionSchema, setActionSchema] = useState("");
  const [permissionQuery, setPermissionQuery] = useState("");
  const [verdict, setVerdict] = useState<Api.Verdict | null>(null);
  const [evalBusy, setEvalBusy] = useState(false);
  const [evalError, setEvalError] = useState("");
  const [policyOpen, setPolicyOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<Api.PolicyRecord | null>(null);
  const [removingPolicy, setRemovingPolicy] = useState<Api.PolicyRecord | null>(null);
  const [policyBusy, setPolicyBusy] = useState(false);

  const permList = permissions.status === "ready" ? permissions.data : [];
  const entityList = entities.status === "ready" ? entities.data : [];
  const policies = access.status === "ready" ? access.data.policies : [];
  const actions = catalog.status === "ready" ? catalog.data : [];
  const savePolicyAction = actions.find((item) => item.schema === "platform.policy.save");
  const removePolicyAction = actions.find((item) => item.schema === "platform.policy.remove");

  const chosenSubject = memberOptions.some((item) => item.value === subject)
    ? subject
    : me.status === "ready" ? me.data.principalId : memberOptions[0]?.value ?? "";

  const chosenType = entityList.some((item) => item.type === objectType)
    ? objectType
    : entityList.find((item) => !item.type.startsWith("build.") && !item.type.startsWith("work."))?.type ?? entityList[0]?.type ?? "";

  const sampleRecords = useRead<Api.RecordPage>(mode === "record" && chosenType ? `/v1/records/${encodeURIComponent(chosenType)}?limit=30` : null);
  const recordOptions = useMemo(() => {
    if (sampleRecords.status !== "ready") return [];
    const displayField = entityList.find((item) => item.type === chosenType)?.display ?? "name";
    return (sampleRecords.data.records as Record<string, unknown>[]).map((row) => {
      const id = String(row.id ?? "");
      const title = String(row[displayField] ?? row.title ?? row.name ?? id);
      return { value: id, label: title !== id ? `${title} (${id})` : id };
    });
  }, [sampleRecords, entityList, chosenType]);

  const typeActions = useMemo(
    () => actions.filter((act) => act.target === chosenType).map((act) => ({ value: act.schema, label: `${t(act.title)} (${act.schema})` })),
    [actions, chosenType],
  );

  const allPermissionOptions = useMemo(() => {
    const out: { value: string; label: string }[] = [];
    const seen = new Set<string>();
    for (const app of permList) {
      for (const readName of [...app.everyone, ...app.reads]) {
        const key = `${app.app}:read:${readName}`;
        if (!seen.has(key)) { seen.add(key); out.push({ value: key, label: `${t(app.title || app.app)} · read:${readName}` }); }
      }
      for (const role of app.roles) {
        for (const act of role.actions) {
          if (!seen.has(act.id)) { seen.add(act.id); out.push({ value: act.id, label: `${t(app.title || app.app)} · ${act.title ? t(act.title) : act.id} (${act.id})` }); }
        }
      }
    }
    return out;
  }, [permList]);

  const chosenPermission = allPermissionOptions.some((item) => item.value === permissionQuery) ? permissionQuery : allPermissionOptions[0]?.value ?? "";
  const explained = useRead<Api.Explanation>(
    mode === "permission" && chosenSubject && chosenPermission
      ? `/v1/authz/explain?member=${encodeURIComponent(chosenSubject)}&permission=${encodeURIComponent(chosenPermission)}`
      : null,
  );

  const subjectProfile = useMemo(() => {
    const today = todayISO();
    const memberView = members.status === "ready" ? members.data.find((item) => item.id === chosenSubject) : undefined;
    let personName = "";
    const units: string[] = [];
    if (enterprise.status === "ready" && chosenSubject) {
      const model = enterprise.data;
      const elemName = (id: string) => model.elements.find((el) => el.id === id)?.name ?? id;
      const signs = model.relationships.find((rel) => rel.stereotype === "SignsInAs" && rel.source === `member:${chosenSubject}` && spanAlive(rel, today));
      if (signs?.target) personName = elemName(signs.target);
      for (const rel of model.relationships) {
        if (rel.stereotype === "ActualOrganizationRole" && spanAlive(rel, today) && (rel.source === `member:${chosenSubject}` || (signs?.target && rel.source === signs.target))) {
          const label = rel.role ? `${elemName(rel.target)} (${t(rel.role)})` : elemName(rel.target);
          if (!units.includes(label)) units.push(label);
        }
      }
    }
    return { memberView, personName, units };
  }, [members, enterprise, chosenSubject]);

  const runRecordExplain = async () => {
    if (!chosenSubject || me.status !== "ready") return;
    setEvalBusy(true);
    setEvalError("");
    try {
      const body: Api.AuthzRequest = {
        principal: chosenSubject,
        tenant: me.data.tenantId,
        kind,
        type: chosenType || undefined,
        record: recordId.trim() || undefined,
        action: kind === "action" ? actionSchema || typeActions[0]?.value : undefined,
      };
      setVerdict(await request<Api.Verdict>("/v1/authz/explain", { body }));
    } catch (problem) {
      setEvalError(reasonOf(problem));
    } finally {
      setEvalBusy(false);
    }
  };

  const activeVerdict = mode === "permission" ? (explained.status === "ready" ? explained.data.verdict : null) : verdict;

  const policyColumns: TableColumn<Api.PolicyRecord>[] = [
    {
      key: "title",
      header: t("Policy"),
      width: proportional(1.2),
      renderCell: (pol) => (
        <VStack gap={0.5}>
          <Text weight="medium" maxLines={1}>{pol.title || pol.id}</Text>
          <Text type="code" color="secondary" maxLines={1}>{pol.id}</Text>
        </VStack>
      ),
    },
    {
      key: "effect",
      header: t("Effect"),
      width: pixel(108),
      renderCell: (pol) => <Token size="sm" color={pol.effect === "deny" ? "red" : "green"} label={t(pol.effect === "deny" ? "Deny" : "Allow")} />,
    },
    { key: "permission", header: t("Permission"), width: proportional(1.2), renderCell: (pol) => <Text type="code" maxLines={1}>{pol.permission}</Text> },
    {
      key: "conditions",
      header: t("Conditions & window"),
      width: proportional(1.8),
      renderCell: (pol) => {
        const parts: string[] = [];
        if (pol.where?.member) parts.push(`${t("Member")}: ${memberName(pol.where.member)}`);
        if (pol.where?.app) parts.push(`${t("App")}: ${pol.where.app}`);
        if (pol.where?.target) parts.push(`${t("Target")}: ${pol.where.target}`);
        if (pol.where?.agent === "true") parts.push(t("Agents only"));
        if (pol.from) parts.push(`${t("From")} ${pol.from}`);
        if (pol.until) parts.push(t("Until {date}").replace("{date}", pol.until));
        return parts.length > 0 ? (
          <HStack gap={1} wrap="wrap">{parts.map((part) => <Token key={part} size="sm" color="default" label={part} />)}</HStack>
        ) : (
          <Text color="secondary">{t("Always active")}</Text>
        );
      },
    },
    {
      key: "actions",
      header: t("Actions"),
      width: pixel(156),
      align: "end",
      renderCell: (pol) => (
        <HStack gap={1.5} hAlign="end">
          {savePolicyAction && <Button label={t("Edit")} variant="secondary" size="sm" onClick={() => { setEditingPolicy(pol); setPolicyOpen(true); }} />}
          {removePolicyAction && <Button label={t("Remove")} variant="secondary" size="sm" onClick={() => setRemovingPolicy(pol)} />}
        </HStack>
      ),
    },
  ];

  const confirmRemovePolicy = async () => {
    if (!removePolicyAction || !removingPolicy) return;
    setPolicyBusy(true);
    try {
      await send(removePolicyAction, removingPolicy.id, {});
      reread();
      setRemovingPolicy(null);
      toast({ body: t("Policy removed.") });
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setPolicyBusy(false);
    }
  };

  return (
    <>
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={0} hasDivider>
            <VStack gap={3} padding={gutter} paddingBlockStart={3} paddingBlockEnd={0}>
              <PageTitle
                title={t("Policies & inspector")}
                actions={
                  <HStack gap={2} vAlign="center">
                    <IconButton label={t("Refresh")} tooltip={t("Refresh")} variant="ghost" size="sm" icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
                    {savePolicyAction && (
                      <Button label={t("New guardrail policy")} variant="primary" onClick={() => { setEditingPolicy(null); setPolicyOpen(true); }} />
                    )}
                  </HStack>
                }
              >
                <Text type="supporting" color="secondary">
                  {t("Simulate why a member can or cannot access a specific record or action across all 9 ReBAC layers, and manage explicit allow/deny guardrail policies.")}
                </Text>
              </PageTitle>

              <TabList value={tab} onChange={(next) => setTab(next as TabId)} role="tablist" isFullBleed>
                <Tab value="inspector" label={t("Access inspector")} />
                <Tab value="policies" label={t("Guardrail policies")} endContent={policies.length > 0 ? <Badge variant="neutral" label={String(policies.length)} /> : undefined} />
              </TabList>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={4}>
              {tab === "inspector" && (
                <VStack gap={4}>
                  <Card padding={4}>
                    <VStack gap={3}>
                      <HStack gap={3} vAlign="end" wrap="wrap">
                        <VStack gap={1}>
                          <Text type="supporting" color="secondary">{t("Inspection target")}</Text>
                          <SegmentedControl label={t("Inspection target")} size="sm" value={mode} onChange={(next) => setMode(next as InspectMode)}>
                            <SegmentedControlItem value="record" label={t("Object & record (9-layer ReBAC)")} />
                            <SegmentedControlItem value="permission" label={t("Action or read endpoint")} />
                          </SegmentedControl>
                        </VStack>
                        <Selector label={t("Member")} width={240} hasSearch value={chosenSubject} onChange={setSubject} options={memberOptions} />
                      </HStack>

                      {mode === "record" ? (
                        <HStack gap={2} vAlign="end" wrap="wrap">
                          <Selector
                            label={t("Operation")}
                            width={130}
                            value={kind}
                            onChange={(next) => setKind(next as Api.AuthzRequest["kind"])}
                            options={[
                              { value: "read", label: t("Read") },
                              { value: "write", label: t("Write") },
                              { value: "create", label: t("Create") },
                              { value: "archive", label: t("Archive") },
                              { value: "action", label: t("Custom action") },
                              { value: "export", label: t("Export") },
                            ]}
                          />
                          <Selector
                            label={t("Object type")}
                            width={220}
                            hasSearch
                            value={chosenType}
                            onChange={(next) => { setObjectType(next); setRecordId(""); setActionSchema(""); }}
                            options={entityList.map((item) => ({ value: item.type, label: `${t(item.title || item.type)} (${item.type})` }))}
                          />
                          {recordOptions.length > 0 && (
                            <Selector
                              label={t("Sample record")}
                              width={195}
                              hasSearch={recordOptions.length > 8}
                              value={recordOptions.some((item) => item.value === recordId) ? recordId : ""}
                              onChange={setRecordId}
                              options={[{ value: "", label: t("Collection level (any row)") }, ...recordOptions]}
                            />
                          )}
                          <TextInput label={t("Record ID")} width={145} value={recordId} placeholder={t("Optional row ID")} onChange={setRecordId} />
                          {kind === "action" && (
                            <Selector label={t("Action")} width={220} value={actionSchema || typeActions[0]?.value || ""} onChange={setActionSchema} options={typeActions} />
                          )}
                          <Button label={t("Evaluate access")} variant="primary" isLoading={evalBusy} isDisabled={!chosenSubject || !chosenType} onClick={() => void runRecordExplain()} />
                        </HStack>
                      ) : (
                        <HStack gap={2} vAlign="end" wrap="wrap">
                          <Selector label={t("Permission")} width={420} hasSearch value={chosenPermission} onChange={setPermissionQuery} options={allPermissionOptions} />
                        </HStack>
                      )}
                    </VStack>
                  </Card>

                  {evalError && <Banner status="error" title={t("Evaluation failed")} description={evalError} />}

                  {chosenSubject && (
                    <ProvenanceFlowCard
                      memberId={chosenSubject}
                      memberDisplay={memberName(chosenSubject)}
                      personName={subjectProfile.personName}
                      isAgent={subjectProfile.memberView?.agent}
                      units={subjectProfile.units}
                      grants={subjectProfile.memberView?.grants ?? []}
                      markings={subjectProfile.memberView?.markings ?? []}
                      memberName={memberName}
                      unitName={unitName}
                    />
                  )}

                  {activeVerdict ? (
                    <GatePipelineTrace verdict={activeVerdict} />
                  ) : (
                    <Card variant="muted" padding={6}>
                      <VStack gap={3} hAlign="center">
                        <AnatomyPlate kind="prism" dark={dark} />
                        <Heading level={3}>{t("Ready to trace 9-layer authorization")}</Heading>
                        <Text type="supporting" color="secondary">
                          {t("Pick a member, object type and optional record ID above, then run Evaluate access to inspect every security gate and field mask.")}
                        </Text>
                      </VStack>
                    </Card>
                  )}
                </VStack>
              )}

              {tab === "policies" && (
                <VStack gap={4}>
                  {access.status === "loading" && <Skeleton height={160} />}
                  {access.status === "ready" && policies.length === 0 && (
                    <Card variant="muted" padding={6}>
                      <VStack gap={3} hAlign="center">
                        <AnatomyPlate kind="vault" dark={dark} />
                        <Heading level={2}>{t("No explicit guardrail policies")}</Heading>
                        <Text type="supporting" color="secondary">
                          {t("Add a deny or allow policy to enforce emergency freezes, agent guardrails or time-bounded exceptions.")}
                        </Text>
                        {savePolicyAction && (
                          <Button label={t("New guardrail policy")} variant="primary" onClick={() => { setEditingPolicy(null); setPolicyOpen(true); }} />
                        )}
                      </VStack>
                    </Card>
                  )}
                  {policies.length > 0 && <Table data={policies} columns={policyColumns} idKey="id" dividers="rows" hasHover textOverflow="truncate" />}
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
      />

      {savePolicyAction && (
        <PolicyDialog isOpen={policyOpen} editing={editingPolicy} permissions={permList} memberOptions={memberOptions} saveAction={savePolicyAction} onClose={() => setPolicyOpen(false)} />
      )}

      <AlertDialog
        isOpen={Boolean(removingPolicy)}
        title={t("Remove guardrail policy?")}
        description={t("This policy stops taking effect immediately across all sessions.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Remove")}
        actionVariant="destructive"
        isActionLoading={policyBusy}
        onOpenChange={(open) => !open && !policyBusy && setRemovingPolicy(null)}
        onAction={() => void confirmRemovePolicy()}
      />
    </>
  );
}
