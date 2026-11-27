import { Avatar } from "@astryxdesign/core/Avatar";
import { AvatarGroup, AvatarGroupOverflow } from "@astryxdesign/core/AvatarGroup";
import { HoverCard } from "@astryxdesign/core/HoverCard";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { type ComponentProps, useState } from "react";
import { type Api, readSnapshot, useRead } from "../host";
import { t } from "../i18n";
import { defaultAvatarFor, useAvatarDirectory, useAvatarUrl } from "../pages/avatars";
import { avatarSizeOf, type SemanticSize, textTypeOf } from "./types";

type AvatarPropSize = ComponentProps<typeof Avatar>["size"];

export type PersonItem = {
  id?: string;
  name?: string;
  avatar?: string;
  agent?: boolean;
};

type DirectoryPerson = {
  id: string;
  name: string;
  avatar?: string;
  unit?: string;
  agent?: boolean;
};

type Directory = { resolve: (id?: string, name?: string) => DirectoryPerson | undefined };
type EnterpriseDirectory = Pick<Api.EnterpriseModel, "elements" | "relationships">;
const emptyMembers: Api.MemberOption[] = [];
const emptyEnterprise: EnterpriseDirectory = { elements: [], relationships: [] };
const directories = new WeakMap<Api.MemberOption[], WeakMap<EnterpriseDirectory, Directory>>();

function buildDirectory(members: Api.MemberOption[], enterprise: EnterpriseDirectory): Directory {
  const byId = new Map<string, DirectoryPerson>();
  const byName = new Map<string, DirectoryPerson>();
  if (enterprise) {
    const elements = enterprise.elements as (Api.EnterpriseModel["elements"][number] & { avatar?: string })[];
    const unitNames = new Map<string, string>();
    for (const element of elements) {
      if (element.stereotype === "ActualOrganization" && !element.deleted) {
        unitNames.set(element.id, element.name);
      }
    }
    const personUnit = new Map<string, string>();
    for (const rel of enterprise.relationships) {
      if (rel.source && rel.target && unitNames.has(rel.target) && !personUnit.has(rel.source)) {
        personUnit.set(rel.source, unitNames.get(rel.target)!);
      }
    }
    for (const element of elements) {
      if (element.stereotype !== "ActualPerson" || element.deleted) continue;
      const entry: DirectoryPerson = {
        id: element.id,
        name: element.name || element.id,
        avatar: element.avatar || defaultAvatarFor(element.id),
        unit: personUnit.get(element.id),
      };
      byId.set(entry.id, entry);
      if (entry.name && !byName.has(entry.name)) byName.set(entry.name, entry);
    }
  }
  if (members) {
    for (const member of members) {
      const existing = byId.get(member.id);
      const entry: DirectoryPerson = {
        id: member.id,
        name: member.name || existing?.name || member.id,
        avatar: member.avatar || existing?.avatar || defaultAvatarFor(member.id),
        unit: existing?.unit,
        agent: member.id.startsWith("agent-") || member.id.startsWith("bot-"),
      };
      byId.set(entry.id, entry);
      if (entry.name) byName.set(entry.name, entry);
    }
  }
  return {
    resolve: (id?: string, name?: string): DirectoryPerson | undefined =>
      (id ? byId.get(id) : undefined) ?? (name ? byName.get(name) : undefined),
  };
}

function directoryOf(members = emptyMembers, enterprise: EnterpriseDirectory = emptyEnterprise): Directory {
  let byEnterprise = directories.get(members);
  if (!byEnterprise) {
    byEnterprise = new WeakMap();
    directories.set(members, byEnterprise);
  }
  let directory = byEnterprise.get(enterprise);
  if (!directory) {
    directory = buildDirectory(members, enterprise);
    byEnterprise.set(enterprise, directory);
  }
  return directory;
}

export function personName(id: string): string | undefined {
  return directoryOf(
    readSnapshot<Api.MemberOption[]>("/v1/member-directory"),
    readSnapshot<Api.EnterpriseModel>("/v1/enterprise"),
  ).resolve(id)?.name;
}

