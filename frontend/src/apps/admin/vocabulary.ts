import type { Api } from "../../host";
import { t } from "../../i18n";
import { reasonOf, send } from "../../pages/act";
import { listPage } from "../../pages/ListEntry";
import type { Dot } from "../../pages/Transitions";

type Row = { id: string; word: Api.Word; lang: string; said: Api.Said };

const language = (code: string) => new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;
const kinds: Record<string, string> = { type: "Types and fields", choice: "Choices", action: "Actions", page: "Pages and apps", other: "Other" };
const origins: Record<string, string> = { platform: "Platform", app: "App or package", workspace: "This workspace", "": "No source" };
const statuses = ["Missing", "Conflict", "Unused", "Overridden", "Translated"];
const dots: Record<string, Dot> = { Missing: "warning", Conflict: "error", Unused: "neutral", Overridden: "accent", Translated: "success" };
const status = ({ word, said }: Row) => (word.stale ? "Unused" : said.origin === "workspace" ? "Overridden" : said.conflict ? "Conflict" : said.origin ? "Translated" : "Missing");
const shown = (code: string) => code !== "en";
const validLang = /^[a-zA-Z]{2,3}(?:-[a-zA-Z0-9]{2,8})*$/;

export default listPage<Api.WordsView, Row>({
  title: "Language and vocabulary",
  about: "Every text the apps show, with what each language says and where it comes from. A translation written here is this workspace's own and wins over the platform's and the package's.",
  read: "/v1/words",
  export: "language-and-vocabulary",
  import: (answer, action) => {
    const set = action("platform.translation.set");
    if (!set) return undefined;
    const targets = answer.languages.filter(shown);
    const byName = new Map(targets.map((code) => [language(code).toLowerCase(), code]));
    const fallback = targets.length === 1 ? targets[0] : "";
    const langOf = (raw: string | undefined) => {
      const value = (raw ?? "").trim();
      if (!value) return fallback;
      const mapped = byName.get(value.toLowerCase());
      if (mapped) return mapped;
      return validLang.test(value) ? value : "";
    };
    return {
      type: "platform.translation",
      title: "Language and vocabulary",
      fields: [
        { name: "text", title: "Text" },
        { name: "translation", title: "Translation" },
        { name: "lang", title: "Language" },
      ],
      run: async (rows, preview) => {
        const results: Api.ImportRow[] = [];
        for (let index = 0; index < rows.length; index += 1) {
          const text = (rows[index].text ?? "").trim();
          const lang = langOf(rows[index].lang);
          const translation = (rows[index].translation ?? "").trim();
          if (!text) {
            results.push({ row: index + 1, id: lang, action: set.schema, outcome: "ERROR_CODE_INVALID_ARGUMENT: text" });
            continue;
          }
          if (!lang) {
            results.push({ row: index + 1, id: "", action: set.schema, outcome: "ERROR_CODE_INVALID_ARGUMENT: lang" });
            continue;
          }
          if (preview) {
            results.push({ row: index + 1, id: lang, action: set.schema, outcome: "ok" });
            continue;
          }
          try {
            await send(set, lang, { text, translation });
            results.push({ row: index + 1, id: lang, action: set.schema, outcome: "ok" });
          } catch (problem) {
            results.push({ row: index + 1, id: lang, action: set.schema, outcome: "refused", error: { code: "refused", message: reasonOf(problem) } });
          }
        }
        return results;
      },
    };
  },
  rows: (answer) =>
    answer.words.flatMap((word) =>
      answer.languages.filter((lang) => shown(lang) && (!word.stale || word.says[lang])).map((lang) => ({ id: `${lang}|${word.app}|${word.area}|${word.text}`, word, lang, said: word.says[lang] ?? { text: word.text } })),
    ),
  id: (row) => row.id,
  columns: (answer) => [
    { key: "text", header: "Text", text: (row) => row.word.text },
    {
      key: "translation",
      header: "Translation",
      text: (row) => (row.said.origin ? row.said.text : ""),
      change: { schema: "platform.translation.set", parameter: "translation", id: (row) => row.lang, fixed: (row) => ({ text: row.word.text }), value: (row) => (row.said.origin ? row.said.text : "") },
    },
    { key: "status", header: "Status", width: 130, kind: "enum", choices: statuses, text: (row) => t(status(row)), value: status, dot: (row) => dots[status(row)] },
    { key: "origin", header: "Source", width: 150, options: Object.entries(origins).map(([value, label]) => ({ value, label: t(label) })), text: (row) => t(origins[row.said.origin ?? ""]), value: (row) => row.said.origin ?? "" },
    { key: "area", header: "App", width: 160, kind: "enum", choices: [...new Set(answer.words.map((word) => word.area))], text: (row) => t(row.word.area || "Unused"), value: (row) => row.word.area },
    { key: "kind", header: "Kind", width: 150, options: Object.entries(kinds).map(([value, label]) => ({ value, label: t(label) })), text: (row) => t(kinds[row.word.kind] ?? kinds.other), value: (row) => row.word.kind },
    { key: "lang", header: "Language", width: 130, hiddenByDefault: answer.languages.filter(shown).length < 2, chip: answer.languages.filter(shown).length > 1, options: answer.languages.filter(shown).map((code) => ({ value: code, label: language(code) })), text: (row) => language(row.lang), value: (row) => row.lang },
  ],
  groups: [
    { key: "area", label: "App", of: (row) => t(row.word.area || "Unused") },
    { key: "kind", label: "Kind", of: (row) => t(kinds[row.word.kind] ?? kinds.other), order: Object.values(kinds).map((label) => t(label)) },
    { key: "status", label: "Status", of: (row) => t(status(row)), order: statuses.map((label) => t(label)) },
  ],
  defaultView: { version: 1, group: "area" },
  actions: {
    row: (row) => [
      ...(row.said.origin === "workspace" ? [{ schema: "platform.translation.set", id: row.lang, fixed: { text: row.word.text, translation: "" }, label: row.word.stale ? "Delete translation" : "Revert to the shipped translation" }] : []),
    ],
  },
});
