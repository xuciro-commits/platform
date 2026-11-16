import { Button } from "../primitives/button";
import { HStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { t } from "../i18n";
import { validTermCounts, type TermCount } from "./TermCounts";

export function TagCounts({ terms, label, selected = [], enabled = true, onSelect }: { terms: TermCount[]; label: string; selected?: readonly string[]; enabled?: boolean; onSelect?: (value?: string) => void }) {
  if (!validTermCounts(terms)) return <p role="alert">{t("Term counts are invalid or exceed their budget.")}</p>;
  if (!terms.length) return <p role="status">{t("No terms in the matching records.")}</p>;
  const title = (term: TermCount) => term.value === null ? t("No value (missing)") : term.value === "" ? t("Empty text") : term.value;
  return <div className="grid min-w-0 gap-2">
    <HStack gap={1} wrap="wrap" role="group" aria-label={label}>
      {terms.map((term) => {
        const picked = term.value !== null && selected.includes(term.value);
        return <Token key={JSON.stringify(term.value)} label={`${title(term)} · ${term.count}`} size="sm" color={picked ? "blue" : "gray"} isDisabled={!enabled} onClick={onSelect && term.value !== null ? () => { if (enabled) onSelect(term.value!); } : undefined} />;
      })}
    </HStack>
    {onSelect && <HStack gap={2} wrap="wrap">
      <Button size="sm" variant="ghost" disabled={!enabled} onClick={() => { if (enabled) onSelect(); }}>{t("Clear group filter")}</Button>
      <Text type="supporting">{t("Missing groups cannot be written to text filters.")}</Text>
    </HStack>}
  </div>;
}
