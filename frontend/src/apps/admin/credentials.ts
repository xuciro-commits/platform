import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.CredentialCatalog, Api.CredentialInfo>({
  title: "Credentials",
  about: "A credential is a sealed secret or a deployment reference that connections use; its value is never shown.",
  read: "/v1/credentials",
  rows: (answer) => answer.credentials,
  id: (credential) => credential.id,
  columns: [
    { key: "title", header: "Credential", text: (credential) => credential.title },
    { key: "state", header: "State", width: 120, text: (credential) => t(credential.state), value: (credential) => credential.state, dot: (credential) => (credential.state === "active" ? "success" : "neutral") },
    { key: "kind", header: "Kind", width: 120, kind: "code", text: (credential) => credential.kind },
    { key: "source", header: "Source", width: 120, kind: "code", text: (credential) => credential.source },
    { key: "reference", header: "Reference", kind: "code", text: (credential) => credential.reference ?? "" },
    { key: "updated", header: "Updated", width: 140, kind: "time", text: (credential) => credential.updatedAt },
    { key: "by", header: "Updated by", width: 160, kind: "member", hiddenByDefault: true, text: (credential) => credential.updatedBy },
  ],
  groups: [
    { key: "kind", label: "Kind", of: (credential) => credential.kind },
    { key: "state", label: "State", of: (credential) => t(credential.state) },
  ],
  actions: {
    create: "platform.credential.save",
    row: (credential) => (credential.state === "active" ? [{ schema: "platform.credential.disable", fixed: { expectedVersion: credential.version } }] : []),
  },
});
