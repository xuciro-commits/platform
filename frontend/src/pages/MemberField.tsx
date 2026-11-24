import { Avatar } from "@astryxdesign/core/Avatar";
import { Badge } from "@astryxdesign/core/Badge";
import { HStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Typeahead, TypeaheadItem } from "@astryxdesign/core/Typeahead";
import { type ComponentProps, useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { useAvatarDirectory, useAvatarUrl } from "./avatars";

type AvatarSize = ComponentProps<typeof Avatar>["size"];

export function PersonAvatar({
  name,
  id,
  avatar,
  size = "sm",
  tooltip = false,
}: {
  name: string;
  id?: string;
  avatar?: string;
  size?: AvatarSize;
  tooltip?: boolean;
}) {
  const directory = useAvatarDirectory();
  const resolved = avatar ?? directory.of(id, name);
  const src = useAvatarUrl(resolved, id || name);
  return <Avatar name={name} src={src} size={size} tooltip={tooltip} aria-hidden={!tooltip} />;
}

export function PersonLabel({
  name,
  id,
  avatar,
  agent,
  size = "sm",
  color,
  weight,
}: {
  name: string;
  id?: string;
  avatar?: string;
  agent?: boolean;
  size?: "xsm" | "sm";
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
}) {
  return (
    <HStack gap={size === "xsm" ? 1 : 2} vAlign="center">
      <PersonAvatar name={name} id={id} avatar={avatar} size={size} />
      <Text type={size === "xsm" ? "supporting" : "body"} color={color} weight={weight} maxLines={1}>{name}</Text>
      {agent && <Badge label={t("AI")} variant="neutral" />}
    </HStack>
  );
}

export function MemberLabel({
  name,
  id,
  avatar,
  size,
  color,
  weight,
}: {
  name: string;
  id?: string;
  avatar?: string;
  size?: "xsm" | "sm";
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
}) {
  return <PersonLabel name={name} id={id} avatar={avatar} size={size} color={color} weight={weight} />;
}

export function AvatarStack({ names, max = 3 }: { names: string[]; max?: number }) {
  const shown = names.slice(0, max);
  const extra = names.length - shown.length;
  return (
    <HStack gap={1} vAlign="center">
      {shown.map((name, index) => <PersonAvatar key={`${name}-${index}`} name={name} size="xsm" tooltip />)}
      {extra > 0 && <Text type="supporting" color="secondary">{`+${extra}`}</Text>}
    </HStack>
  );
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
  const source = useMemo(() => {
    const find = async (query: string) => {
      const lower = query.trim().toLocaleLowerCase();
      return (members ?? await request<Api.MemberOption[]>("/v1/member-directory")).filter((member) => member.name.toLocaleLowerCase().includes(lower)).slice(0, 10)
        .map((member) => ({ id: member.id, label: member.name }));
    };
    return { bootstrap: () => find(""), search: find };
  }, [members]);
  return <Typeahead
    {...props}
    value={selection}
    startIcon={selection && <PersonAvatar name={selection.label} id={selection.id} size="xsm" />}
    placeholder={t("Find a member")}
    emptySearchText={directory.status === "loading" ? t("Loading…") : directory.status === "ready" ? t("No matching members") : t("Members could not be searched")}
    searchSource={source}
    hasEntriesOnFocus
    renderItem={(item) => <TypeaheadItem item={item} icon={<PersonAvatar name={item.label} id={item.id} size="sm" />} />}
    onChange={(next) => {
      setPicked(next ?? undefined);
      onChange(next?.id ?? "");
    }}
  />;
}
