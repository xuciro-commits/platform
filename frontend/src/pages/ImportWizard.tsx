import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FileInput } from "@astryxdesign/core/FileInput";
import { Layout, LayoutContent, LayoutFooter, LayoutHeader } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Stack";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Step, Stepper } from "@astryxdesign/core/Stepper";
import { Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, request, reread } from "../host";
import { t } from "../i18n";
import { reasonOf } from "./act";
import type { Described } from "./describe";

type ImportRows = Promise<Api.ImportRow[]>;
export type ImportField = { name: string; title: string };
export type ImportTarget = {
  type: string;
  title: string;
  fields: ImportField[];
  withId?: boolean;
  run: (rows: Record<string, string>[], preview: boolean) => ImportRows;
};

type Line = { row: number; cells: string[]; result?: Api.ImportRow; done?: boolean };

const steps = ["File", "Columns", "Check"];
const skip = "-";
const shown = 100;
const plain = (text: string) => text.trim().toLowerCase();
const quote = (cell: string) => `"${cell.replaceAll("\"", "\"\"")}"`;
const unreadable = "ERROR_CODE_INVALID_ARGUMENT: ";

function targetOf(described: Described): ImportTarget {
  const fields = (described.create?.payload ?? []).flatMap((parameter) => {
    const field = described.field(parameter.name);
    return field && !field.readOnly && field.type !== "lines" && field.type !== "attachment" ? [{ name: field.name, title: field.title }] : [];
  });
  return {
    type: described.type,
    title: described.plural,
    fields,
    withId: true,
    run: (rows, preview) => {
      const names = rows[0] ? Object.keys(rows[0]) : [];
      const csv = [names, ...rows.map((row) => names.map((name) => row[name] ?? ""))]
        .map((cells) => cells.map(quote).join(",")).join("\r\n");
      return request<Api.ImportRow[]>(`/v1/import/${encodeURIComponent(described.type)}${preview ? "?preview=true" : ""}`, { body: new Blob([csv], { type: "text/csv" }) });
    },
  };
}

