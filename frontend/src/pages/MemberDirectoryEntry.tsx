import { type Api, useRead } from "../host";
import { ListView } from "./ListPage";

export function MemberDirectoryEntry({ id }: { id: string }) {
  const directory = useRead<Api.MemberOption[]>("/v1/member-directory");
  return <ListView read={directory} definition={{
    title: "Members",
    about: "A member is a person or agent who can sign in to this workspace.",
    rows: (members) => members.filter((member) => member.id === id),
    id: (member) => member.id,
    columns: [{ key: "name", header: "Member", text: (member) => member.name }],
  }} />;
}
