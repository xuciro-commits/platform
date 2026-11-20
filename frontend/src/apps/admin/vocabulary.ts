import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";
import type { Dot } from "../../pages/Transitions";

type Row = { id: string; word: Api.Word; lang: string; said: Api.Said };

const language = (code: string) => new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;
const kinds: Record<string, string> = { type: "Types and fields", choice: "Choices", action: "Actions", page: "Pages and apps", other: "Other" };
const origins: Record<string, string> = { platform: "Platform", app: "App or package", workspace: "This workspace", "": "No source" };
const statuses = ["Missing", "Conflict", "Overridden", "Translated"];
const dots: Record<string, Dot> = { Missing: "warning", Conflict: "error", Overridden: "accent", Translated: "success" };
const status = ({ said }: Row) => (said.origin === "workspace" ? "Overridden" : said.conflict ? "Conflict" : said.origin ? "Translated" : "Missing");
const shown = (code: string) => code !== "en";

export default listPage<Api.WordsView, Row>({
  title: "Language and vocabulary",
  about: "Every text the apps show, with what each language says and where it comes from. A translation written here is this workspace's own and wins over the platform's and the package's.",
  read: "/v1/words",
  rows: (answer) =>
    answer.words.flatMap((word) =>
      answer.languages.filter(shown).map((lang) => ({ id: `${lang}|${word.app}|${word.area}|${word.text}`, word, lang, said: word.says[lang] ?? { text: word.text } })),
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
    { key: "area", header: "App", width: 160, kind: "enum", choices: [...new Set(answer.words.map((word) => word.area))], text: (row) => t(row.word.area), value: (row) => row.word.area },
    { key: "kind", header: "Kind", width: 150, options: Object.entries(kinds).map(([value, label]) => ({ value, label: t(label) })), text: (row) => t(kinds[row.word.kind] ?? kinds.other), value: (row) => row.word.kind },
    { key: "lang", header: "Language", width: 130, hiddenByDefault: answer.languages.filter(shown).length < 2, chip: answer.languages.filter(shown).length > 1, options: answer.languages.filter(shown).map((code) => ({ value: code, label: language(code) })), text: (row) => language(row.lang), value: (row) => row.lang },
  ],
  groups: [
    { key: "area", label: "App", of: (row) => t(row.word.area) },
    { key: "kind", label: "Kind", of: (row) => t(kinds[row.word.kind] ?? kinds.other), order: Object.values(kinds).map((label) => t(label)) },
    { key: "status", label: "Status", of: (row) => t(status(row)), order: statuses.map((label) => t(label)) },
  ],
  defaultView: { version: 1, group: "area" },
  actions: {
    row: (row) => [
      ...(row.said.origin === "workspace" ? [{ schema: "platform.translation.set", id: row.lang, fixed: { text: row.word.text, translation: "" }, label: "Revert to the shipped translation" }] : []),
    ],
  },
});