export function ImportWizard({ described, target, onClose }: { described?: Described; target?: ImportTarget; onClose: () => void }) {
  const resolved = target ?? (described ? targetOf(described) : undefined);
  const [step, setStep] = useState(0);
  const [file, setFile] = useState<File | null>(null);
  const [columns, setColumns] = useState<string[]>([]);
  const [mapping, setMapping] = useState<string[]>([]);
  const [lines, setLines] = useState<Line[]>([]);
  const [imported, setImported] = useState(0);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  if (!resolved) return null;
  const options = [
    { value: skip, label: t("Skip this column") },
    ...(resolved.withId ? [{ value: "id", label: t("Record ID") }] : []),
    ...resolved.fields.map((field) => ({ value: field.name, label: t(field.title) })),
  ];
  const rawTitle = (name: string) => resolved.fields.find((field) => field.name === name)?.title ?? "";
  const titleOf = (name: string) => options.find((option) => option.value === name)?.label ?? name;
  const guess = (column: string) => options.find((option) => option.value !== skip &&
    [option.value, option.label, rawTitle(option.value)].some((name) => plain(name) === plain(column)))?.value;

  const read = async (next: File | null) => {
    setFile(next);
    setFailure("");
    setColumns([]);
    if (!next) return;
    setBusy(true);
    try {
      const sheet = await request<Api.ImportSheet>(`/v1/import/${encodeURIComponent(resolved.type)}/sheet`, { body: next });
      const taken = new Set<string>();
      setColumns(sheet.columns);
      setMapping(sheet.columns.map((column) => {
        const found = guess(column);
        if (!found || taken.has(found)) return skip;
        taken.add(found);
        return found;
      }));
      setLines(sheet.rows.map((cells, index) => ({ row: index + 1, cells })));
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const used = mapping.flatMap((name, index) => (name === skip ? [] : [{ name, index }]));
  const pending = lines.filter((line) => !line.done);
  const run = async (preview: boolean) => {
    const sending = preview ? pending : pending.filter((line) => line.result?.outcome === "ok");
    const rows = sending.map((line) => Object.fromEntries(used.map(({ name, index }) => [name, line.cells[index] ?? ""])));
    setBusy(true);
    setFailure("");
    try {
      const results = await resolved.run(rows, preview);
      const answered = new Map(sending.map((line, index) => [line.row, results[index]]));
      setLines((current) => current.map((line) => {
        const result = answered.get(line.row);
        return result ? { ...line, result, done: !preview && result.outcome === "ok" } : line;
      }));
      if (!preview) {
        setImported((count) => count + results.filter((result) => result.outcome === "ok").length);
        reread();
      }
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  const edit = (row: number, index: number, value: string) => setLines((current) => current.map((line) => (
    line.row === row ? { ...line, result: undefined, cells: line.cells.map((cell, at) => (at === index ? value : cell)) } : line
  )));

  const problems = pending.filter((line) => line.result?.outcome !== "ok");
  const ready = pending.length - problems.length;
  const checked = pending.every((line) => line.result);
  const blamed = (line: Line) => {
    const outcome = line.result?.outcome ?? "";
    return new Set([...(outcome.startsWith(unreadable) ? [outcome.slice(unreadable.length)] : []), ...(line.result?.error?.issues ?? []).map((issue) => issue.path[0] ?? "")]);
  };
  const problemOf = (line: Line) => {
    if (!line.result) return t("Not checked yet");
    if (line.result.outcome.startsWith(unreadable)) return `${t("This value cannot be read")}: ${titleOf(line.result.outcome.slice(unreadable.length))}`;
    return line.result.error?.message || t("Not saved");
  };
  const next = () => {
    if (step === 1) void run(true);
    setStep((current) => current + 1);
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={880} purpose="form">
      <Layout
        height="fill"
        padding={4}
        defaultHasDividers
        header={
          <>
            <DialogHeader title={`${t("Import")} · ${t(resolved.title)}`} onOpenChange={(open) => !open && onClose()} hasDivider={false} />
            <LayoutHeader hasDivider={false}>
              <Stepper activeStep={step} orientation="horizontal" density="compact" label={t("Import")}>
                {steps.map((label, index) => <Step key={label} step={index} label={t(label)} />)}
              </Stepper>
            </LayoutHeader>
          </>
        }
        content={
          <LayoutContent>
            <VStack gap={5}>
              {failure && <Banner status="error" title={t("Not done")} description={failure} />}
              {step === 0 && (
                <FileInput label={t("File")} description={t("A CSV file or an Excel workbook; the first row names the columns.")} accept=".csv,.xlsx,text/csv"
                  placeholder={t("Choose file")} mode="dropzone" value={file} isLoading={busy} onChange={(chosen) => void read(chosen instanceof File ? chosen : null)} />
              )}
              {step === 0 && columns.length > 0 && <Text type="supporting">{`${lines.length} ${t("records")}`}</Text>}
              {step === 1 && (
                <Table aria-label={t("Columns")} density="compact">
                  <TableHeader>
                    <TableRow>
                      <TableHeaderCell>{t("Columns")}</TableHeaderCell>
                      <TableHeaderCell>{t("Fields")}</TableHeaderCell>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {columns.map((column, index) => (
                      <TableRow key={`${index}:${column}`}>
                        <TableCell>
                          <VStack gap={0}>
                            <Text>{column}</Text>
                            <Text type="supporting">{lines.find((line) => line.cells[index])?.cells[index] ?? ""}</Text>
                          </VStack>
                        </TableCell>
                        <TableCell>
                          <Selector label={`${t("Fields")}: ${column}`} isLabelHidden size="sm" options={options} value={mapping[index]}
                            onChange={(value) => setMapping((current) => current.map((name, at) => (at === index ? value : name === value && value !== skip ? skip : name)))} />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              {step === 2 && (
                <VStack gap={4}>
                  {imported > 0 && <Banner status="success" title={`${imported} ${t("records imported")}`} />}
                  {busy && <Text type="supporting">{t("Checking…")}</Text>}
                  {!busy && checked && pending.length > 0 && (
                    <Text>{`${ready} ${t("can be imported")} · ${problems.length} ${t("need fixing")}`}</Text>
                  )}
                  {problems.length > 0 && (
                    <Table aria-label={t("need fixing")} density="compact">
                      <TableHeader>
                        <TableRow>
                          <TableHeaderCell>{t("Row")}</TableHeaderCell>
                          {used.map(({ name }) => <TableHeaderCell key={name}>{titleOf(name)}</TableHeaderCell>)}
                          <TableHeaderCell>{t("Outcome")}</TableHeaderCell>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {problems.slice(0, shown).map((line) => {
                          const wrong = blamed(line);
                          return (
                            <TableRow key={line.row}>
                              <TableCell>{line.row}</TableCell>
                              {used.map(({ name, index }) => (
                                <TableCell key={name}>
                                  <TextInput label={`${titleOf(name)} · ${t("Row")} ${line.row}`} isLabelHidden size="sm" value={line.cells[index] ?? ""}
                                    status={wrong.has(name) ? { type: "error" } : undefined} onChange={(value) => edit(line.row, index, value)} />
                                </TableCell>
                              ))}
                              <TableCell>
                                <HStack gap={2} vAlign="center">
                                  <StatusDot variant={line.result ? "error" : "neutral"} label={line.result ? t("Not saved") : t("Not checked yet")} />
                                  <Text>{problemOf(line)}</Text>
                                </HStack>
                              </TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  )}
                  {shown < problems.length && <Text type="supporting">{`${shown} / ${problems.length}`}</Text>}
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <Button label={t("Back")} variant="secondary" isDisabled={step === 0 || busy || imported > 0} onClick={() => setStep((current) => current - 1)} />
              <StackItem size="fill" />
              {step < 2 && <Button label={t("Continue")} variant="primary" isDisabled={busy || !columns.length || (step === 1 && !used.length)} onClick={next} />}
              {step === 2 && pending.length > 0 && <Button label={t("Check")} variant="secondary" isDisabled={busy || checked} onClick={() => void run(true)} />}
              {step === 2 && pending.length > 0 && <Button label={`${t("Import")} ${ready}`} variant="primary" isDisabled={busy || !checked || !ready} onClick={() => void run(false)} />}
              {step === 2 && pending.length === 0 && <Button label={t("Done")} variant="primary" onClick={onClose} />}
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
