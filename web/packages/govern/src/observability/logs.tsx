import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import {
  Button, DataTable, EmptyState, Input, Notice, PageHeader, PropertyList, Select, Sheet, StatusTag, Switch, Tag,
  defineStatuses, formatStamp, humanizeKernelError, notify, t, type ColumnDef,
} from "@platform/ui";
import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

type Observation = Api.Observation;
type ObservationPage = Api.ObservationPage;
type ObservationCoverage = Api.ObservationLogCoverage;

type RangeKey = "15m" | "1h" | "24h" | "7d" | "30d";
type AppliedRange = { kind: RangeKey } | { kind: "custom"; from: string; to: string };
type TimeWindow = { from: string; to: string };
type FrozenLogs = { rows: Observation[]; coverage: ObservationCoverage; ids: Set<string>; newIds: Set<string> };

const ranges: { id: RangeKey; duration: number }[] = [
  { id: "15m", duration: 15 * 60_000 },
  { id: "1h", duration: 60 * 60_000 },
  { id: "24h", duration: 24 * 60 * 60_000 },
  { id: "7d", duration: 7 * 24 * 60 * 60_000 },
  { id: "30d", duration: 30 * 24 * 60 * 60_000 },
];

const logLevels = defineStatuses({
  debug: { label: "Debug", tone: "neutral" },
  info: { label: "Info", tone: "info" },
  warn: { label: "Warning", tone: "warning" },
  error: { label: "Error", tone: "danger" },
});

const sourceLabels: Record<string, string> = {
  request: "Request",
  submit: "Submission",
  run: "Run",
  worker: "Worker",
  data: "Data",
  app: "Application",
};

const outcomeLabels: Record<string, string> = {
  ok: "OK",
  refused: "Refused",
  failed: "Failed",
  pending: "Pending",
};

const localDateTime = (value: string | Date) => {
  const at = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(at.getTime())) return "";
  return new Date(at.getTime() - at.getTimezoneOffset() * 60_000).toISOString().slice(0, 19);
};

const parseLocalDateTime = (value: string) => {
  if (!value) return undefined;
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? undefined : at;
};

function presetWindow(kind: RangeKey, anchor = Date.now()): TimeWindow {
  const duration = ranges.find((range) => range.id === kind)?.duration ?? 60 * 60_000;
  return { from: new Date(anchor - duration).toISOString(), to: new Date(anchor).toISOString() };
}

function logsPath(window: TimeWindow, filters: { level: string; source: string; app: string; q: string }, cursor?: string) {
  const search = new URLSearchParams({ from: window.from, to: window.to, limit: "100" });
  if (filters.level) search.set("level", filters.level);
  if (filters.source) search.set("source", filters.source);
  if (filters.app) search.set("app", filters.app);
  if (filters.q) search.set("q", filters.q);
  if (cursor) search.set("cursor", cursor);
  return `/v1/monitoring/logs?${search.toString()}`;
}

function mergedRows(page: ObservationPage | undefined, tail: ObservationPage[]) {
  const rows = new Map<string, Observation>();
  for (const entry of [page, ...tail].flatMap((part) => part?.entries ?? [])) if (!rows.has(entry.id)) rows.set(entry.id, entry);
  return [...rows.values()];
}

const errorMessage = (error: unknown) => humanizeKernelError(error instanceof Error ? error.message : String(error));

function CopyValue({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const timeout = useRef<number | undefined>(undefined);
  useEffect(() => () => { if (timeout.current !== undefined) window.clearTimeout(timeout.current); }, []);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      if (timeout.current !== undefined) window.clearTimeout(timeout.current);
      timeout.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      notify.error(t("Copy failed"));
    }
  };
  return <span className="flex min-w-0 items-start gap-1">
    <code className="min-w-0 break-all font-mono text-xs">{value}</code>
    <Button size="icon" variant="ghost" aria-label={copied ? t("Copied") : t("Copy {field}", { field: label })} title={copied ? t("Copied") : t("Copy {field}", { field: label })} onClick={() => void copy()}>
      {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
    </Button>
  </span>;
}

