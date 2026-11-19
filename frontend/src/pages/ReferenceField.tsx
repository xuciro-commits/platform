import type { StyleXStyles } from "@stylexjs/stylex";
import { HoverCard } from "@astryxdesign/core/HoverCard";
import { Icon } from "@astryxdesign/core/Icon";
import { InputGroup } from "@astryxdesign/core/InputGroup";
import { Link } from "@astryxdesign/core/Link";
import { VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Typeahead } from "@astryxdesign/core/Typeahead";
import { useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import type { InputStatus } from "./ParameterInput";
import { plain, present, shown } from "./values";

type Reference = { id: string; label: string };

export function ReferenceLink({ type, id, name, compact }: { type: string; id: string; name: string; compact?: boolean }) {
  const [open, setOpen] = useState(false);
  const types = useRead<Api.EntityInfo[]>(open ? "/v1/entities" : null);
  const read = useRead<Api.RecordView>(open ? `/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}` : null);
  const target = types.status === "ready" ? types.data.find((item) => item.type === type) : undefined;
  const values = read.status === "ready" ? read.data.values : undefined;
  const lifecycle = target?.lifecycle;
  const state = lifecycle?.states?.find((candidate) => candidate.name === values?.[lifecycle.field]);
  const fields = target?.fields.filter((field) => field.name !== target.display && field.name !== lifecycle?.field && values?.[field.name] != null).slice(0, 3);
  return (
    <HoverCard
      onOpenChange={setOpen}
      placement="below"
      content={
        <VStack gap={2}>
          <Heading level={4}>{name}</Heading>
          {state && <Text type="supporting">{t(state.title)}</Text>}
          {fields && fields.length > 0 && (
            <MetadataList>
              {fields.map((field) => <MetadataListItem key={field.name} label={t(field.title)}>{present(field, values?.[field.name])}</MetadataListItem>)}
            </MetadataList>
          )}
        </VStack>
      }
    >
      <Link href={`/ontology/objects/${encodeURIComponent(type)}/${encodeURIComponent(id)}`} label={compact ? name : undefined}>
        {compact ? <Icon icon="externalLink" size="sm" /> : name}
      </Link>
    </HoverCard>
  );
}

type ReferenceInputProps = {
  control?: StyleXStyles;
  type: string;
  id: string;
  name?: string;
  label: string;
  description?: string;
  required?: boolean;
  focused?: boolean;
  status?: InputStatus;
  onChange: (value: string) => void;
};

export function ReferenceInput({ type, id, name, label, description, required, focused, status, onChange, control }: ReferenceInputProps) {
  const [picked, setPicked] = useState<Reference>();
  const [searchFailed, setSearchFailed] = useState(false);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const target = types.status === "ready" ? types.data.find((item) => item.type === type) : undefined;
  const display = target?.display;
  const searchSource = useMemo(() => ({
    bootstrap: () => [],
    search: async (query: string) => {
      try {
        const [answer, described] = await Promise.all([
          request<Api.RecordPage>(`/v1/records/${encodeURIComponent(type)}?search=${encodeURIComponent(query)}&limit=10`),
          display ? undefined : request<Api.EntityInfo[]>("/v1/entities"),
        ]);
        const field = display ?? described?.find((item) => item.type === type)?.display;
        setSearchFailed(false);
        return (answer.records as Record<string, unknown>[]).map((row) => ({
          id: String(row.id),
          label: shown(plain(field ? row[field] : row.id)) || String(row.id),
        }));
      } catch {
        setSearchFailed(true);
        return [];
      }
    },
  }), [type, display]);
  const selection = id ? { id, label: name ?? (picked?.id === id ? picked.label : id) } : null;
  const input = (
    <Typeahead
      label={name ? "" : label}
      description={description}
      isRequired={required}
      status={status}
      controlXstyle={control}
      hasAutoFocus={focused}
      placeholder={t("Find a record")}
      emptySearchText={searchFailed ? t("Records could not be searched") : t("No matching records")}
      searchSource={searchSource}
      value={selection}
      onChange={(next) => {
        setPicked(next ?? undefined);
        onChange(next?.id ?? "");
      }}
    />
  );
  return name ? (
    <InputGroup label={label} description={description} isRequired={required} status={status}>
      {input}
      <ReferenceLink type={type} id={id} name={name} compact />
    </InputGroup>
  ) : input;
}
