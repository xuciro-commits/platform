import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Table, TableBody, TableCell, TableFooter, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Thumbnail } from "@astryxdesign/core/Thumbnail";
import { colorVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useMemo } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { CodeValue, SemanticValue, StatusValue, TemporalValue } from "../semantic";
import { describe, type Described, headerStatus, type Values } from "./describe";
import { qrDataUri } from "./qr";

type PrintConfig = { title?: string; fields?: string[]; footer?: string };
type Kept = { id: string; revision: number; archived?: boolean };
type Line = Values & { id: string; revision: number };

const styles = stylex.create({
  page: {
    width: "100%",
    maxWidth: { default: "210mm", "@media print": "100%" },
    marginInline: "auto",
    padding: { default: spacingVars["--spacing-6"], "@media print": spacingVars["--spacing-4"] },
    backgroundColor: colorVars["--color-background-surface"],
  },
  toolbar: {
    display: { default: "flex", "@media print": "none" },
  },
  section: {
    breakInside: "avoid",
  },
  footer: {
    breakInside: "avoid",
  },
});

function PrintLines({ parent, id, set, child }: { parent: string; id: string; set: Api.RecordSet; child: Described }) {
  const read = useRead<Api.RecordSetResult>(`/v1/record-sets/${parent}/${encodeURIComponent(id)}/${encodeURIComponent(set.name)}`);
  if (read.status !== "ready") return null;
  const answer = read.data;
  const rows = answer.records as Line[];
  if (rows.length === 0) return null;
  const via = child.entity.fields.find((field) => field.ref === parent && field.inverse === set.from);
  const withheld = new Map(answer.authorization?.elements.map((item) => [item.record, new Set(item.decision.withheld ?? [])]));
  const fields = child.table.filter((field) => field.name !== via?.name && !field.aside && rows.some((row) => !withheld.get(row.id)?.has(field.name)));
  if (fields.length === 0) return null;
  const title = t(child.plural);
  return (
    <VStack gap={2}>
      <Heading level={3} accessibilityLevel={2}>{title}</Heading>
      <Table aria-label={title} density="compact">
        <TableHeader>
          <TableRow>
            {fields.map((field) => <TableHeaderCell key={field.name}>{t(field.title)}</TableHeaderCell>)}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const hidden = withheld.get(row.id);
            return (
              <TableRow key={row.id}>
                {fields.map((field) => (
                  <TableCell key={field.name}>
                    {hidden?.has(field.name) ? null : (
                      <SemanticValue
                        field={field}
                        value={row[field.name]}
                        name={answer.refs?.[field.name]?.[String(row[field.name] ?? "")]}
                        size="sm"
                        surface="cell"
                        interactive={false}
                      />
                    )}
                  </TableCell>
                ))}
              </TableRow>
            );
          })}
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
            </TableRow>
          </TableFooter>
        )}
      </Table>
    </VStack>
  );
}

