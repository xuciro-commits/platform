import "../i18n";
import { Button, Input, PageHeader, t, useSearch, type SearchAnswer } from "@platform/ui";
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
    <div className="grid max-w-4xl gap-3">
      <PageHeader title={t("Search")} help={t("Search commands, applications, resources, objects, members and knowledge, only within what you may see.")} />
      <form onSubmit={event => { event.preventDefault(); run(q); }}>
        <Input aria-label={t("Search")} autoFocus placeholder={t("Search or run a command…")} value={q} onChange={event => setQ(event.target.value)} />
      </form>
      {pending && <p role="status" className="text-sm text-muted">{t("Searching records…")}</p>}
      {!!answer?.errors.length && <div role="alert" className="grid justify-items-start gap-1 text-sm">
        <p className="text-danger">{t("Search failed.")} {answer.errors.join(" ")}</p>
        <Button size="sm" variant="ghost" onClick={() => run(q)}>{t("Retry")}</Button>
      </div>}
      {!pending && answer && answer.groups.length === 0 && <p className="text-sm text-muted">{t("No results")}</p>}
      {answer?.groups.map(group => (
        <section key={group.kind} aria-label={group.label} className="grid gap-1">
          <h2 className="text-2xs font-semibold uppercase tracking-wider text-muted">{group.label}</h2>
          <ul className="grid gap-0.5">
            {group.items.map(item => (
              <li key={item.id}>
                <Button variant="row" disabled={!!item.disabled} title={item.disabled} className="min-h-8 w-full gap-2"
                  onClick={event => search.open(item, event.metaKey || event.ctrlKey ? "beside" : undefined)}>
                  {item.icon}
                  <span className="min-w-0 flex-1 truncate text-left">{item.label}</span>
                  {item.detail && <span className="max-w-[45%] truncate text-xs text-muted">{item.detail}</span>}
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
