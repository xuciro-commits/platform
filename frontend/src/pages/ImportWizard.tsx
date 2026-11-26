import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FileInput } from "@astryxdesign/core/FileInput";
import { Layout, LayoutContent, LayoutFooter, LayoutHeader } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Stack";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Step, Stepper } from "@astryxdesign/core/Stepper";
import { Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useMemo, useState } from "react";
import { type Api, request, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf } from "./act";
import { type Described, describe } from "./describe";
import {
  type ObjectPlan,
  type PlanLine,
  buildObjectPlan,
  matchSheetPlan,
  plainKey,
  readWorkbookSheets,
  topologicalOrder,
} from "./workbook";

type ImportRows = Promise<Api.ImportRow[]>;
export type ImportField = { name: string; title: string };
export type ImportTarget = {
  type: string;
  title: string;
  fields: ImportField[];
  withId?: boolean;
  run: (rows: Record<string, string>[], preview: boolean) => ImportRows;
};

type SheetState = {
  name: string;
  columns: string[];
  plan: ObjectPlan;
  mapping: string[];
  lines: PlanLine[];
};

const steps = ["File", "Columns", "Check"];
const skip = "-";
const shown = 100;
const unreadable = "ERROR_CODE_INVALID_ARGUMENT: ";

function planFromTarget(target: ImportTarget): ObjectPlan {
  return {
    type: target.type,
    title: target.title,
    displayKey: target.fields[0]?.name ?? "id",
    fields: target.fields.map((field) => ({
      key: field.name,
      field: field.name,
      title: field.title,
      aliases: [field.name, field.title, t(field.title)],
      source: "own",
    })),
    withId: Boolean(target.withId),
    run: async (lines, used, preview) => {
      const rows = lines.map((line) => Object.fromEntries(used.map(({ name, index }) => [name, line.cells[index] ?? ""])));
      const answered = await target.run(rows, preview);
      const results = new Map<number, Api.ImportRow>();
      lines.forEach((line, idx) => { if (answered[idx]) results.set(line.row, answered[idx]); });
      return { results, baseIds: new Map() };
    },
  };
}

function autoMapColumns(columns: string[], plan: ObjectPlan): string[] {
  const taken = new Set<string>();
  return columns.map((column) => {
    const key = plainKey(column);
    if (plan.withId && [plainKey("id"), plainKey("Record ID"), plainKey(t("Record ID"))].includes(key) && !taken.has("id")) {
      taken.add("id");
      return "id";
    }
    const found = plan.fields.find((field) => !taken.has(field.key) && field.aliases.some((alias) => plainKey(alias) === key));
    if (!found) return skip;
    taken.add(found.key);
    return found.key;
  });
}

function optionsOf(plan: ObjectPlan) {
  return [
    { value: skip, label: t("Skip this column") },
    ...(plan.withId ? [{ value: "id", label: t("Record ID") }] : []),
    ...plan.fields.map((field) => ({ value: field.key, label: field.title.includes(" · ") ? field.title.split(" · ").map((part) => t(part)).join(" · ") : t(field.title) })),
  ];
}

function titleInPlan(plan: ObjectPlan, key: string) {
  return optionsOf(plan).find((option) => option.value === key)?.label ?? key;
}

