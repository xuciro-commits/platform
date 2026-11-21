import type { StyleXStyles } from "@stylexjs/stylex";
import { HoverCard } from "@astryxdesign/core/HoverCard";
import { Icon } from "@astryxdesign/core/Icon";
import { InputGroup } from "@astryxdesign/core/InputGroup";
import { Link } from "@astryxdesign/core/Link";
import { VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { type SearchContext, Typeahead, TypeaheadItem } from "@astryxdesign/core/Typeahead";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Field } from "@astryxdesign/core/Field";
import { PlusIcon } from "@heroicons/react/24/outline";
import { useId, useMemo, useRef, useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { enterable, type InputStatus } from "./ParameterInput";
import { usePeek } from "./RecordPeek";
import { plain, present, shown } from "./values";

type Reference = { id: string; label: string };
const createOption = "__create__";

function canCreate(actions: Api.Action[] | undefined, type: string) {
  return actions?.find((action) => action.target === type && action.schema === `${type}.create` && !action.payload.some((parameter) => parameter.required && !enterable(parameter)));
}

export function ReferenceLink({ type, id, name, compact }: { type: string; id: string; name: string; compact?: boolean }) {
  const [open, setOpen] = useState(false);
  const href = useRecordHref();
  const peek = usePeek();
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
      <Link
        href={href(type, id)}
        label={compact ? name : undefined}
        onClick={(event) => {
          if (!peek || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
          event.preventDefault();
          event.stopPropagation();
          setOpen(false);
          peek.open({ type, id, name });
        }}
      >
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
  labelHidden?: boolean;
  status?: InputStatus;
  onChange: (value: string) => void;
};

export function ReferenceInput({ type, id, name, label, description, required, focused, labelHidden, status, onChange, control }: ReferenceInputProps) {
  const [picked, setPicked] = useState<Reference>();
  const [creating, setCreating] = useState(false);
  const [searchFailed, setSearchFailed] = useState(false);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const actions = useRead<Api.Action[]>("/v1/actions");
  const target = types.status === "ready" ? types.data.find((item) => item.type === type) : undefined;
  const create = actions.status === "ready" ? canCreate(actions.data, type) : undefined;
  const display = target?.display;
  const displayRef = useRef(display);
  displayRef.current = display;
  const createLabel = create && target ? `${t("New")} ${t(target.title)}…` : "";
  const createLabelRef = useRef(createLabel);
  createLabelRef.current = createLabel;
  const named = useRead<Api.RecordView>(id && !name && picked?.id !== id ? `/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}` : null);
  const known = named.status === "ready" && display ? shown(plain(named.data.values?.[display])) : "";
  const searchSource = useMemo(() => {
    const cache = new Map<string, Reference>();
    const withCreate = (items: Reference[], labelText: string) => labelText ? [...items, { id: createOption, label: labelText }] : items;
    const find = async (query: string, context?: SearchContext<Reference>) => {
      const needle = query.toLocaleLowerCase();
      const shownFirst = [...cache.values()].filter((item) => item.label.toLocaleLowerCase().includes(needle));
      context?.update(withCreate(shownFirst, createLabelRef.current));
      try {
        const [answer, described, catalog] = await Promise.all([
          request<Api.RecordPage>(`/v1/records/${encodeURIComponent(type)}?${new URLSearchParams(query ? { search: query, limit: "9" } : { limit: "9" })}`),
          displayRef.current && createLabelRef.current ? undefined : request<Api.EntityInfo[]>("/v1/entities"),
          createLabelRef.current ? undefined : request<Api.Action[]>("/v1/actions"),
        ]);
        const entity = described?.find((item) => item.type === type);
        const field = displayRef.current ?? entity?.display;
        const resolvedCreate = createLabelRef.current || (entity && canCreate(catalog, type) ? `${t("New")} ${t(entity.title)}…` : "");
        if (resolvedCreate) createLabelRef.current = resolvedCreate;
        setSearchFailed(false);
        const found = (answer.records as Record<string, unknown>[]).map((row) => ({
          id: String(row.id),
          label: shown(plain(field ? row[field] : row.id)) || String(row.id),
        }));
        for (const item of found) cache.set(item.id, item);
        const combined = context ? [...shownFirst, ...found.filter((item) => !shownFirst.some((early) => early.id === item.id))] : found;
        return withCreate(combined.slice(0, 9), resolvedCreate);
      } catch {
        setSearchFailed(true);
        return withCreate([], createLabelRef.current);
      }
    };
    return { bootstrap: () => find(""), search: find };
  }, [type]);
  const selection = id ? { id, label: name ?? (picked?.id === id ? picked.label : known || id) } : null;
  const handleCreated = async (createdId: string) => {
    setCreating(false);
    let createdLabel = createdId;
    try {
      const view = await request<Api.RecordView>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(createdId)}`);
      if (display) createdLabel = shown(plain(view.values?.[display])) || createdId;
    } catch {
      createdLabel = createdId;
    }
    setPicked({ id: createdId, label: createdLabel });
    onChange(createdId);
  };
  const input = (
    <Typeahead
      label={name ? "" : label}
      isLabelHidden={labelHidden}
      description={description}
      isRequired={required}
      status={status}
      controlXstyle={control}
      hasAutoFocus={focused}
      hasEntriesOnFocus
      maxMenuItems={11}
      placeholder={t("Find a record")}
      emptySearchText={searchFailed ? t("Records could not be searched") : t("No matching records")}
      searchSource={searchSource}
      renderItem={(item) => item.id === createOption ? <TypeaheadItem item={item} icon={<Icon icon={PlusIcon} size="sm" />} /> : <TypeaheadItem item={item} />}
      value={selection}
      onChange={(next) => {
        if (next?.id === createOption) {
          setCreating(true);
          return;
        }
        setPicked(next ?? undefined);
        onChange(next?.id ?? "");
      }}
    />
  );
  return (
    <>
      {name ? (
        <InputGroup label={label} isLabelHidden={labelHidden} description={description} isRequired={required} status={status}>
          {input}
          <ReferenceLink type={type} id={id} name={name} compact />
        </InputGroup>
      ) : input}
      {creating && create && target && (
        <ActionDialog
          action={create}
          described={target}
          heading={t(create.title)}
          confirm={t("Create")}
          revision={0}
          quick
          onClose={() => setCreating(false)}
          onDone={(createdId) => void handleCreated(createdId)}
        />
      )}
    </>
  );
}

type OneOfInputProps = {
  title: string;
  members: Api.FieldInfo[];
  values: Record<string, unknown>;
  names?: Record<string, string | undefined>;
  status?: InputStatus;
  onChange: (name: string, id: string) => void;
};

export function OneOfInput({ title, members, values, names, status, onChange }: OneOfInputProps) {
  const [picked, setPicked] = useState<string>();
  const inputID = useId();
  const linked = members.find((member) => String(values[member.name] ?? "") !== "");
  const active = members.find((member) => member.name === picked) ?? linked ?? members[0];
  return (
    <Field label={title} inputID={inputID} isGroupLabel>
      <VStack gap={2}>
        <SegmentedControl label={title} size="sm" value={active.name} onChange={setPicked}>
          {members.map((member) => <SegmentedControlItem key={member.name} value={member.name} label={t(member.title)} />)}
        </SegmentedControl>
        <ReferenceInput key={active.name} type={String(active.ref)} id={String(values[active.name] ?? "")} name={names?.[active.name]} label={t(active.title)} labelHidden
          status={status} onChange={(next) => onChange(active.name, next)} />
      </VStack>
    </Field>
  );
}
