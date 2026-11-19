import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

const language = (code: string) => new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;

export default listPage<Api.WordsView, Api.Word>({
  title: "Language and vocabulary",
  about: "The words an app shows, with their translation in each language this workspace offers; the workspace's own wording wins.",
  read: "/v1/words",
  rows: (answer) => answer.words,
  id: (word) => word.text,
  columns: (answer) => [
    { key: "text", header: "Text", text: (word) => word.text },
    ...answer.languages
      .filter((code) => code !== "en")
      .map((code) => ({ key: code, header: language(code), text: (word: Api.Word) => word.own[code] || word.translations[code] || "" })),
  ],
});