function LogDetails({ entry, onClose }: { entry: Observation; onClose: () => void }) {
  const { me } = useHost();
  const associations: [string, string | undefined][] = [
    [t("Request ID"), entry.requestId],
    [t("Trace ID"), entry.traceId],
    [t("Span ID"), entry.spanId],
    [t("Run ID"), entry.runId],
    [t("Resource"), entry.resource],
    [t("Submission ID"), entry.submissionId],
    [t("Change ID"), entry.changeId],
  ];
  const attributes: [string, ReactNode][] = Object.entries(entry.attributes ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => [key, <span className="break-all font-mono text-xs">{String(value)}</span>]);
  const associationItems: [string, ReactNode][] = associations.map(([label, value]) => [label, value
    ? <CopyValue value={value} label={label} />
    : <span className="text-muted">—</span>]);
  return <Sheet isolated width={560} open onOpenChange={(open) => { if (!open) onClose(); }} title={t("Log event")}>
    <div className="grid min-w-0 gap-4 text-sm">
      <PropertyList columns={2} items={[
        [t("Event ID"), <CopyValue value={entry.id} label={t("Event ID")} />],
        [t("Time"), formatStamp(entry.at)],
        [t("Level"), <StatusTag status={entry.level} registry={logLevels} />],
        [t("Source"), <Tag label={t(sourceLabels[entry.source] ?? entry.source)} />],
        [t("Outcome"), entry.outcome ? <Tag label={t(outcomeLabels[entry.outcome] ?? entry.outcome)} /> : "—"],
        [t("Tenant"), entry.tenant],
        [t("Member"), entry.member ? <CopyValue value={entry.member} label={t("Member")} /> : "—"],
        [t("Application"), entry.app ? <span className="grid min-w-0 gap-0.5">
          <span className="break-words">{me.apps.find((app) => app.id === entry.app)?.title ?? entry.app}</span>
          {me.apps.some((app) => app.id === entry.app) && <code className="break-all font-mono text-xs text-muted">{entry.app}</code>}
        </span> : "—"],
        [t("Route"), entry.route ? <CopyValue value={entry.route} label={t("Route")} /> : "—"],
      ]} />
      <div className="grid gap-1">
        <span className="text-xs font-medium text-muted">{t("Message")}</span>
        <p className="whitespace-pre-wrap break-words">{entry.message}</p>
      </div>
      <div className="grid gap-2">
        <h2 className="text-sm font-semibold">{t("Related")}</h2>
        <PropertyList items={associationItems} />
      </div>
      <div className="grid gap-2">
        <h2 className="text-sm font-semibold">{t("Attributes")}</h2>
        {Object.keys(entry.attributes ?? {}).length === 0
          ? <p className="text-xs text-muted">{t("No attributes recorded.")}</p>
          : <PropertyList items={attributes} />}
      </div>
    </div>
  </Sheet>;
}

