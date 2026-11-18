import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { useRef, useState } from "react";
import type { Api } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { type BatchRow, sendBatch } from "./act";
import type { Described, Values } from "./describe";
import { enterable } from "./ParameterInput";

type Batch = { action: Api.Action; rows: BatchRow[] };

export function BatchActions({ described, records, ids, clear }: { described: Described; records: Api.RecordPage; ids: string[]; clear: () => void }) {
  const [pending, setPending] = useState<Batch>();
  const [result, setResult] = useState<{ action: Api.Action; rows: BatchRow[]; payload: Values }>();
  const [busy, setBusy] = useState(false);
  const locked = useRef(false);
  const can = ids.length ? (records.actions?.[ids[0]] ?? []).flatMap((item) => {
    const action = described.action(item.schema);
    return action && !action.new && action.schema !== described.edit?.schema && item.allowed &&
      ids.every((id) => records.actions?.[id]?.some((current) => current.schema === action.schema && current.allowed)) ? [action] : [];
  }) : [];
  const run = async (batch: Batch, payload: Values, retry = false) => {
    if (locked.current) return;
    locked.current = true;
    setBusy(true);
    const updated = await sendBatch(batch.action, batch.rows, payload, retry);
    const replacements = new Map(updated.map((row) => [row.id, row]));
    setResult({ action: batch.action, payload, rows: retry && result ? result.rows.map((row) => replacements.get(row.id) ?? row) : updated });
    setPending(undefined);
    clear();
    setBusy(false);
    locked.current = false;
  };
  const start = (action: Api.Action) => {
    const rows = (records.records as Values[]).filter((row) => ids.includes(String(row.id))).map((row) => ({
      id: String(row.id), name: described.name(row, String(row.id)), revision: Number(row.revision),
    }));
    const batch = { action, rows };
    if (!action.reverse || action.payload.some(enterable)) setPending(batch);
    else void run(batch, {});
  };
  const label = pending ? t(pending.action.title) : "";
  const heading = pending ? `${label} · ${pending.rows.length} ${t("records")}` : "";
  const failed = result?.rows.filter((row) => row.reason) ?? [];
  return (
    <>
      {can.length > 0 && (
        <HStack gap={2} wrap="wrap">
          {can.map((action) => <Button key={action.schema} label={t(action.title)} variant="secondary" size="sm" isDisabled={busy} onClick={() => start(action)} />)}
        </HStack>
      )}
      {result && (
        <VStack gap={2}>
          {result.rows.map((row) => <Text key={row.id} type="body">{row.name}: {row.reason || t("Done")}</Text>)}
          {failed.length > 0 && <Button label={t("Retry failed rows")} variant="secondary" isDisabled={busy} onClick={() => run({ action: result.action, rows: failed }, result.payload, true)} />}
        </VStack>
      )}
      {pending && (pending.action.payload.some(enterable) ? (
        <ActionDialog action={pending.action} described={described.entity} heading={heading} confirm={label} revision={0} onClose={() => !locked.current && setPending(undefined)} perform={(payload) => run(pending, payload)} onDone={() => setPending(undefined)} />
      ) : (
        <AlertDialog isOpen title={heading} description={t("This cannot be undone here.")} actionLabel={label} onOpenChange={(open) => !open && !locked.current && setPending(undefined)} isActionLoading={busy} onAction={() => run(pending, {})} />
      ))}
    </>
  );
}