export function usePersonDirectory() {
  const members = useRead<Api.MemberOption[]>("/v1/member-directory");
  const enterprise = useRead<Api.EnterpriseModel>("/v1/enterprise");
  return directoryOf(members.status === "ready" ? members.data : undefined, enterprise.status === "ready" ? enterprise.data : undefined);
}

function toAvatarSize(size?: SemanticSize | AvatarPropSize): AvatarPropSize {
  if (!size) return "sm";
  if (size === "xs") return "xsm";
  if (size === "sm" || size === "md" || size === "lg") return avatarSizeOf(size);
  return size;
}

export function PersonAvatar({
  name,
  id,
  avatar,
  agent,
  size = "sm",
  tooltip = false,
}: {
  name: string;
  id?: string;
  avatar?: string;
  agent?: boolean;
  size?: SemanticSize | AvatarPropSize;
  tooltip?: boolean | string;
}) {
  const directory = useAvatarDirectory();
  const resolved = avatar ?? directory.of(id, name);
  const src = useAvatarUrl(resolved, id || name);
  return (
    <Avatar
      name={name}
      src={src}
      size={toAvatarSize(size)}
      shape={agent ? "rounded" : "circle"}
      tooltip={tooltip}
      aria-hidden={!tooltip}
    />
  );
}

export function EntityAvatar({
  name,
  src,
  size = "sm",
  shape = "rounded",
  tooltip = false,
}: {
  name: string;
  src?: string;
  size?: SemanticSize | AvatarPropSize;
  shape?: "rounded" | "square";
  tooltip?: boolean | string;
}) {
  return <Avatar name={name} src={src} size={toAvatarSize(size)} shape={shape} tooltip={tooltip} aria-hidden={!tooltip} />;
}

function PersonCardContent({
  id,
  name,
  avatar,
  unit,
  agent,
}: {
  id?: string;
  name: string;
  avatar?: string;
  unit?: string;
  agent?: boolean;
}) {
  const members = useRead<Api.MemberView[]>(id ? "/v1/members" : null);
  const matched = members.status === "ready" ? members.data.find((entry) => entry.id === id) : undefined;
  const email = matched?.profile.email;
  const jobTitle = matched?.profile.title;
  const isAgent = Boolean(agent || matched?.agent);
  const roleSummary = matched
    ? Object.entries(matched.roles)
        .filter(([, role]) => Boolean(role))
        .slice(0, 3)
        .map(([app, role]) => `${app}: ${role}`)
        .join(", ")
    : "";
  return (
    <VStack gap={2}>
      <HStack gap={2} vAlign="center">
        <PersonAvatar name={name} id={id} avatar={avatar} agent={isAgent} size="lg" tooltip={false} />
        <VStack gap={0.5}>
          <HStack gap={1.5} vAlign="center">
            <Heading level={4}>{name}</Heading>
            {isAgent && <Token size="sm" color="purple" label={t("AI")} />}
          </HStack>
          {jobTitle && <Text type="supporting" color="secondary">{jobTitle}</Text>}
          {!jobTitle && id && id !== name && <Text type="code" color="secondary">{id}</Text>}
        </VStack>
      </HStack>
      {(email || unit || roleSummary) && (
        <MetadataList>
          {unit && <MetadataListItem label={t("Organization")}>{unit}</MetadataListItem>}
          {email && <MetadataListItem label={t("Email")}>{email}</MetadataListItem>}
          {roleSummary && <MetadataListItem label={t("Roles")}>{roleSummary}</MetadataListItem>}
        </MetadataList>
      )}
    </VStack>
  );
}