function LogPolicySheet({ onClose }: { onClose: () => void }) {
  const { me, decide } = useHost();
  const tenant = useReadQuery<Api.TenantRecord>("/v1/tenant");
  const currentSettings = tenant.data?.settings ?? me.tenant.settings;
  const setting = (name: string, fallback: string) => currentSettings[name] ?? fallback;
  const [level, setLevel] = useState(() => setting("logLevel", "info"));
  const [requests, setRequests] = useState(() => setting("logRequests", "10"));
  const [retention, setRetention] = useState(() => setting("logRetentionDays", "30"));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const initial = useRef({ level: setting("logLevel", "info"), requests: setting("logRequests", "10"), retention: setting("logRetentionDays", "30") });
  const changes = [
    { name: "logLevel", value: level, before: initial.current.level },
    { name: "logRequests", value: requests, before: initial.current.requests },
    { name: "logRetentionDays", value: retention, before: initial.current.retention },
  ].filter((change) => change.value !== change.before);
  const update = (settings: Record<string, string>) => {
    const values = {
      level: settings.logLevel ?? "info",
      requests: settings.logRequests ?? "10",
      retention: settings.logRetentionDays ?? "30",
    };
    setLevel(values.level);
    setRequests(values.requests);
    setRetention(values.retention);
    initial.current = values;
  };
  const save = async () => {
    const percent = Number(requests);
    const days = Number(retention);
    if (!Number.isSafeInteger(percent) || percent < 0 || percent > 100) {
      setError(t("Enter a percentage from 0 to 100."));
      return;
    }
    if (!Number.isSafeInteger(days) || days < 0 || days > 3650) {
      setError(t("Enter retention days from 0 to 3650."));
      return;
    }
    setSaving(true);
    setError("");
    let failure = "";
    try {
      for (const change of changes) {
        const accepted = await decide("platform.setting.set", { type: "platform.setting", id: `platform/${change.name}` }, { value: change.value }, {
          quiet: true,
          onRefused: (reason) => { failure = humanizeKernelError(reason); },
        });
        if (!accepted) {
          failure ||= t("Log policy could not be saved.");
          break;
        }
      }
      const refreshed = await tenant.refetch();
      if (refreshed.data) update(refreshed.data.settings);
      if (failure) {
        setError(refreshed.error ? `${failure} ${t("Tenant settings could not be refreshed.")}` : failure);
        return;
      }
      if (refreshed.error) {
        setError(t("Log policy was saved, but tenant settings could not be refreshed."));
        return;
      }
      notify.success(t("Log policy saved"));
      onClose();
    } catch (problem) {
      setError(errorMessage(problem));
    } finally {
      setSaving(false);
    }
  };
  return <Sheet isolated width={460} open onOpenChange={(open) => { if (!open && !saving) onClose(); }} title={t("Log policy")}>
    <div className="grid gap-4 text-sm">
      <p className="text-xs text-muted">{t("Changes affect new log capture. Retention removes eligible older logs at its next cleanup. Host limits still apply.")}</p>
      {error && <Notice tone="danger" title={t("Log policy")} message={error} />}
      <label className="grid gap-1 text-xs">{t("Application log level")}
        <Select aria-label={t("Application log level")} value={level} onChange={(event) => setLevel(event.target.value)}>
          {level && !["debug", "info", "warn", "error"].includes(level) && <option value={level}>{level}</option>}
          {Object.entries(logLevels).map(([value, status]) => <option key={value} value={value}>{status.label}</option>)}
        </Select>
      </label>
      <label className="grid gap-1 text-xs">{t("Request log sample (%)")}
        <Input aria-label={t("Request log sample (%)")} type="number" min={0} max={100} step={1} value={requests} onChange={(event) => setRequests(event.target.value)} />
      </label>
      <p className="text-xs text-muted">{t("Failures and requests taking one second or more are always recorded; this percentage applies to faster successful requests.")}</p>
      <label className="grid gap-1 text-xs">{t("Retention days")}
        <Input aria-label={t("Retention days")} type="number" min={0} max={3650} step={1} value={retention} onChange={(event) => setRetention(event.target.value)} />
      </label>
      <p className="text-xs text-muted">{t("0 stops new logs without deleting existing logs.")}</p>
      <p className="text-xs text-muted">{t("Settings are saved separately; a failure stops the remaining changes.")}</p>
      <div className="flex justify-end gap-2">
        <Button disabled={saving} onClick={onClose}>{t("Cancel")}</Button>
        <Button variant="primary" loading={saving} disabled={changes.length === 0} onClick={() => void save()}>{t("Save policy")}</Button>
      </div>
    </div>
  </Sheet>;
}

