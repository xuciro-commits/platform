// Control Panel: the tenant's words (ADR-0083 D1). Everything the tenant
// defined in Build — object types, fields, states, actions, applications,
// pages — is written once in one language; here an administrator says what it
// reads as in each other language, and the host serves it translated like any
// shipped text. Shipped translations may be overridden the same way.
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, Form, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import { useEffect, useState } from "react";
import { useAdmin } from "./shared";

const languageNames: Record<string, string> = { en: "English", "zh-CN": "简体中文", "zh-TW": "繁體中文", ja: "日本語", ko: "한국어", de: "Deutsch", fr: "Français", es: "Español", pt: "Português", th: "ไทย", vi: "Tiếng Việt", id: "Bahasa Indonesia", ms: "Bahasa Melayu", ar: "العربية" };
const languageName = (tag: string) => languageNames[tag] ? `${languageNames[tag]} (${tag})` : tag;

export function Words() {
  const { decide } = useHost();
  const { apps } = useAdmin();
  const [app, setApp] = useState("build");
  const [lang, setLang] = useState("zh-CN");
  const [adding, setAdding] = useState("");
  const [extra, setExtra] = useState<string[]>([]);
  const [q, setQ] = useState("");
  const [onlyMissing, setOnlyMissing] = useState(false);
  const words = useRead<Api.WordsView>(`/v1/words?app=${encodeURIComponent(app)}`);
  const languages = Array.from(new Set([...(words.data?.languages ?? []), ...extra])).filter((l) => l !== "en");
  useEffect(() => { if (languages.length && !languages.includes(lang)) setLang(languages[0]!); }, [languages.join(" ")]); // eslint-disable-line react-hooks/exhaustive-deps
  const needle = q.trim().toLocaleLowerCase();
  const list = (words.data?.words ?? []).filter((w) => (!onlyMissing || !w.translations[lang]) && (!needle || w.text.toLocaleLowerCase().includes(needle) || (w.translations[lang] ?? "").toLocaleLowerCase().includes(needle)));
  const missing = (words.data?.words ?? []).filter((w) => !w.translations[lang]).length;
  const save = async (text: string, translation: string) => {
    if (await decide("platform.translation.set", { type: "platform.translation", id: lang }, { text, translation })) await words.refetch();
  };
  const appTitle = (id: string) => apps.find((a) => a.id === id)?.title || id;
  return <>
    <PageHeader title={t("Languages and words")} description={t("What the names this organisation defined read as in each language: object types, fields, states, actions, applications and pages written in Build, and any shipped text you want said differently. Write a name once in one language and translate it here; never put two languages in one name.")} />
    <Panel className="mb-3">
      <div className="flex flex-wrap items-end gap-3 text-xs">
        <label className="grid gap-1">{t("Texts of")}<Select value={app} onChange={(e) => setApp(e.target.value)} className="w-56">
          <option value="build">{t("Tenant definitions (Build)")}</option>
          {(words.data?.apps ?? []).filter((id) => id !== "build").map((id) => <option key={id} value={id}>{appTitle(id)} · {id}</option>)}
        </Select></label>
        <label className="grid gap-1">{t("Language")}<Select value={lang} onChange={(e) => setLang(e.target.value)} className="w-48">
          {languages.map((l) => <option key={l} value={l}>{languageName(l)}</option>)}
        </Select></label>
        <Form className="flex items-end gap-1" onSubmit={() => { const tag = adding.trim(); if (!tag || /[\s/]/.test(tag)) return; setExtra((x) => x.includes(tag) ? x : [...x, tag]); setLang(tag); setAdding(""); }}>
          <label className="grid gap-1">{t("Add a language")}<Input value={adding} onChange={(e) => setAdding(e.target.value)} placeholder="ja, de, zh-TW…" className="w-32" /></label>
          <Button type="submit" size="sm" disabled={!adding.trim()}>{t("Add")}</Button>
        </Form>
        <label className="grid gap-1">{t("Find")}<Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("Text or translation")} className="w-56" /></label>
        <Checkbox checked={onlyMissing} onChange={setOnlyMissing}>{t("Only untranslated")}</Checkbox>
        <span className="ml-auto text-muted">{t("{n} texts · {missing} without a translation in {lang}", { n: words.data?.words.length ?? 0, missing, lang: languageName(lang) })}</span>
      </div>
    </Panel>
    {words.isError && <p role="alert" className="text-sm text-[var(--tone-danger)]">{t("Words could not be loaded.")}</p>}
    <Panel>
      <div className="grid grid-cols-[minmax(12rem,1fr)_minmax(16rem,1.4fr)_6rem] items-center gap-x-3 border-b border-border pb-1 text-xs font-semibold text-muted">
        <span>{t("Text as declared")}</span><span>{languageName(lang)}</span><span>{t("Source")}</span>
      </div>
      {list.map((w) => <WordRow key={w.text} word={w} lang={lang} onSave={save} />)}
      {!list.length && !words.isLoading && <p className="py-3 text-sm text-muted">{onlyMissing ? t("Every text has a translation in this language.") : t("No text matches.")}</p>}
    </Panel>
  </>;
}

function WordRow({ word: w, lang, onSave }: { word: Api.Word; lang: string; onSave: (text: string, translation: string) => Promise<void> }) {
  const own = w.own[lang], shipped = w.translations[lang];
  const [value, setValue] = useState(own ?? "");
  useEffect(() => setValue(own ?? ""), [own, lang]);
  const commit = async () => { if ((value.trim() || "") !== (own ?? "")) await onSave(w.text, value.trim()); };
  return <div className="grid grid-cols-[minmax(12rem,1fr)_minmax(16rem,1.4fr)_6rem] items-center gap-x-3 border-b border-border py-1 text-xs last:border-0">
    <span className="min-w-0 break-words">{w.text}</span>
    <Input aria-label={t("Translation of {text}", { text: w.text })} value={value} placeholder={own === undefined && shipped ? shipped : t("Not translated")} onChange={(e) => setValue(e.target.value)}
      onBlur={() => void commit()} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void commit(); } }} className="h-7 text-xs" />
    <span>{own !== undefined ? <Tag label={t("Own")} tone="success" /> : shipped ? <Tag label={t("Shipped")} /> : <Tag label={t("Missing")} tone="warning" />}</span>
  </div>;
}
