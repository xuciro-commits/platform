import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.SupportGrant[], Api.SupportGrant>({
  title: "Support grants",
  about: "A support grant lets a host administrator act as a member of this workspace for a stated reason until it expires; every use is recorded.",
  read: "/v1/support-grants",
  rows: (answer) => answer,
  id: (grant) => grant.id,
  columns: [
    { key: "reason", header: "Reason", text: (grant) => grant.reason },
    { key: "member", header: "Member", width: 160, kind: "member", text: (grant) => grant.member },
    { key: "opened", header: "Opened by", width: 160, kind: "code", text: (grant) => grant.opened },
    { key: "expires", header: "Expires", width: 140, kind: "time", text: (grant) => grant.expires },
    { key: "uses", header: "Uses", width: 90, kind: "number", text: (grant) => String(grant.uses), value: (grant) => grant.uses },
  ],
});