export function MonitoringLogsView() {
  const host = useHost();
  const [range, setRange] = useState<AppliedRange>({ kind: "1h" });
  const [anchor, setAnchor] = useState(() => Date.now());
  const [customOpen, setCustomOpen] = useState(false);
  const [customFrom, setCustomFrom] = useState(() => localDateTime(new Date(Date.now() - 60 * 60_000)));
  const [customTo, setCustomTo] = useState(() => localDateTime(new Date()));
  const [customError, setCustomError] = useState("");
  const [level, setLevel] = useState("");
  const [source, setSource] = useState("");
  const [app, setApp] = useState("");
  const [searchDraft, setSearchDraft] = useState("");
  const [search, setSearch] = useState("");
  const [live, setLive] = useState(false);
  const [paused, setPaused] = useState<FrozenLogs | null>(null);
  const [lastHead, setLastHead] = useState<{ scope: string; page: ObservationPage } | null>(null);
  const [tailState, setTailState] = useState<{ scope: string; pages: ObservationPage[] }>({ scope: "", pages: [] });
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [selected, setSelected] = useState<Observation>();
  const [policyOpen, setPolicyOpen] = useState(false);
  const timeWindow = range.kind === "custom" ? { from: range.from, to: range.to } : presetWindow(range.kind, anchor);
  const filters = { level, source, app, q: search };
  const scope = JSON.stringify([range, level, source, app, search]);
  const query = useReadQuery<ObservationPage>(logsPath(timeWindow, filters));
  const tail = tailState.scope === scope ? tailState.pages : [];
  const current = query.data ?? (lastHead?.scope === scope ? lastHead.page : undefined);
  const rows = mergedRows(current, tail);
  const coverage = paused?.coverage ?? current?.coverage;
  const visibleRows = paused?.rows ?? rows;
  const lastPage = tail.at(-1) ?? current;
  const canLoadMore = !live && !!lastPage && lastPage.hasMore && !!lastPage.nextCursor && !loadingMore && !query.isError;
  const textError = query.isError ? errorMessage(query.error) : "";
  const denied = /HTTP 403/.test(textError);
  const selectedTimeFrom = new Date(timeWindow.from).getTime();
  const retainedFrom = coverage?.retainedFrom ? new Date(coverage.retainedFrom).getTime() : undefined;
  const retentionBeforeRange = retainedFrom !== undefined && !Number.isNaN(retainedFrom) && retainedFrom > selectedTimeFrom;
  const rangeBeforeRetention = retainedFrom !== undefined && !Number.isNaN(retainedFrom) && retainedFrom >= new Date(timeWindow.to).getTime();
  const refresh = query.refetch;

  useEffect(() => {
    if (!query.data) return;
    setLastHead({ scope, page: query.data });
  }, [query.data, scope]);

  useEffect(() => {
    if (!query.data) return;
    setPaused((frozen) => {
      if (!frozen) return frozen;
      const fresh = query.data!.entries.filter((entry: Observation) => !frozen.ids.has(entry.id) && !frozen.newIds.has(entry.id));
      if (fresh.length === 0) return frozen;
      const next = new Set(frozen.newIds);
      for (const entry of fresh) {
        if (next.size >= 1000) break;
        next.add(entry.id);
      }
      return { ...frozen, newIds: next };
    });
  }, [query.data]);

  useEffect(() => {
    setPaused(null);
    setLoadError("");
    setTailState({ scope, pages: [] });
  }, [scope]);

  useEffect(() => {
    if (denied) setSelected(undefined);
  }, [denied]);

  useEffect(() => {
    if (!live) return;
    const timer = window.setInterval(() => {
      if (range.kind === "custom") void refresh();
      else setAnchor(Date.now());
    }, 2000);
    return () => window.clearInterval(timer);
  }, [live, range.kind, refresh]);

  const chooseRange = (kind: RangeKey) => {
    setRange({ kind });
    setAnchor(Date.now());
    setCustomOpen(false);
    setCustomError("");
  };

  const openCustom = () => {
    const currentWindow = timeWindow;
    setCustomFrom(localDateTime(currentWindow.from));
    setCustomTo(localDateTime(currentWindow.to));
    setCustomOpen(true);
    setCustomError("");
  };

  const applyCustom = () => {
    const from = parseLocalDateTime(customFrom);
    const to = parseLocalDateTime(customTo);
    if (!from || !to) {
      setCustomError(t("Enter a start and end time."));
      return;
    }
    const span = to.getTime() - from.getTime();
    if (span <= 0) {
      setCustomError(t("The end time must be after the start time."));
      return;
    }
    if (span > 30 * 24 * 60 * 60_000) {
      setCustomError(t("The maximum range is 30 days."));
      return;
    }
    setRange({ kind: "custom", from: from.toISOString(), to: to.toISOString() });
    setCustomError("");
  };

  const setLiveMode = (on: boolean) => {
    setLive(on);
    setPaused(null);
    setLoadError("");
    if (on) setTailState({ scope, pages: [] });
    else if (range.kind !== "custom") setAnchor(Date.now());
  };

  const pause = () => {
    if (!current) return;
    setPaused({
      rows: mergedRows(current, tail),
      coverage: current.coverage,
      ids: new Set(rows.map((entry) => entry.id)),
      newIds: new Set(),
    });
  };

  const loadMore = async () => {
    const cursor = lastPage?.nextCursor;
    if (!cursor || live) return;
    setLoadingMore(true);
    setLoadError("");
    try {
      const next = await host.client.get<ObservationPage>(logsPath(timeWindow, filters, cursor), true);
      setTailState((state) => ({ scope, pages: [...(state.scope === scope ? state.pages : []), next] }));
    } catch (problem) {
      setLoadError(errorMessage(problem));
    } finally {
      setLoadingMore(false);
    }
  };

  const columns: ColumnDef<Observation, any>[] = [
    { accessorKey: "at", header: t("Time"), meta: { width: 168 }, cell: ({ getValue }) => formatStamp(getValue()) },
    { accessorKey: "level", header: t("Level"), meta: { width: 96 }, cell: ({ row: { original } }) => <StatusTag status={original.level} registry={logLevels} /> },
    { accessorKey: "source", header: t("Source"), meta: { width: 108 }, cell: ({ row: { original } }) => t(sourceLabels[original.source] ?? original.source) },
    { accessorKey: "message", header: t("Message"), cell: ({ row: { original } }) => <span className="block truncate" title={original.message}>{original.message}</span> },
    { accessorKey: "app", header: t("Application"), meta: { width: 140 }, cell: ({ row: { original } }) => original.app ? host.me.apps.find((item) => item.id === original.app)?.title ?? original.app : "—" },
    {
      id: "related", header: t("Related"), meta: { width: 240 }, accessorFn: (entry) => [
        entry.runId && `${t("Run")} ${entry.runId}`,
        entry.requestId && `${t("Request")} ${entry.requestId}`,
        entry.traceId && `${t("Trace")} ${entry.traceId}`,
        entry.resource && `${t("Resource")} ${entry.resource}`,
        entry.submissionId && `${t("Submission")} ${entry.submissionId}`,
        entry.changeId && `${t("Change")} ${entry.changeId}`,
      ].filter(Boolean).join(" · "),
      cell: ({ getValue }) => <span className="block truncate font-mono text-2xs text-muted" title={getValue()}>{getValue() || "—"}</span>,
    },
  ];

  const hasFilters = !!level || !!source || !!app || !!search;
  const empty = rangeBeforeRetention
      ? { title: t("No logs are retained for this range"), hint: t("Retention starts at {time}. Choose a newer range.", { time: coverage?.retainedFrom ? formatStamp(coverage.retainedFrom) : t("Not reported") }) }
      : hasFilters
        ? { title: t("No logs match these filters"), hint: t("Widen the time range or clear the filters.") }
        : { title: t("No logs in this time range"), hint: t("Widen the time range.") };

  const coverageNotes = coverage ? [
    coverage.filtered && t("Access filters were applied to this result."),
    coverage.sampled && t("The result is sampled."),
    coverage.truncated && t("The result is truncated."),
    coverage.dropped > 0 && t("{count} events were dropped before retention.", { count: coverage.dropped.toLocaleString() }),
  ].filter(Boolean).join(" ") : "";

  return <>
    <PageHeader
      title={t("Logs")}
      help={t("Operational events for this tenant, newest first. Narrow by time, level, source and application, then open an event for its identifiers and attributes. Live polls every 2 seconds while this view is visible; Pause freezes the table and counts new events.")}
      actions={host.holds("platform", "admin") ? <Button onClick={() => setPolicyOpen(true)}>{t("Log policy")}</Button> : undefined}
    />
    <div className="mb-2 flex min-w-0 flex-wrap items-center gap-1 text-xs">
      <span className="mr-1 text-2xs font-semibold uppercase tracking-wider text-muted">{t("Time range")}</span>
      {ranges.map((option) => <Button key={option.id} size="sm" variant={!customOpen && range.kind === option.id ? "primary" : "ghost"}
        aria-pressed={!customOpen && range.kind === option.id} onClick={() => chooseRange(option.id)}>{t(option.id)}</Button>)}
      <Button size="sm" variant={customOpen ? "primary" : "ghost"} aria-pressed={customOpen} onClick={openCustom}>{t("Custom")}</Button>
    </div>
    {customOpen && <div className="mb-3 flex min-w-0 flex-wrap items-end gap-2 text-xs">
      <label className="grid min-w-0 gap-1">{t("Start")}<Input aria-label={t("Start")} type="datetime-local" step="1" className="w-52 max-w-full" value={customFrom} onChange={(event) => setCustomFrom(event.target.value)} /></label>
      <label className="grid min-w-0 gap-1">{t("End")}<Input aria-label={t("End")} type="datetime-local" step="1" className="w-52 max-w-full" value={customTo} onChange={(event) => setCustomTo(event.target.value)} /></label>
      <Button size="sm" onClick={applyCustom}>{t("Apply")}</Button>
      {customError && <p role="alert" className="text-xs text-danger">{customError}</p>}
    </div>}
    <div className="mb-3 flex min-w-0 flex-wrap items-end gap-2 text-xs">
      <label className="grid min-w-0 gap-1">{t("Level")}<Select aria-label={t("Level")} className="w-32" value={level} onChange={(event) => setLevel(event.target.value)}>
        <option value="">{t("All levels")}</option>
        {Object.entries(logLevels).map(([value, status]) => <option key={value} value={value}>{status.label}</option>)}
      </Select></label>
      <label className="grid min-w-0 gap-1">{t("Source")}<Select aria-label={t("Source")} className="w-40" value={source} onChange={(event) => setSource(event.target.value)}>
        <option value="">{t("All sources")}</option>
        {Object.entries(sourceLabels).map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}
      </Select></label>
      <label className="grid min-w-0 gap-1">{t("Application")}<Select aria-label={t("Application")} className="w-44" value={app} onChange={(event) => setApp(event.target.value)}>
        <option value="">{t("All applications")}</option>
        {host.me.apps.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}
      </Select></label>
      <label className="grid min-w-0 flex-[1_1_16rem] gap-1">{t("Search")}<Input aria-label={t("Search logs")} placeholder={t("Search log messages and indexed attributes")} value={searchDraft}
        onChange={(event) => setSearchDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.nativeEvent.isComposing) {
            event.preventDefault();
            setSearch(searchDraft.trim());
          }
        }} /></label>
      <Button size="sm" onClick={() => setSearch(searchDraft.trim())}>{t("Search")}</Button>
      <div className="ml-auto flex min-w-0 flex-wrap items-center gap-2">
        <Switch checked={live} onChange={setLiveMode} label={t("Live")} />
        {live && (paused
          ? <Button size="sm" onClick={() => setPaused(null)}>{t("Resume")}</Button>
          : <Button size="sm" disabled={!current} onClick={pause}>{t("Pause")}</Button>)}
        {paused && <Tag label={paused.newIds.size > 0 ? t("{count} new", { count: paused.newIds.size }) : t("Paused")} tone={paused.newIds.size > 0 ? "info" : "neutral"} />}
      </div>
    </div>
    {!denied && current && coverage && <div className="mb-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs" aria-label={t("Log coverage")}>
      <span><span className="text-muted">{t("Retained from")}:</span> {coverage.retainedFrom ? formatStamp(coverage.retainedFrom) : t("Not reported")}</span>
      <span><span className="text-muted">{t("Filtered")}:</span> {coverage.filtered ? t("Yes") : t("No")}</span>
      <span><span className="text-muted">{t("Sampled")}:</span> {coverage.sampled ? t("Yes") : t("No")}</span>
      <span><span className="text-muted">{t("Truncated")}:</span> {coverage.truncated ? t("Yes") : t("No")}</span>
      <span><span className="text-muted">{t("Dropped")}:</span> {coverage.dropped.toLocaleString()}</span>
    </div>}
    {!denied && retentionBeforeRange && <div className="mb-2"><Notice tone="warning" title={t("Retention starts later")} message={t("Events before {time} are no longer retained.", { time: coverage?.retainedFrom ? formatStamp(coverage.retainedFrom) : t("Not reported") })} /></div>}
    {!denied && coverage && coverageNotes && <div className="mb-2"><Notice tone="warning" title={t("Coverage limits")} message={coverageNotes} /></div>}
    {query.isError && !denied && current && <div className="mb-2 flex min-w-0 flex-wrap items-center gap-2">
      <Notice tone="warning" title={t("Refresh failed")} message={t("Showing the last successful result.")} />
      <Button size="sm" onClick={() => void query.refetch()}>{t("Retry")}</Button>
    </div>}
    {policyOpen && <LogPolicySheet onClose={() => setPolicyOpen(false)} />}
    {selected && <LogDetails entry={selected} onClose={() => setSelected(undefined)} />}
    {denied
      ? <EmptyState title={t("You do not have access to operational logs")} hint={t("Ask an administrator for monitoring log access. This does not show whether matching events exist.")} action={<Button size="sm" onClick={() => void query.refetch()}>{t("Retry")}</Button>} />
      : !current && query.isError
        ? <div className="grid justify-items-start gap-2 text-sm">
            <Notice tone="danger" title={t("Logs could not be loaded")} message={textError} />
            <Button size="sm" onClick={() => void query.refetch()}>{t("Retry")}</Button>
          </div>
        : <>
          <DataTable storageKey="monitoring:logs" data={visibleRows} columns={columns} getRowId={(entry) => entry.id} rowHeight={28}
            height="max(240px, calc(100dvh - 360px))" onRowClick={(entry) => setSelected(entry)} selectedId={selected?.id} searchable={false}
            loading={!current} loadingText={t("Loading logs…")} empty={{ ...empty, action: !rangeBeforeRetention && hasFilters ? <Button size="sm" onClick={() => { setLevel(""); setSource(""); setApp(""); setSearchDraft(""); setSearch(""); }}>{t("Clear filters")}</Button> : undefined }} />
          {(lastPage?.hasMore || loadingMore || loadError) && <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2">
            {live
              ? <span className="text-xs text-muted">{t("Pause Live to load older events.")}</span>
              : <Button size="sm" loading={loadingMore} disabled={!canLoadMore} onClick={() => void loadMore()}>{t("Load more")}</Button>}
            {loadError && <span role="alert" className="break-words text-xs text-danger">{loadError}</span>}
          </div>}
        </>}
  </>;
}
