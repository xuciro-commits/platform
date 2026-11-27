import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { DateInput } from "@astryxdesign/core/DateInput";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { useState } from "react";
import { type Api, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { reasonOf, send } from "../act";
import { PersonLabel, TemporalValue } from "../../semantic";
import { addDaysISO, asIsoDate, roleTitle } from "./shared";

export function AccessRequestDecisionCard({
  request,
  appTitle,
  unitLabel,
  memberName,
  today,
  actions,
}: {
  request: Api.AccessRequest;
  appTitle: (id?: string) => string;
  unitLabel: (id: string) => string;
  memberName: (id?: string) => string;
  today: string;
  actions: Api.Action[];
}) {
  const toast = useToast();
  const [untilOverride, setUntilOverride] = useState(request.until ?? "");
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const approveAction = actions.find((item) => item.schema === "platform.access.approve");
  const rejectAction = actions.find((item) => item.schema === "platform.access.reject");
  const withdrawAction = actions.find((item) => item.schema === "platform.access.withdraw");
  const me = useRead<Api.MeView>("/v1/me");
  const principalId = me.status === "ready" ? me.data.principalId : "";
  const isMine = request.requester === principalId;
  const minDate = addDaysISO(1, today);

  const targetSummary = request.scope === "project"
    ? `${t("Project")}: ${request.project ?? ""} · ${roleTitle(request.role ?? "")}`
    : `${t(appTitle(request.app))} · ${roleTitle(request.role ?? "")}${request.unit ? ` · ${unitLabel(request.unit)}` : ""}`;

  const stateColor = request.state === "approved" ? "green"
    : request.state === "rejected" ? "red"
    : request.state === "pending" ? "orange"
    : "gray";

  const decide = async (schema: "approve" | "reject" | "withdraw") => {
    const action = schema === "approve" ? approveAction : schema === "reject" ? rejectAction : withdrawAction;
    if (!action) return;
    setBusy(true);
    setFailure("");
    try {
      const payload = schema === "approve"
        ? { comment: comment.trim(), ...(untilOverride && untilOverride !== (request.until ?? "") ? { until: untilOverride } : {}) }
        : schema === "reject"
          ? { comment: comment.trim() }
          : {};
      await send(action, request.id, payload, request.revision || undefined);
      reread();
      setComment("");
      toast({
        body: schema === "approve"
          ? t("Access request approved.")
          : schema === "reject"
            ? t("Access request rejected.")
            : t("Access request withdrawn."),
      });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card padding={4}>
      <VStack gap={3}>
        {failure && <Banner status="error" title={t("Action failed")} description={failure} />}
        <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
          <HStack gap={2} vAlign="center" wrap="wrap">
            <Heading level={4}>{targetSummary}</Heading>
            <Token size="sm" color={stateColor} label={t(request.state)} />
          </HStack>
          {request.created.at && <TemporalValue value={request.created.at} kind="datetime" mode="relative" size="xs" surface="inline" />}
        </HStack>

        <MetadataList>
          <MetadataListItem label={t("Requester")}>
            <PersonLabel id={request.requester} name={memberName(request.requester)} />
          </MetadataListItem>
          <MetadataListItem label={t("Requested duration")}>
            <Text type="body">
              {request.until ? t("Until {date}").replace("{date}", request.until) : t("No end")}
            </Text>
          </MetadataListItem>
          {request.unit && (
            <MetadataListItem label={t("Organization unit")}>
              <Text type="body">{unitLabel(request.unit)}</Text>
            </MetadataListItem>
          )}
          <MetadataListItem label={t("Business reason")}>
            <Text type="body">{request.reason}</Text>
          </MetadataListItem>
          {request.approvers.length > 0 && (
            <MetadataListItem label={t("Eligible approvers")}>
              <Text type="body">{request.approvers.map((id) => memberName(id)).join(", ")}</Text>
            </MetadataListItem>
          )}
          {request.decidedBy && (
            <MetadataListItem label={t("Decided by")}>
              <HStack gap={2} vAlign="center" wrap="wrap">
                <PersonLabel id={request.decidedBy} name={memberName(request.decidedBy)} />
                {request.granted && <Text type="supporting" color="secondary">{request.granted}</Text>}
                {request.outcome && <Text type="supporting" color="secondary">{request.outcome}</Text>}
              </HStack>
            </MetadataListItem>
          )}
        </MetadataList>

        {request.state === "pending" && request.canDecide && approveAction && rejectAction && (
          <HStack gap={2} vAlign="end" wrap="wrap">
            <DateInput
              label={t("Grant until")}
              isOptional
              hasClear
              min={minDate}
              width={190}
              format="system_date"
              value={asIsoDate(untilOverride)}
              onChange={(next) => setUntilOverride(next ?? "")}
            />
            <TextInput
              label={t("Decision note")}
              isOptional
              width={260}
              value={comment}
              placeholder={t("Optional note for the audit trail")}
              onChange={setComment}
            />
            <Button
              label={t("Approve")}
              variant="primary"
              size="sm"
              isDisabled={busy || Boolean(untilOverride && untilOverride <= today)}
              isLoading={busy}
              onClick={() => void decide("approve")}
            />
            <Button
              label={t("Reject")}
              variant="secondary"
              size="sm"
              isDisabled={busy}
              onClick={() => void decide("reject")}
            />
          </HStack>
        )}

        {request.state === "pending" && isMine && withdrawAction && (
          <HStack gap={2}>
            <Button
              label={t("Withdraw request")}
              variant="secondary"
              size="sm"
              isDisabled={busy}
              onClick={() => void decide("withdraw")}
            />
          </HStack>
        )}
      </VStack>
    </Card>
  );
}
