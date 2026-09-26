// Settings: documents and the glossary (ADR-0022, ADR-0023).
import { GeneratedForm, Records, newId, useHost, type Passage } from "@platform/app";
import { Button, Card, Dialog, Input, t } from "@platform/ui";
import { BookA, BookOpen } from "lucide-react";
import { useState } from "react";

// The tenant's glossary (ADR-0023 D1): its own words, layered on the model.
export function Glossary() {
  const { can, decide } = useHost();
  const [writing, setWriting] = useState(false);
  return (
    <>
      <Records type="knowledge.term" description={t("This organisation's own words: what each means here and what it refers to. Agents read them and Search understands them; they never change what an entity, field or action is.")}
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
      <form className="flex max-w-2xl gap-2" onSubmit={async (e) => { e.preventDefault(); setFound(await client.get<Passage[]>(`/v1/knowledge?q=${encodeURIComponent(q)}`)); }}>
        <Input aria-label={t("Question")} placeholder={t("What an agent might ask")} value={q} onChange={(e) => setQ(e.target.value)} />
        <Button type="submit">{t("Search")}</Button>
      </form>
      {found && <div className="mt-2 grid max-w-2xl gap-2">
        {found.length === 0 && <p className="text-sm text-muted">{t("Nothing you may read answers it.")}</p>}
        {found.map((p) => <Card key={`${p.document}#${p.chunk}`} className="p-3">
          <div className="text-sm font-medium">{p.title}</div>
          <div className="font-mono text-xs text-muted">{p.document} {t("· passage")} {p.chunk + 1} {t("· score")} {p.score}</div>
          <p className="mt-1 whitespace-pre-wrap text-sm">{p.text}</p></Card>)}
      </div>}
      <Dialog open={writing} onOpenChange={setWriting} title={t("New document")}>
        <GeneratedForm type="knowledge.document" submitLabel={t("Save")} onCancel={() => setWriting(false)}
          onSubmit={async (v) => { if (await decide("knowledge.document.create", { type: "knowledge.document", id: newId("DOC") }, v, { expectedRevision: 0 })) setWriting(false); }} />
      </Dialog>
    </>
  );
}
