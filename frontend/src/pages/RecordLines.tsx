import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Table, TableBody, TableCell, TableFooter, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TrashIcon } from "@heroicons/react/24/outline";
import { useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { useAct } from "./act";
import { type Described, type Values } from "./describe";
import { enterable } from "./ParameterInput";
import { PropertyField } from "./PropertyField";
import { EditorConflict, useEditor } from "./RecordFields";
import { SemanticValue } from "../semantic";

type Line = Values & { id: string; revision: number };

export function RecordLines({ parent, id, set, child }: { parent: string; id: string; set: Api.RecordSet; child: Described }) {
  const read = useRead<Api.RecordSetResult>(`/v1/record-sets/${parent}/${encodeURIComponent(id)}/${encodeURIComponent(set.name)}`);
  const [creating, setCreating] = useState(false);
  const [refusal, setRefusal] = useState("");
  const act = useAct(child);
  const via = child.entity.fields.find((field) => field.ref === parent && field.inverse === set.from);
  const create = via && child.create;
  const fields = child.table.filter((field) => field.name !== via?.name && !field.aside);
  const archive = child.action(`${child.type}.archive`);
  const title = t(child.plural);
  const answer = read.status === "ready" ? read.data : null;
  const withheld = new Map(answer?.authorization?.elements.map((item) => [item.record, item.decision.withheld ?? []]));
  return (
    <VStack gap={3}>
      <HStack vAlign="center" gap={2}>
        <StackItem size="fill"><Heading level={3} accessibilityLevel={2}>{title}</Heading></StackItem>
        {answer && create && <Button label={t("Add a row")} variant="secondary" onClick={() => setCreating(true)} />}
      </HStack>
      {read.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
      {read.status === "error" && <Banner status="error" title={t("Rows could not be loaded")} description={read.reason} />}
      {read.status === "refused" && <Banner status="warning" title={t("You cannot see these rows")} description={read.reason} />}
      {refusal && <Banner status="error" title={t("Not done")} description={refusal} />}
      {answer?.partial && <Banner status="warning" title={t("Some rows are unavailable")} />}
      {answer && answer.records.length === 0 && <EmptyState headingLevel={4} title={t("No rows yet")} description={create ? t("Add a row to get started.") : undefined} />}
      {answer && answer.records.length > 0 && (
        <Table aria-label={title} density="compact">
          <TableHeader>
            <TableRow>
              {fields.map((field) => <TableHeaderCell key={field.name}>{t(field.title)}</TableHeaderCell>)}
              {archive && <TableHeaderCell>{t("Actions")}</TableHeaderCell>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {(answer.records as Line[]).map((row) => (
              <LineRow key={row.id} row={row} child={child} fields={fields} withheld={withheld.get(row.id)}
                archive={archive && (() => act.run({
                  action: archive, id: row.id, revision: row.revision, label: t("Archive row"), name: child.name(row, row.id),
                  reversible: false, onRefused: setRefusal,
                }))} />
            ))}
          </TableBody>
          {answer.sums && (
            <TableFooter>
              <TableRow>
                {fields.map((field, index) => (
                  <TableCell key={field.name}>
                    {index === 0 && <Text type="supporting" weight="medium">{t("Totals")}</Text>}
                    <SemanticValue field={field} value={answer.sums?.[field.name]} size="sm" surface="cell" interactive={false} />
                  </TableCell>
                ))}
                {archive && <TableCell />}
              </TableRow>
            </TableFooter>
          )}
        </Table>
      )}
      {act.element}
      {creating && create && via && (
        <ActionDialog action={create} described={child.entity} heading={t("Add a row")} confirm={t("Create")} revision={0}
          fixed={{ [via.name]: id }} onClose={() => setCreating(false)} onDone={() => setCreating(false)} />
      )}
    </VStack>
  );
}

function LineRow({ row, child, fields, withheld, archive }: { row: Line; child: Described; fields: Api.FieldInfo[]; withheld?: string[]; archive?: () => void }) {
  const editor = useEditor(child, row.id, row, row.revision);
  return (
    <TableRow>
      {fields.map((field, index) => {
        const parameter = child.edit?.payload.find((item) => item.name === field.name && enterable(item));
        return (
          <TableCell key={field.name}>
            {index === 0 && <EditorConflict editor={editor} described={child} />}
            {withheld?.includes(field.name) ? <Text type="supporting">{t("Cannot view")}</Text>
              : (
                <PropertyField field={field} parameter={parameter} described={child.entity} value={editor.values[field.name]} form={editor.values}
                  status={editor.status[field.name]} saving={editor.saving.has(field.name)} labelHidden
                  onChange={parameter && ((value) => editor.change(parameter, value))} onCommit={() => editor.commit(field.name)} />
              )}
          </TableCell>
        );
      })}
      {archive && (
        <TableCell>
          <IconButton label={t("Archive row")} tooltip={t("Archive row")} variant="ghost" size="sm" icon={<Icon icon={TrashIcon} size="sm" />} onClick={archive} />
        </TableCell>
      )}
    </TableRow>
  );
}
