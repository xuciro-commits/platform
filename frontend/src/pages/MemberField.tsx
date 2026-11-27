import { Typeahead, TypeaheadItem } from "@astryxdesign/core/Typeahead";
import { type ComponentProps, useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { MemberLabel, PersonAvatar, PersonLabel, PersonStack, PersonValue } from "../semantic";

export { MemberLabel, PersonAvatar, PersonLabel, PersonStack as AvatarStack, PersonValue };

type MemberInputProps = Pick<ComponentProps<typeof Typeahead>,
  "label" | "description" | "isRequired" | "isLabelHidden" | "status" | "hasAutoFocus" | "controlXstyle"> & {
  id: string;
  name?: string;
  onChange: (value: string) => void;
};

export function MemberInput({ id, name, onChange, ...props }: MemberInputProps) {
  const directory = useRead<Api.MemberOption[]>("/v1/member-directory");
  const members = directory.status === "ready" ? directory.data : undefined;
  const [picked, setPicked] = useState<{ id: string; label: string }>();
  const matchedName = members?.find((entry) => entry.id === id)?.name;
  const selection = id ? { id, label: name ?? matchedName ?? (picked?.id === id ? picked.label : t("Unnamed member")) } : null;
  const source = useMemo(() => {
    const find = async (query: string) => {
      const lower = query.trim().toLocaleLowerCase();
      return (members ?? await request<Api.MemberOption[]>("/v1/member-directory"))
        .filter((member) => member.name.toLocaleLowerCase().includes(lower))
        .slice(0, 10)
        .map((member) => ({ id: member.id, label: member.name }));
    };
    return { bootstrap: () => find(""), search: find };
  }, [members]);
  return (
    <Typeahead
      {...props}
      value={selection}
      startIcon={selection && <PersonAvatar name={selection.label} id={selection.id} size="xs" />}
      placeholder={t("Find a member")}
      emptySearchText={directory.status === "loading" ? t("Loading…") : directory.status === "ready" ? t("No matching members") : t("Members could not be searched")}
      searchSource={source}
      hasEntriesOnFocus
      renderItem={(item) => <TypeaheadItem item={item} icon={<PersonAvatar name={item.label} id={item.id} size="sm" />} />}
      onChange={(next) => {
        setPicked(next ?? undefined);
        onChange(next?.id ?? "");
      }}
    />
  );
}
