import "../i18n";
import { Button, Input, Notice, PageHeader, t, useSearch, type SearchAnswer } from "@platform/ui";
import { Heading } from "@astryxdesign/core/Heading";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { useCallback, useEffect, useRef, useState } from "react";

export function Search({ initial = "" }: { initial?: string }) {
  const search = useSearch();
  const [q, setQ] = useState(initial);
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<{ text: string; answer: SearchAnswer }>();
  const ticket = useRef(0);
  const run = useCallback((text: string) => {
    const current = ++ticket.current;
    setPending(true);
    void search.search(text).then(answer => {
      if (current === ticket.current) setResult({ text, answer });
    }).catch(error => {
      if (current === ticket.current) setResult({ text, answer: { groups: [], errors: [error instanceof Error ? error.message : String(error)] } });
    }).finally(() => {
      if (current === ticket.current) setPending(false);
    });
  }, [search]);
  useEffect(() => { run(initial); }, [initial, run]);
  const answer = result?.text === q ? result.answer : undefined;
  return (
    <VStack gap={3} maxWidth={896}>
      <PageHeader title={t("Search")} help={t("Search commands, applications, resources, objects, members and knowledge, only within what you may see.")} />
      <form onSubmit={event => { event.preventDefault(); run(q); }}>
        <Input aria-label={t("Search")} autoFocus placeholder={t("Search or run a command…")} value={q} onChange={event => setQ(event.target.value)} />
      </form>
      {pending && <Text as="p" role="status" type="supporting">{t("Searching records…")}</Text>}
      {!!answer?.errors.length && <VStack gap={1} role="alert">
        <Notice tone="danger" message={`${t("Search failed.")} ${answer.errors.join(" ")}`} />
        <Button size="sm" variant="ghost" onClick={() => run(q)}>{t("Retry")}</Button>
      </VStack>}
      {!pending && answer && answer.groups.length === 0 && <Text as="p" type="supporting">{t("No results")}</Text>}
      {answer?.groups.map(group => (
        <section key={group.kind} aria-label={group.label}>
          <VStack gap={1}>
            <Heading level={2}>{group.label}</Heading>
            {group.items.map(item => (
              <Button key={item.id} variant="row" disabled={!!item.disabled} title={item.disabled}
                onClick={event => search.open(item, event.metaKey || event.ctrlKey ? "beside" : undefined)}>
                {item.icon}{item.label}
                {item.detail && <Text type="supporting" maxLines={1}>{item.detail}</Text>}
              </Button>
            ))}
          </VStack>
        </section>
      ))}
    </VStack>
  );
}