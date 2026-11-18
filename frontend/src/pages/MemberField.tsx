import { Avatar } from "@astryxdesign/core/Avatar";
import { HStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Typeahead, TypeaheadItem } from "@astryxdesign/core/Typeahead";
import { type ComponentProps, useMemo, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";

export function MemberLabel({ name }: { name: string }) {
  return <HStack gap={2} vAlign="center">
    <Avatar name={name} size="sm" tooltip={false} />
    <Text type="body" maxLines={1}>{name}</Text>
  </HStack>;
}

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
  const selection = id ? { id, label: name ?? (picked?.id === id ? picked.label : t("Unnamed member")) } : null;
  const source = useMemo(() => ({
    bootstrap: () => [],
    search: (query: string) => {
      const lower = query.trim().toLocaleLowerCase();
      return (members ?? []).filter((member) => member.name.toLocaleLowerCase().includes(lower)).slice(0, 10)
        .map((member) => ({ id: member.id, label: member.name }));
    },
  }), [members]);
  return <Typeahead
    {...props}
    value={selection}
    startIcon={selection && <Avatar name={selection.label} size="xsm" tooltip={false} />}
    placeholder={t("Find a member")}
    emptySearchText={directory.status === "loading" ? t("Loading…") : directory.status === "ready" ? t("No matching members") : t("Members could not be searched")}
    searchSource={source}
    renderItem={(item) => <TypeaheadItem item={item} icon={<Avatar name={item.label} size="sm" tooltip={false} />} />}
    onChange={(next) => {
      setPicked(next ?? undefined);
      onChange(next?.id ?? "");
    }}
  />;
}