export function PersonValue({
  id,
  name,
  avatar,
  agent,
  variant = "avatar-name",
  size = "sm",
  color,
  weight,
  card = true,
}: {
  id?: string;
  name?: string;
  avatar?: string;
  agent?: boolean;
  variant?: "avatar-name" | "avatar-only";
  size?: SemanticSize | "xsm";
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
  card?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const directory = usePersonDirectory();
  const normalizedSize: SemanticSize = size === "xsm" ? "xs" : size;
  const resolved = directory.resolve(id, name);
  const fallbackUnknown = t("Unnamed member");
  const effectiveName =
    (name && name !== fallbackUnknown && name !== id ? name : undefined) ??
    resolved?.name ??
    (name && name !== fallbackUnknown ? name : undefined) ??
    id ??
    fallbackUnknown;
  const effectiveId = id ?? resolved?.id;
  const effectiveAvatar = avatar ?? resolved?.avatar;
  const isAgent = Boolean(agent ?? resolved?.agent);

  const trigger = variant === "avatar-only" ? (
    <PersonAvatar
      name={effectiveName}
      id={effectiveId}
      avatar={effectiveAvatar}
      agent={isAgent}
      size={normalizedSize}
      tooltip={card ? false : effectiveName}
    />
  ) : (
    <HStack gap={normalizedSize === "xs" ? 1 : 2} vAlign="center">
      <PersonAvatar
        name={effectiveName}
        id={effectiveId}
        avatar={effectiveAvatar}
        agent={isAgent}
        size={normalizedSize}
        tooltip={false}
      />
      <Text
        type={textTypeOf(normalizedSize)}
        color={color}
        weight={weight ?? (normalizedSize === "lg" ? "semibold" : undefined)}
        maxLines={1}
      >
        {effectiveName}
      </Text>
      {isAgent && <Token size="sm" color="purple" label={t("AI")} />}
    </HStack>
  );

  if (!card) return trigger;

  return (
    <HoverCard
      onOpenChange={setOpen}
      placement="below"
      content={
        open ? (
          <PersonCardContent
            id={effectiveId}
            name={effectiveName}
            avatar={effectiveAvatar}
            unit={resolved?.unit}
            agent={isAgent}
          />
        ) : null
      }
    >
      {trigger}
    </HoverCard>
  );
}

export function PersonLabel(props: {
  name: string;
  id?: string;
  avatar?: string;
  agent?: boolean;
  size?: "xsm" | SemanticSize;
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
  card?: boolean;
}) {
  return <PersonValue {...props} variant="avatar-name" />;
}

export function MemberLabel(props: {
  name: string;
  id?: string;
  avatar?: string;
  size?: "xsm" | SemanticSize;
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
  card?: boolean;
}) {
  return <PersonValue {...props} variant="avatar-name" />;
}

export function PersonStack({
  items,
  names,
  max = 3,
  size = "xs",
}: {
  items?: PersonItem[];
  names?: string[];
  max?: number;
  size?: SemanticSize | "xsm";
}) {
  const directory = usePersonDirectory();
  const avatarDir = useAvatarDirectory();
  const normalizedSize: SemanticSize = size === "xsm" ? "xs" : size;
  const list: PersonItem[] = items ?? (names ?? []).map((entry) => ({ name: entry }));
  const resolved = list
    .map((entry) => {
      const found = directory.resolve(entry.id, entry.name);
      const label = entry.name || found?.name || entry.id || "";
      if (!label) return undefined;
      return {
        id: entry.id ?? found?.id,
        name: label,
        avatar: entry.avatar ?? found?.avatar ?? avatarDir.of(entry.id ?? found?.id, label),
      };
    })
    .filter((entry): entry is NonNullable<typeof entry> => Boolean(entry));

  if (resolved.length === 0) return null;
  const visibleLimit = resolved.length <= max + 1 ? resolved.length : max;
  const shown = resolved.slice(0, visibleLimit);
  const hidden = resolved.slice(visibleLimit);

  const group = (
    <AvatarGroup size={avatarSizeOf(normalizedSize)}>
      {shown.map((person, index) => (
        <GroupFace key={`${person.id ?? person.name}-${index}`} name={person.name} id={person.id} avatar={person.avatar} />
      ))}
      {hidden.length > 0 && <AvatarGroupOverflow count={hidden.length} />}
    </AvatarGroup>
  );

  if (hidden.length === 0) return group;

  return (
    <HoverCard
      placement="below"
      content={
        <VStack gap={1.5}>
          {resolved.map((person, index) => (
            <PersonValue
              key={`${person.id ?? person.name}-${index}`}
              id={person.id}
              name={person.name}
              avatar={person.avatar}
              size="xs"
              card={false}
            />
          ))}
        </VStack>
      }
    >
      {group}
    </HoverCard>
  );
}

function GroupFace({ name, id, avatar }: { name: string; id?: string; avatar?: string }) {
  const src = useAvatarUrl(avatar, id || name);
  return <Avatar name={name} src={src} tooltip={name} />;
}
