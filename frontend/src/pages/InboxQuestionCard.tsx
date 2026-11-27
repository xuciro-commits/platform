import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { useState } from "react";
import { type Api, reasonOf, reread, send, useRead } from "../host";
import { t } from "../i18n";
import { RelatedRecord } from "./InboxRelatedRecord";

type QuestionValues = {
  action?: string;
  title?: string;
  reason?: string;
  target?: string;
  payload?: string;
  options?: string[];
  optionTitles?: string[];
  recommend?: number;
  state?: string;
  answer?: string;
  outcome?: string;
  caller?: { kind?: string; name?: string; tool?: string; model?: string };
};

function formatParamValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "boolean") return value ? t("Yes") : t("No");
  if (typeof value === "number" || typeof value === "string") return String(value);
  if (Array.isArray(value)) return value.map((entry) => formatParamValue(entry)).join(", ");
  if (typeof value === "object") {
    const obj = value as Record<string, unknown>;
    if (typeof obj.amount === "number" && typeof obj.currency === "string") {
      return `${(obj.amount / 100).toLocaleString()} ${obj.currency}`;
    }
    return Object.entries(obj)
      .map(([k, v]) => `${k}: ${formatParamValue(v)}`)
      .join(", ");
  }
  return String(value);
}

function parseParameters(raw: string | undefined, fields: Api.Field[] | undefined): { label: string; value: string }[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return [];
    return Object.entries(parsed as Record<string, unknown>).map(([key, val]) => {
      const matched = fields?.find((field) => field.name === key);
      return { label: matched?.label || matched?.description || key, value: formatParamValue(val) };
    });
  } catch {
    return [];
  }
}

export function InboxQuestionSection({ questionId }: { questionId: string }) {
  const questionRead = useRead<Api.RecordView>(`/v1/records/work.question/${encodeURIComponent(questionId)}`);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  if (questionRead.status === "loading" || catalog.status === "loading") {
    return (
      <VStack gap={2}>
        <Skeleton height={24} index={0} />
        <Skeleton height={24} index={1} />
      </VStack>
    );
  }
  if (questionRead.status === "error" || questionRead.status === "refused") {
    return <Banner status="error" title={t("Question could not be loaded")} description={questionRead.reason || t("Reload the page to try again.")} />;
  }
  if (catalog.status !== "ready") return null;

  const values = (questionRead.data.values ?? {}) as QuestionValues;
  const options = values.options ?? ["approve", "reject"];
  const optionTitles = values.optionTitles ?? ["Approve", "Reject"];
  const recommend = typeof values.recommend === "number" && values.recommend >= 0 && values.recommend < options.length ? values.recommend : 0;
  const recommendedTitle = optionTitles[recommend] ?? options[recommend] ?? "";
  const state = values.state ?? "open";
  const targetAction = catalog.data.find((entry) => entry.schema === values.action);
  const answerAction = catalog.data.find((entry) => entry.schema === "work.question.answer");
  const params = parseParameters(values.payload, targetAction?.payload);
  const targetRef = values.target && values.target.includes("/") ? values.target : undefined;
  const callerText = values.caller?.name ? `${values.caller.name}${values.caller.kind ? ` (${values.caller.kind})` : ""}` : values.caller?.kind;

  const stateMeta: Record<string, { color: "orange" | "green" | "red" | "gray"; label: string }> = {
    open: { color: "orange", label: "Open" },
    approved: { color: "green", label: "Approved" },
    answered: { color: "green", label: "Answered" },
    rejected: { color: "red", label: "Rejected" },
    expired: { color: "gray", label: "Expired" },
    refused: { color: "red", label: "Refused when run" },
  };
  const badge = stateMeta[state] ?? { color: "gray" as const, label: state };

  const choose = async (choice: string) => {
    if (!answerAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(answerAction, questionId, { choice }, (questionRead.data.record as { revision?: number } | undefined)?.revision);
      reread();
      toast({
        body: choice === "approve" ? t("Question approved.") : choice === "reject" ? t("Question rejected.") : t("Question answered."),
      });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <VStack gap={3}>
      {failure && <Banner status="error" title={t("Not done")} description={failure} />}
      <MetadataList>
        {targetRef && (
          <MetadataListItem label={t("Record")}>
            <RelatedRecord reference={targetRef} />
          </MetadataListItem>
        )}
        {callerText && (
          <MetadataListItem label={t("Caller")}>
            <Text type="body">{callerText}</Text>
          </MetadataListItem>
        )}
        {recommendedTitle && (
          <MetadataListItem label={t("Recommended")}>
            <Token size="sm" color="blue" label={t(recommendedTitle)} />
          </MetadataListItem>
        )}
        {state !== "open" && (
          <MetadataListItem label={t("Status")}>
            <Token size="sm" color={badge.color} label={t(badge.label)} />
          </MetadataListItem>
        )}
        {values.answer && (
          <MetadataListItem label={t("Answer")}>
            <Text type="body">{t(optionTitles[options.indexOf(values.answer)] ?? values.answer)}</Text>
          </MetadataListItem>
        )}
        {values.outcome && (
          <MetadataListItem label={t("Outcome")}>
            <Text type="body">{values.outcome}</Text>
          </MetadataListItem>
        )}
        {params.map((param) => (
          <MetadataListItem key={param.label} label={param.label}>
            <Text type="body">{param.value}</Text>
          </MetadataListItem>
        ))}
      </MetadataList>
      {state === "open" && answerAction && (
        <HStack gap={2} wrap="wrap">
          {options.map((option, index) => (
            <Button
              key={option}
              label={t(optionTitles[index] ?? option)}
              variant={index === recommend ? "primary" : "secondary"}
              isDisabled={busy}
              isLoading={busy && index === recommend}
              onClick={() => void choose(option)}
            />
          ))}
        </HStack>
      )}
    </VStack>
  );
}
