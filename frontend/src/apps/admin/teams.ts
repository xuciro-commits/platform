import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.AccessConfig, Api.Team>({
  title: "Teams",
  about: "A team is a set of members who hold the team's grants together, for as long as they belong to it.",
  read: "/v1/access",
  rows: (answer) => answer.teams,
  id: (team) => team.id,
  columns: [
    { key: "name", header: "Team", text: (team) => team.name },
    { key: "members", header: "Members", text: (team) => team.members.join(", ") },
    { key: "grants", header: "Grants", text: (team) => team.grants.map((grant) => `${grant.app}: ${grant.role}`).join(", ") },
  ],
  actions: { create: "platform.team.save", row: () => [{ schema: "platform.team.remove" }] },
});
