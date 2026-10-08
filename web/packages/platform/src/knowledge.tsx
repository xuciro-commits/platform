// Settings: documents and the glossary (ADR-0022, ADR-0023).
import { GeneratedForm, Records, newId, useDefinitions, useHost, useReadQuery, type Passage } from "@platform/app";
import { Button, Card, Dialog, Form, Input, Markdown, Panel, Tag, t } from "@platform/ui";
import { BookA, BookOpen } from "lucide-react";
import { useState } from "react";

// Where a term is actually felt (ADR-0084 D7): the host validates RefersTo when
// the term is saved (knowledge.go, host.Declares), Search proposes entity names
// whose title, plural, synonyms or glossary terms contain the word
// (languages.go, names()), and an agent's prompt carries the glossary of the app
// it runs in (glossary(app)). This panel shows that reach term by term instead
// of leaving the reader to take it on faith.
function GlossaryReach() {
  const terms = useReadQuery<{ records?: { id: string; term: string; synonyms?: string; refersTo?: string; apps?: string[] }[] }>("/v1/records/knowledge.term?limit=200").data?.records ?? [];
  const { data } = useDefinitions();
  const definitions = data ?? [];
  if (!terms.length) return null;
  const names = definitions.flatMap((d) => {
    const entity = d.entity, action = d.action, page = d.page, application = d.application;
    const words = entity ? [entity.title, entity.plural, entity.synonyms] : action ? [action.title] : page ? [page.title] : application ? [application.title] : [];
    return [{ ref: `${d.ref.app}/${d.ref.kind}/${d.ref.name}`, title: entity?.title ?? action?.title ?? page?.title ?? application?.title ?? d.ref.name, text: words.filter(Boolean).join(" ").toLowerCase() }];
  });
  const reached = terms.map((t) => {
    const words = [t.term, ...(t.synonyms ?? "").split(",")].map((w) => w.trim().toLowerCase()).filter(Boolean);
    const matched = names.filter((n) => words.some((w) => n.text.includes(w)));
    return { term: t, matched, names: new Set(matched.map((m) => m.ref)).size };
  });
  const byRef = reached.filter((r) => r.term.refersTo).length;
  const byName = reached.filter((r) => r.names > 0).length;
  return <Panel className="grid gap-1" title={t("Where these words work")}
    description={t("{byRef} of {total} name a declaration the host checked when it was saved; {byName} are words a declared name or synonym also uses, so Search proposes them. A term with neither is documentation: it teaches people, and no query reads it.", { byRef, byName, total: reached.length })}>
    {reached.map(({ term, matched, names: n }) => <p key={term.id} className="flex flex-wrap items-baseline gap-2 text-xs">
      <span className="font-medium">{term.term}</span>
      {term.refersTo && <Tag label={t("names {ref}", { ref: term.refersTo })} tone="info" />}
      {n > 0 && <Tag label={t("{n} declared names match", { n })} tone="success" />}
      {!term.refersTo && n === 0 && <Tag label={t("documentation only")} />}
      {!!term.apps?.length && <span className="text-muted">{t("read by members of {apps}", { apps: term.apps.join(", ") })}</span>}
      {n > 0 && <span className="text-muted">{matched.slice(0, 4).map((m) => m.title).join(" · ")}</span>}
    </p>)}
  </Panel>;
}

// The tenant's glossary (ADR-0023 D1): its own words, layered on the model.
export function Glossary() {
  const { can, decide } = useHost();
  const [writing, setWriting] = useState(false);
  return (
    <>
      <GlossaryReach />
      <Records type="knowledge.term" description={t("Jargon of this organisation and what it refers to (an object type, a field or an action). Three things read it: Search, which finds records by the term; agents, whose prompts carry it; and anyone asking what a word means here. It does not rename anything — to change how a name reads in a language, use Control Panel → Languages and words.")}
        actions={can("knowledge.term.create") && <Button variant="primary" onClick={() => setWriting(true)}><BookA />{t("New term")}</Button>} />
      <Dialog open={writing} onOpenChange={setWriting} title={t("New term")}>
        <GeneratedForm type="knowledge.term" submitLabel={t("Save")} onCancel={() => setWriting(false)}
          onSubmit={async (v) => { if (await decide("knowledge.term.create", { type: "knowledge.term", id: newId("TERM") }, v, { expectedRevision: 0 })) setWriting(false); }} />
      </Dialog>
    </>
  );
}

// Knowledge (ADR-0022): documents agents and members find by words and, with
// an embedding model, by meaning; each is read by members of the apps it names.
export function Knowledge() {
  const { can, decide, client } = useHost();
  const [writing, setWriting] = useState(false);
  const [q, setQ] = useState("");
  const [found, setFound] = useState<Passage[]>();
  return (
    <>
      <Records type="knowledge.document" description={t("House rules, manuals, FAQs. Agents search them with their knowledge tool and cite what they used; members find them in Search. Set the embedding model in App settings → Knowledge to search by meaning as well as by words.")}
        actions={can("knowledge.document.create") && <Button variant="primary" onClick={() => setWriting(true)}><BookOpen />{t("New document")}</Button>} />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Try a search")}</h2>
      <Form className="flex max-w-2xl gap-2" onSubmit={async () => { setFound(await client.get<Passage[]>(`/v1/knowledge?q=${encodeURIComponent(q)}`)); }}>
        <Input aria-label={t("Question")} placeholder={t("What an agent might ask")} value={q} onChange={(e) => setQ(e.target.value)} />
        <Button type="submit">{t("Search")}</Button>
      </Form>
      {found && <div className="mt-2 grid max-w-2xl gap-2">
        {found.length === 0 && <p className="text-sm text-muted">{t("Nothing you may read answers it.")}</p>}
        {found.map((p) => <Card key={`${p.document}#${p.chunk}`} className="p-3">
          <div className="text-sm font-medium">{p.title}</div>
          <div className="font-mono text-xs text-muted">{p.document} {t("· passage")} {p.chunk + 1} {t("· score")} {p.score}</div>
          <Markdown content={p.text} className="mt-1" /></Card>)}
      </div>}
      <Dialog open={writing} onOpenChange={setWriting} title={t("New document")} wide>
        <GeneratedForm type="knowledge.document" submitLabel={t("Save")} onCancel={() => setWriting(false)}
          onSubmit={async (v) => { if (await decide("knowledge.document.create", { type: "knowledge.document", id: newId("DOC") }, v, { expectedRevision: 0 })) setWriting(false); }} />
      </Dialog>
    </>
  );
}
