import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";
import type { Dot } from "../../pages/Transitions";

const states = ["open", "proposed", "merged", "closed"];
const dots: Record<string, Dot> = { open: "accent", proposed: "warning", merged: "success", closed: "neutral" };

export default listPage<Api.BranchDetail[], Api.BranchDetail>({
  title: "Proposals",
  about: "A proposal is a branch of changes that reviewers approve before it merges into the main line.",
  read: "/v1/branches",
  rows: (answer) => answer,
  id: (branch) => branch.id,
  columns: [
    { key: "title", header: "Proposal", text: (branch) => branch.title || branch.name },
    { key: "state", header: "State", width: 130, kind: "enum", choices: states, text: (branch) => t(branch.state), value: (branch) => branch.state, dot: (branch) => dots[branch.state] },
    { key: "changes", header: "Changes", width: 100, kind: "number", text: (branch) => String(branch.changes), value: (branch) => branch.changes },
    { key: "by", header: "Proposed by", width: 160, kind: "member", text: (branch) => branch.proposal?.by ?? branch.created.by ?? "" },
    { key: "changed", header: "Updated", width: 140, kind: "time", text: (branch) => branch.changed.at ?? "" },
    { key: "resources", header: "Changed resources", hiddenByDefault: true, text: (branch) => branch.resources.map((resource) => `${resource.type} ${resource.id}`).join(", ") },
  ],
  groups: [{ key: "state", label: "State", of: (branch) => t(branch.state), order: states.map((state) => t(state)) }],
});
