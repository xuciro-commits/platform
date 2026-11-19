import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.Passage[], Api.Passage>({
  title: "Knowledge",
  about: "Search the documents agents can read: each result is a passage, the closest matches first.",
  read: "/v1/knowledge",
  query: "q",
  rows: (answer) => answer,
  id: (passage) => `${passage.document}#${passage.chunk}`,
  columns: [
    { key: "text", header: "Passage", text: (passage) => passage.text },
    { key: "title", header: "Document", width: 220, text: (passage) => passage.title },
    { key: "score", header: "Score", width: 100, kind: "number", text: (passage) => passage.score.toFixed(2), value: (passage) => passage.score },
  ],
});
