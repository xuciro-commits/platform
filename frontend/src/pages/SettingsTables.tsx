import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Collapsible } from "@astryxdesign/core/Collapsible";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, type TableColumn, pixel, proportional } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import { createReorderable } from "@astryxdesign/core";
import { List, ListItem } from "@astryxdesign/core/List";
import { ArchiveBoxIcon, Cog6ToothIcon, PlusIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useMemo, useRef, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { reasonOf, send, useAct } from "./act";
import { type Described, describe, numeric } from "./describe";
import { PageTitle, useGutter } from "./frame";
import { iconOf } from "./icons";
import { palette } from "./options";
import { PropertyField, PropertyValue } from "./PropertyField";
import { dots } from "./Transitions";
import { plain } from "./values";
import { newView, readView } from "./view";

type Row = Record<string, unknown> & { id: string; revision?: number };

const styles = stylex.create({
  wash: { backgroundColor: colorVars["--color-background-body"] },
  scroll: { overflowX: "auto", maxWidth: "100%" },
  cell: (width: number) => ({ width, flexShrink: 0, minWidth: 0 }),
  first: { flexGrow: 1, minWidth: 160 },
  tile: {
    display: "grid",
    placeItems: "center",
    width: 24,
    height: 24,
    flexShrink: 0,
    borderRadius: radiusVars["--radius-inner"],
    backgroundColor: colorVars["--color-background-muted"],
  },
});

function Group({ described, isOpen, onOpenChange }: { described: Described; isOpen: boolean; onOpenChange: (open: boolean) => void }) {
  const entity = described.entity;
  const view = readView(entity.view == null ? null : JSON.stringify(entity.view), newView(described.columns.map((field) => field.name)));
  const sort = view.sort.map((item) => (item.direction === "descending" ? "-" : "") + item.sortKey).join(",");
  const read = useRead<Api.RecordPage>(`/v1/records/${encodeURIComponent(entity.type)}?${new URLSearchParams(sort ? { limit: "200", sort } : { limit: "200" })}`);
  const [creating, setCreating] = useState(false);
  const [failure, setFailure] = useState("");
  const act = useAct(described);
  const rows = read.status === "ready" ? (read.data.records as Row[]) : [];
  const refs = read.status === "ready" ? read.data.refs ?? {} : {};
  const allowed = (id: string, schema?: string) => Boolean(schema) && read.status === "ready" &&
    Boolean(read.data.actions?.[id]?.some((action) => action.schema === schema && action.allowed));
  const edit = described.edit;
  const archive = described.action(`${entity.type}.archive`);
  const ordered = described.field("position")?.type === "integer" && view.sort[0]?.sortKey === "position" && Boolean(edit);
  const fields = view.columns.flatMap((name) => (name === "position" && ordered ? [] : described.field(name) ?? []));

  const [pending, setPending] = useState<string[]>();
  const shown = pending ? pending.flatMap((key) => rows.find((row) => row.id === key) ?? []) : rows;
  const reorder = async (key: string, to: number) => {
    if (!edit) return;
    const keys = shown.map((row) => row.id).filter((other) => other !== key);
    keys.splice(to, 0, key);
    setPending(keys);
    setFailure("");
    try {
      for (const [index, id] of keys.entries()) {
        const row = rows.find((candidate) => candidate.id === id);
        if (row && Number(plain(row.position)) !== index + 1) await send(edit, id, { position: index + 1 }, Number(row.revision));
      }
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setPending(undefined);
    }
  };
  const latest = useRef(reorder);
  latest.current = reorder;
  const reorderable = useMemo(() => createReorderable({ onReorder: ({ key, to }) => void latest.current(String(key), to) }), []);

  const cell = (field: Api.FieldInfo) => (row: Row) => {
    const name = refs[field.name]?.[String(plain(row[field.name]))];
    const dot = field.name === "color" ? palette[String(plain(row[field.name]))] ?? dots.neutral : undefined;
    const parameter = edit && described.editable.has(field.name) && !field.readOnly && allowed(row.id, edit.schema)
      ? edit.payload.find((candidate) => candidate.name === field.name) : undefined;
    if (!parameter || !edit) return <PropertyValue field={field} value={row[field.name]} name={name} dot={dot} />;
    return (
      <PropertyField field={field} parameter={parameter} described={entity} value={row[field.name]} name={name} dot={dot} labelHidden
        onCommit={(next) => (next === row[field.name] ? undefined : send(edit, row.id, { [field.name]: next }, Number(row.revision)))} />
    );
  };

  const columns: TableColumn<Row>[] = [
    ...fields.map((field, index): TableColumn<Row> => ({
      key: field.name,
      header: t(field.title),
      width: index === 0 ? proportional(2) : field.type === "longtext" ? proportional(3) : pixel(numeric.has(field.type) ? 120 : 180),
      align: numeric.has(field.type) ? "end" : undefined,
      renderCell: cell(field),
    })),
    {
      key: "__row",
      header: "",
      width: pixel(44),
      align: "end",
      renderCell: (row) => {
        const name = described.name(row, row.id);
        return (
          <HStack gap={0} hAlign="end">
            {archive && allowed(row.id, archive.schema) && (
              <IconButton label={`${t("Archive")} ${name}`} tooltip={t("Archive")} variant="ghost" size="sm" icon={<Icon icon={ArchiveBoxIcon} size="sm" />}
                onClick={() => act.run({ action: archive, id: row.id, revision: Number(row.revision), label: t("Archive"), name, reversible: false, onRefused: setFailure })} />
            )}
          </HStack>
        );
      },
    },
  ];

  return (
    <Card padding={2}>
      <Collapsible
        isOpen={isOpen}
        onOpenChange={onOpenChange}
        trigger={
          <HStack gap={2} vAlign="center" paddingBlock={2} paddingInline={2}>
            <HStack xstyle={styles.tile}>
              <Icon icon={iconOf(entity.icon, Cog6ToothIcon)} size="sm" />
            </HStack>
            <Text weight="semibold">{t(entity.plural)}</Text>
            {read.status === "ready" && <Badge variant="neutral" label={String(rows.length)} />}
          </HStack>
        }>
        <VStack gap={2} paddingBlockStart={3}>
          {entity.description && <Text type="supporting" color="secondary">{t(entity.description)}</Text>}
          {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
          {read.status === "loading" ? <Skeleton height={96} /> : (
            ordered ? (
              <List reorderable={reorderable} density="compact" edge="bleed" hasDividers>
                {shown.map((row) => (
                  <ListItem
                    key={row.id}
                    itemKey={row.id}
                    label={
                      <HStack gap={3} vAlign="center">
                        {fields.map((field, index) => (
                          <HStack key={field.name} xstyle={index === 0 ? styles.first : styles.cell(field.type === "longtext" ? 280 : 160)}>{cell(field)(row)}</HStack>
                        ))}
                      </HStack>
                    }
                    endContent={columns[columns.length - 1].renderCell?.(row)}
                  />
                ))}
              </List>
            ) : (
              <VStack xstyle={styles.scroll}>
                <Table data={rows} columns={columns} idKey="id" density="compact" dividers="rows" textOverflow="truncate" />
              </VStack>
            )
          )}
          {described.create && (
            <HStack paddingInline={2} paddingBlockEnd={1}>
              <Button label={`${t("Add")} ${t(entity.title)}`} variant="ghost" size="sm" icon={<Icon icon={PlusIcon} size="sm" />} onClick={() => setCreating(true)} />
            </HStack>
          )}
        </VStack>
      </Collapsible>
      {creating && described.create && (
        <ActionDialog action={described.create} described={entity} heading={t(described.create.title)} confirm={t("Create")}
          onClose={() => setCreating(false)} onDone={() => setCreating(false)} />
      )}
      {act.element}
    </Card>
  );
}

export function SettingsTables({ title, types }: { title: string; types: string[] }) {
  const gutter = useGutter();
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const actions = useRead<Api.Action[]>("/v1/actions");
  const [closed, setClosed] = useState<Set<string>>(() => new Set());
  const groups = useMemo(() => {
    if (entities.status !== "ready" || actions.status !== "ready") return [];
    return types.flatMap((type) => {
      const entity = entities.data.find((candidate) => candidate.type === type);
      return entity ? [describe(entity, actions.data)] : [];
    });
  }, [entities, actions, types]);
  const allOpen = closed.size === 0;
  return (
    <Layout
      height="auto"
      contentWidth={1200}
      xstyle={styles.wash}
      header={
        <LayoutHeader padding={gutter}>
          <PageTitle
            title={t(title)}
            actions={<Button label={allOpen ? t("Collapse all") : t("Expand all")} variant="ghost" onClick={() => setClosed(allOpen ? new Set(types) : new Set())} />}>
            <Text type="body" color="secondary">{t("Lists the rest of the application picks from. Changes apply to new and existing records at once.")}</Text>
          </PageTitle>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={gutter}>
          <VStack gap={4}>
            {groups.length === 0 && <Skeleton height={160} />}
            {groups.map((described) => (
              <Group
                key={described.type}
                described={described}
                isOpen={!closed.has(described.type)}
                onOpenChange={(open) => setClosed((previous) => {
                  const next = new Set(previous);
                  if (open) next.delete(described.type);
                  else next.add(described.type);
                  return next;
                })}
              />
            ))}
          </VStack>
        </LayoutContent>
      }
    />
  );
}
