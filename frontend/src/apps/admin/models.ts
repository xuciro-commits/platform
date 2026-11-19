import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.Model[], Api.Model>({
  title: "Models",
  about: "A model is a provider's language model this workspace lets members call, with who may call it and a daily token cap.",
  read: "/v1/ai-models",
  rows: (answer) => answer,
  id: (model) => `${model.provider}/${model.model}`,
  columns: [
    { key: "model", header: "Model", kind: "code", text: (model) => model.model },
    { key: "provider", header: "Provider", width: 140, kind: "code", text: (model) => model.provider },
    { key: "access", header: "Access", width: 130, text: (model) => t(model.access), value: (model) => model.access },
    { key: "daily", header: "Daily tokens", width: 140, kind: "number", text: (model) => (model.dailyTokens ? model.dailyTokens.toLocaleString() : t("No cap")), value: (model) => model.dailyTokens ?? 0 },
  ],
  groups: [{ key: "provider", label: "Provider", of: (model) => model.provider }],
  actions: {
    row: (model) => [
      { schema: "ai.model.enable", fixed: model.revision == null ? undefined : { expectedRevision: model.revision } },
      { schema: "ai.model.disable" },
    ],
  },
});