export function ImportWizard({ described, target, onClose }: { described?: Described; target?: ImportTarget; onClose: () => void }) {
  const entitiesRead = useRead<Api.EntityInfo[]>(described ? "/v1/entities" : null);
  const actionsRead = useRead<Api.Action[]>(described ? "/v1/actions" : null);

  const { primaryPlan, allPlans } = useMemo(() => {
    if (target) {
      const plan = planFromTarget(target);
      return { primaryPlan: plan, allPlans: [plan] };
    }
    if (!described) return { primaryPlan: undefined, allPlans: [] as ObjectPlan[] };
    const entities = entitiesRead.status === "ready" ? entitiesRead.data : [described.entity];
    const actions = actionsRead.status === "ready" ? actionsRead.data : [
      ...(described.create ? [described.create] : []),
      ...(described.edit ? [described.edit] : []),
    ];
    const byType = new Map<string, Described>();
    for (const entity of entities) byType.set(entity.type, describe(entity, actions));
    byType.set(described.type, described);
    const primary = buildObjectPlan(described, byType);
    const plans: ObjectPlan[] = [];
    for (const item of byType.values()) {
      if (!item.create || item.type.startsWith("build.") || item.type.startsWith("platform.")) continue;
      plans.push(item.type === described.type ? primary : buildObjectPlan(item, byType));
    }
    if (!plans.some((plan) => plan.type === primary.type)) plans.unshift(primary);
    return { primaryPlan: primary, allPlans: plans };
  }, [target, described, entitiesRead, actionsRead]);

  const [step, setStep] = useState(0);
  const [file, setFile] = useState<File | null>(null);
  const [sheets, setSheets] = useState<SheetState[]>([]);
  const [activeSheet, setActiveSheet] = useState(0);
  const [imported, setImported] = useState(0);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  if (!primaryPlan) return null;

  const read = async (next: File | null) => {
    setFile(next);
    setFailure("");
    setSheets([]);
    setActiveSheet(0);
    setImported(0);
    if (!next) return;
    setBusy(true);
    try {
      const parsed = !target ? await readWorkbookSheets(next) : null;
      if (parsed && parsed.length > 0) {
        const built: SheetState[] = parsed.map((sheet) => {
          const plan = parsed.length > 1 ? matchSheetPlan(sheet.name, primaryPlan, allPlans) : primaryPlan;
          return {
            name: sheet.name,
            columns: sheet.columns,
            plan,
            mapping: autoMapColumns(sheet.columns, plan),
            lines: sheet.rows.map((cells, index) => ({ row: index + 1, cells })),
          };
        });
        setSheets(built);
      } else {
        const single = await request<Api.ImportSheet>(`/v1/import/${encodeURIComponent(primaryPlan.type)}/sheet`, { body: next });
        setSheets([{
          name: primaryPlan.title,
          columns: single.columns,
          plan: primaryPlan,
          mapping: autoMapColumns(single.columns, primaryPlan),
          lines: single.rows.map((cells, index) => ({ row: index + 1, cells })),
        }]);
      }
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const multiSheet = sheets.length > 1;
  const currentSheet = sheets[activeSheet] ?? sheets[0];
  const totalLines = sheets.reduce((sum, sheet) => sum + sheet.lines.length, 0);
  const usedOf = (sheet: SheetState) => sheet.mapping.flatMap((name, index) => (name === skip ? [] : [{ name, index }]));
  const allPending = sheets.flatMap((sheet, sheetIndex) => sheet.lines.filter((line) => !line.done).map((line) => ({ sheetIndex, sheet, line })));
  const problems = allPending.filter(({ line }) => line.result?.outcome !== "ok");
  const ready = allPending.length - problems.length;
  const checked = allPending.length > 0 && allPending.every(({ line }) => Boolean(line.result));
  const anyMapped = sheets.length > 0 && sheets.every((sheet) => usedOf(sheet).length > 0);

  const run = async (preview: boolean) => {
    setBusy(true);
    setFailure("");
    try {
      const order = topologicalOrder(sheets);
      const nextSheets = sheets.map((sheet) => ({ ...sheet, lines: sheet.lines.map((line) => ({ ...line })) }));
      const pendingRefs = new Map<string, Set<string>>();
      let newlyImported = 0;

      const recordDisplayValues = (sheet: SheetState) => {
        const displayIndex = sheet.mapping.findIndex((key) => key === sheet.plan.displayKey);
        if (displayIndex < 0) return;
        const set = pendingRefs.get(sheet.plan.type) ?? new Set<string>();
        for (const line of sheet.lines) {
          if (line.done || line.result?.outcome === "ok") {
            const text = plainKey(line.cells[displayIndex] ?? "");
            if (text) set.add(text);
          }
        }
        pendingRefs.set(sheet.plan.type, set);
      };

      for (const sheetIndex of order) {
        const sheet = nextSheets[sheetIndex];
        const used = usedOf(sheet);
        const sending = sheet.lines.filter((line) => !line.done && (preview || line.result?.outcome === "ok"));
        if (sending.length > 0 && used.length > 0) {
          const { results, baseIds } = await sheet.plan.run(sending, used, preview, pendingRefs);
          sheet.lines = sheet.lines.map((line) => {
            const baseId = baseIds.get(line.row) ?? line.baseId;
            const result = results.get(line.row);
            if (!result) return baseId !== line.baseId ? { ...line, baseId } : line;
            const ok = !preview && result.outcome === "ok";
            if (ok) newlyImported += 1;
            return { ...line, baseId, result, done: ok };
          });
        }
        recordDisplayValues(sheet);
      }

      setSheets(nextSheets);
      if (!preview && newlyImported > 0) {
        setImported((count) => count + newlyImported);
        reread();
      }
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const changeSheetPlan = (sheetIndex: number, type: string) => {
    const chosen = allPlans.find((plan) => plan.type === type);
    if (!chosen) return;
    setSheets((current) => current.map((sheet, idx) => (idx === sheetIndex ? {
      ...sheet,
      plan: chosen,
      mapping: autoMapColumns(sheet.columns, chosen),
      lines: sheet.lines.map((line) => ({ ...line, result: undefined, baseId: undefined })),
    } : sheet)));
  };

  const changeMapping = (sheetIndex: number, colIndex: number, value: string) => {
    setSheets((current) => current.map((sheet, idx) => (idx === sheetIndex ? {
      ...sheet,
      mapping: sheet.mapping.map((name, at) => (at === colIndex ? value : name === value && value !== skip ? skip : name)),
      lines: sheet.lines.map((line) => (line.done ? line : { ...line, result: undefined })),
    } : sheet)));
  };

  const editCell = (sheetIndex: number, row: number, colIndex: number, value: string) => {
    setSheets((current) => current.map((sheet, idx) => (idx === sheetIndex ? {
      ...sheet,
      lines: sheet.lines.map((line) => (line.row === row ? {
        ...line,
        result: undefined,
        cells: line.cells.map((cell, at) => (at === colIndex ? value : cell)),
      } : line)),
    } : sheet)));
  };

  const blamed = (line: PlanLine) => {
    const outcome = line.result?.outcome ?? "";
    return new Set([
      ...(outcome.startsWith(unreadable) ? [outcome.slice(unreadable.length)] : []),
      ...(line.result?.error?.issues ?? []).map((issue) => issue.path[0] ?? ""),
    ]);
  };

  const problemOf = (plan: ObjectPlan, line: PlanLine) => {
    if (!line.result) return t("Not checked yet");
    if (line.result.outcome.startsWith(unreadable)) {
      return `${t("This value cannot be read")}: ${titleInPlan(plan, line.result.outcome.slice(unreadable.length))}`;
    }
    return line.result.error?.message || t("Not saved");
  };

  const next = () => {
    if (step === 1) void run(true);
    setStep((current) => current + 1);
  };

  const problemGroups = sheets.flatMap((sheet, sheetIndex) => {
    const sheetProblems = sheet.lines.filter((line) => !line.done && line.result?.outcome !== "ok");
    return sheetProblems.length > 0 ? [{ sheetIndex, sheet, lines: sheetProblems }] : [];
  });

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={880} purpose="form">
      <Layout
        height="fill"
        padding={4}
        defaultHasDividers
        header={
          <>
            <DialogHeader
              title={`${t("Import")} · ${multiSheet ? t("Workbook") : t(primaryPlan.title)}`}
              onOpenChange={(open) => !open && onClose()}
              hasDivider={false}
            />
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
                <FileInput
                  label={t("File")}
                  description={t("A CSV file or an Excel workbook; the first row names the columns.")}
                  accept=".csv,.xlsx,text/csv"
                  placeholder={t("Choose file")}
                  mode="dropzone"
                  value={file}
                  isLoading={busy}
                  onChange={(chosen) => void read(chosen instanceof File ? chosen : null)}
                />
              )}
              {step === 0 && sheets.length > 0 && (
                <VStack gap={1}>
                  <Text type="supporting">{`${totalLines} ${t("records")}`}</Text>
                  {multiSheet && (
                    <Text type="supporting" color="secondary">
                      {sheets.map((sheet) => `${sheet.name} → ${t(sheet.plan.title)} (${sheet.lines.length})`).join(" · ")}
                    </Text>
                  )}
                </VStack>
              )}
              {step === 1 && currentSheet && (
                <VStack gap={4}>
                  {multiSheet && (
                    <HStack gap={3} vAlign="end" wrap="wrap">
                      <SegmentedControl
                        label={t("Worksheets")}
                        value={String(activeSheet)}
                        onChange={(value) => setActiveSheet(Number(value))}
                      >
                        {sheets.map((sheet, idx) => (
                          <SegmentedControlItem key={`${idx}:${sheet.name}`} value={String(idx)} label={`${sheet.name} (${sheet.lines.length})`} />
                        ))}
                      </SegmentedControl>
                      <StackItem size="fill" />
                      <Selector
                        label={t("Target object")}
                        size="sm"
                        value={currentSheet.plan.type}
                        options={allPlans.map((plan) => ({ value: plan.type, label: t(plan.title) }))}
                        onChange={(value) => changeSheetPlan(activeSheet, value)}
                      />
                    </HStack>
                  )}
                  <Table aria-label={t("Columns")} density="compact">
                    <TableHeader>
                      <TableRow>
                        <TableHeaderCell>{t("Columns")}</TableHeaderCell>
                        <TableHeaderCell>{t("Fields")}</TableHeaderCell>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {currentSheet.columns.map((column, index) => (
                        <TableRow key={`${activeSheet}:${index}:${column}`}>
                          <TableCell>
                            <VStack gap={0}>
                              <Text>{column}</Text>
                              <Text type="supporting">{currentSheet.lines.find((line) => line.cells[index])?.cells[index] ?? ""}</Text>
                            </VStack>
                          </TableCell>
                          <TableCell>
                            <Selector
                              label={`${t("Fields")}: ${column}`}
                              isLabelHidden
                              size="sm"
                              options={optionsOf(currentSheet.plan)}
                              value={currentSheet.mapping[index]}
                              onChange={(value) => changeMapping(activeSheet, index, value)}
                            />
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </VStack>
              )}
              {step === 2 && (
                <VStack gap={4}>
                  {imported > 0 && <Banner status="success" title={`${imported} ${t("records imported")}`} />}
                  {busy && <Text type="supporting">{t("Checking…")}</Text>}
                  {!busy && checked && allPending.length > 0 && (
                    <Text>{`${ready} ${t("can be imported")} · ${problems.length} ${t("need fixing")}`}</Text>
                  )}
                  {problemGroups.map(({ sheetIndex, sheet, lines: badLines }) => {
                    const used = usedOf(sheet);
                    return (
                      <VStack key={`${sheetIndex}:${sheet.name}`} gap={2}>
                        {multiSheet && <Text weight="semibold">{`${sheet.name} · ${t(sheet.plan.title)}`}</Text>}
                        <Table aria-label={multiSheet ? `${sheet.name} · ${t("need fixing")}` : t("need fixing")} density="compact">
                          <TableHeader>
                            <TableRow>
                              <TableHeaderCell>{t("Row")}</TableHeaderCell>
                              {used.map(({ name }) => <TableHeaderCell key={name}>{titleInPlan(sheet.plan, name)}</TableHeaderCell>)}
                              <TableHeaderCell>{t("Outcome")}</TableHeaderCell>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {badLines.slice(0, shown).map((line) => {
                              const wrong = blamed(line);
                              const rowContext = multiSheet ? `${sheet.name} · ${t("Row")} ${line.row}` : `${t("Row")} ${line.row}`;
                              return (
                                <TableRow key={line.row}>
                                  <TableCell>{line.row}</TableCell>
                                  {used.map(({ name, index }) => (
                                    <TableCell key={name}>
                                      <TextInput
                                        label={`${titleInPlan(sheet.plan, name)} · ${rowContext}`}
                                        isLabelHidden
                                        size="sm"
                                        value={line.cells[index] ?? ""}
                                        status={wrong.has(name) ? { type: "error" } : undefined}
                                        onChange={(value) => editCell(sheetIndex, line.row, index, value)}
                                      />
                                    </TableCell>
                                  ))}
                                  <TableCell>
                                    <HStack gap={2} vAlign="center">
                                      <StatusDot variant={line.result ? "error" : "neutral"} label={line.result ? t("Not saved") : t("Not checked yet")} />
                                      <Text>{problemOf(sheet.plan, line)}</Text>
                                    </HStack>
                                  </TableCell>
                                </TableRow>
                              );
                            })}
                          </TableBody>
                        </Table>
                        {shown < badLines.length && <Text type="supporting">{`${shown} / ${badLines.length}`}</Text>}
                      </VStack>
                    );
                  })}
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
              {step < 2 && (
                <Button
                  label={t("Continue")}
                  variant="primary"
                  isDisabled={busy || sheets.length === 0 || (step === 1 && !anyMapped)}
                  onClick={next}
                />
              )}
              {step === 2 && allPending.length > 0 && (
                <Button label={t("Check")} variant="secondary" isDisabled={busy || checked} onClick={() => void run(true)} />
              )}
              {step === 2 && allPending.length > 0 && (
                <Button label={`${t("Import")} ${ready}`} variant="primary" isDisabled={busy || !checked || !ready} onClick={() => void run(false)} />
              )}
              {step === 2 && allPending.length === 0 && <Button label={t("Done")} variant="primary" onClick={onClose} />}
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
