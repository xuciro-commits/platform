import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { useEffect, useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, type Intent } from "./act";
import type { Values } from "./describe";
import { compareDraftObject, RecordComparison } from "./RecordComparison";

export function PublishDialog({ intent, cancel, confirm, busy }: { intent: Intent; cancel: () => void; confirm: () => void; busy: boolean }) {
  const [check, setCheck] = useState<Api.ActionCheckResult>();
  const [failure, setFailure] = useState("");
  const draftView = useRead<Api.RecordView>(`/v1/records/${encodeURIComponent(intent.action.target)}/${encodeURIComponent(intent.id)}`);
  useEffect(() => {
    const controller = new AbortController();
    request<Api.ActionCheckResult>(`/v1/actions/${intent.action.schema}/check`, {
      body: { target: { object: intent.action.target, id: intent.id } },
      signal: controller.signal,
    }).then(setCheck, (problem) => {
      if (!controller.signal.aborted) setFailure(reasonOf(problem));
    });
    return () => controller.abort();
  }, [intent]);
  const impact = check?.impact;
  const rows = useMemo(() => {
    if (draftView.status !== "ready") return [];
    return compareDraftObject(draftView.data.record as Values | undefined, draftView.data.changes, {
      refs: draftView.data.refs,
      impact,
    });
  }, [draftView, impact]);
  const stale = impact && impact.revision !== intent.revision;
  const reason = intent.publishProblem || failure || (stale ? t("The draft changed. Close and review it again.") : check?.allowed === false ? check.message || t("Publishing is unavailable.") : "");
  const title = `${intent.label} “${intent.name}”?`;
  const available = Boolean(check?.allowed && !reason);
  return (
    <Dialog isOpen width={680} onOpenChange={(open) => !open && !busy && cancel()} purpose="form">
      <Layout header={<DialogHeader title={title} onOpenChange={(open) => !open && !busy && cancel()} />} content={
        <LayoutContent padding={4}>
          <VStack gap={3}>
            {!check && !failure && <Text type="body">{t("Checking the effects of publishing…")}</Text>}
            {impact && (
              <>
                <Text type="body">{t("Records reviewed:")} {impact.records.toLocaleString()}</Text>
                <RecordComparison rows={rows} beforeHeader={t("Published")} afterHeader={t("Draft")} />
                {impact.fields.map((field) => (
                  <Text key={field.field} type="body">
                    {field.field} · {field.change === "removed" ? t("Removed field") : t("Changed type")} · {field.records.toLocaleString()} {t("records with data")}
                  </Text>
                ))}
              </>
            )}
            {reason && <Banner status="error" title={t("Publishing is unavailable.")} description={reason} />}
          </VStack>
        </LayoutContent>
      } footer={
        <LayoutFooter padding={4}>
          <HStack gap={2} hAlign="end">
            <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={cancel} />
            <Button label={intent.label} variant="primary" isDisabled={busy || !available} onClick={confirm} />
          </HStack>
        </LayoutFooter>
      } />
    </Dialog>
  );
}