export function RecordPrint({
  described,
  id,
  base,
  values,
  kept,
  refs,
  restricted,
  entities,
  catalog,
}: {
  described: Described;
  id: string;
  base: string;
  values: Values;
  kept: Kept;
  refs?: Api.RecordView["refs"];
  restricted: ReadonlySet<string>;
  entities: Api.EntityInfo[];
  catalog: Api.Action[];
}) {
  const me = useRead<Api.MeView>("/v1/me");
  const printConfig = (described.entity.view as { print?: PrintConfig } | undefined)?.print;
  const allowedFields = printConfig?.fields?.length ? new Set(printConfig.fields) : null;
  const canPrint = (field: Api.FieldInfo) => !restricted.has(field.name) && (!allowedFields || allowedFields.has(field.name)) && described.shows(field, values) && values[field.name] != null && values[field.name] !== "";
  const name = described.name(values, id);
  const heading = printConfig?.title ? t(printConfig.title) : name;
  const status = headerStatus(described, values, kept.archived);
  const sections = described.sections
    .map((section) => ({ title: section.title, fields: section.fields.filter(canPrint) }))
    .filter((section) => section.fields.length > 0);
  const asideFields = described.aside.filter(canPrint);
  const printedAt = useMemo(() => new Date().toISOString(), []);
  const recordUrl = `${window.location.origin}${base}`;
  const qr = useMemo(() => qrDataUri(recordUrl), [recordUrl]);
  const printer = me.status === "ready" ? (me.data.account?.displayName || me.data.principalId) : "";

  return (
    <VStack gap={5} xstyle={styles.page} role="main">
      <HStack gap={2} vAlign="center" xstyle={styles.toolbar}>
        <Button label={t("Back to record")} variant="secondary" href={base} />
        <StackItem size="fill" />
        <Button label={t("Print")} variant="primary" onClick={() => window.print()} />
      </HStack>

      <VStack gap={2}>
        <HStack gap={3} vAlign="center" wrap="wrap">
          <StackItem size="fill">
            <Heading level={1}>{heading}</Heading>
          </StackItem>
          {status && <StatusValue label={t(status.title)} tone={status.tone} size="md" appearance="pill" />}
        </HStack>
        <HStack gap={3} vAlign="center" wrap="wrap">
          {printConfig?.title && <Text type="supporting">{name}</Text>}
          <CodeValue value={id} size="sm" />
        </HStack>
      </VStack>

      <Divider />

      {sections.map((section) => (
        <VStack key={section.title} gap={2} xstyle={styles.section}>
          <Heading level={3} accessibilityLevel={2}>{t(section.title)}</Heading>
          <MetadataList>
            {section.fields.map((field) => (
              <MetadataListItem key={field.name} label={t(field.title)}>
                <SemanticValue
                  field={field}
                  value={values[field.name]}
                  name={refs?.[field.name]?.[String(values[field.name] ?? "")]}
                  size="md"
                  surface="detail"
                  interactive={false}
                />
              </MetadataListItem>
            ))}
          </MetadataList>
        </VStack>
      ))}

      {described.entity.sets?.flatMap((set) => {
        if (set.direction !== "inverse") return [];
        const type = entities.find((entry) => entry.type === set.object);
        return type ? <PrintLines key={set.name} parent={described.type} id={id} set={set} child={describe(type, catalog)} /> : [];
      })}

      {asideFields.length > 0 && (
        <VStack gap={2} xstyle={styles.section}>
          <Heading level={3} accessibilityLevel={2}>{t("Properties")}</Heading>
          <MetadataList>
            {asideFields.map((field) => (
              <MetadataListItem key={field.name} label={t(field.title)}>
                <SemanticValue
                  field={field}
                  value={values[field.name]}
                  name={refs?.[field.name]?.[String(values[field.name] ?? "")]}
                  size="md"
                  surface="detail"
                  interactive={false}
                />
              </MetadataListItem>
            ))}
          </MetadataList>
        </VStack>
      )}

      <Divider />

      <HStack gap={4} vAlign="center" wrap="wrap" xstyle={styles.footer}>
        <StackItem size="fill">
          <VStack gap={2}>
            <MetadataList>
              <MetadataListItem label={t("Record ID")}>
                <CodeValue value={id} size="sm" />
              </MetadataListItem>
              <MetadataListItem label={t("Revision")}>
                <Text type="body" hasTabularNumbers>{String(kept.revision)}</Text>
              </MetadataListItem>
              {printer && (
                <MetadataListItem label={t("Printed by")}>
                  <Text type="body">{printer}</Text>
                </MetadataListItem>
              )}
              <MetadataListItem label={t("Printed at")}>
                <TemporalValue value={printedAt} kind="datetime" mode="absolute" size="sm" surface="detail" />
              </MetadataListItem>
            </MetadataList>
            {printConfig?.footer && <Text type="supporting" color="secondary">{t(printConfig.footer)}</Text>}
          </VStack>
        </StackItem>
        <Thumbnail src={qr} alt={t("Record QR code")} label={t("Record QR code")} />
      </HStack>
    </VStack>
  );
}
