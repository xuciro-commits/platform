import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Drawer, DrawerHeader } from "@astryxdesign/core/Drawer";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { createContext, Fragment, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { describe, withRestrictedFields } from "./describe";
import { useOptions } from "./options";
import { enterable } from "./ParameterInput";
import { PropertyField } from "./PropertyField";
import { FieldGroup, useEditor } from "./RecordFields";
import { dots, Transitions } from "./Transitions";
import { plain } from "./values";

export type PeekTarget = { type: string; id: string; name?: string };
export type PeekSurfaceMode = "drawer" | "panel";
type PeekEntry = PeekTarget & { key: number; open: boolean };
type PeekContextValue = { open: (target: PeekTarget) => void; close: () => void };

const PeekContext = createContext<PeekContextValue | null>(null);

export function usePeek() {
  return useContext(PeekContext);
}

export function PeekProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<PeekEntry[]>([]);
  const nextKey = useRef(1);
  const value = useMemo<PeekContextValue>(() => ({
    open: (target) => setEntries((previous) => {
      const alive = previous.filter((entry) => entry.open);
      const existing = alive.findIndex((entry) => entry.type === target.type && entry.id === target.id);
      if (existing >= 0) return alive.slice(0, existing + 1);
      return [...alive, { ...target, key: nextKey.current++, open: true }];
    }),
    close: () => setEntries((previous) => previous.map((entry, index) => index === previous.length - 1 ? { ...entry, open: false } : entry)),
  }), []);
  return (
    <PeekContext.Provider value={value}>
      {children}
      {entries.map((entry) => (
        <RecordPeek
          key={entry.key}
          type={entry.type}
          id={entry.id}
          name={entry.name}
          isOpen={entry.open}
          onOpenChange={(open) => {
            if (!open) setEntries((previous) => previous.map((item) => item.key === entry.key ? { ...item, open: false } : item));
          }}
        />
      ))}
    </PeekContext.Provider>
  );
}

type PeekDrawerProps = {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  label: string;
  title: ReactNode;
  subtitle?: ReactNode;
  endContent?: ReactNode;
  hasScrim?: boolean;
  surface?: PeekSurfaceMode;
  footer?: ReactNode;
  children: ReactNode;
};

export function PeekDrawer({ isOpen, onOpenChange, label, title, subtitle, endContent, hasScrim = true, surface = "drawer", footer, children }: PeekDrawerProps) {
  useEffect(() => {
    if (surface !== "panel" || !isOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      if (document.querySelector("dialog[open], [role=alertdialog]") || document.activeElement?.closest("[role=listbox], [role=menu], [aria-expanded=true]")) return;
      onOpenChange(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [surface, isOpen, onOpenChange]);

  const inner = (
    <Layout
      height="fill"
      padding={surface === "panel" ? 4 : undefined}
      header={<DrawerHeader title={title} subtitle={subtitle} endContent={endContent} onOpenChange={onOpenChange} />}
      content={<LayoutContent>{children}</LayoutContent>}
      footer={footer ? <LayoutFooter>{footer}</LayoutFooter> : undefined}
    />
  );

  if (surface === "panel") {
    if (!isOpen) return null;
    return (
      <LayoutPanel width={440} hasDivider padding={0} isScrollable={false} role="dialog" label={label}>
        {inner}
      </LayoutPanel>
    );
  }
  return (
    <Drawer isOpen={isOpen} onOpenChange={onOpenChange} label={label} width={440} hasScrim={hasScrim}>
      {inner}
    </Drawer>
  );
}

type Kept = { id: string; revision: number; changed?: { at?: string }; archived?: boolean };

function Bullet() {
  return <Text type="supporting" color="secondary">{"・"}</Text>;
}

function RecordPeekFields({ described, detail, id, values, kept, refs, restricted, allowed, heading, sections, options, parameters, title, onFailure }: {
  described: ReturnType<typeof describe>;
  detail: ReturnType<typeof describe>;
  id: string;
  values: Record<string, unknown>;
  kept: Kept;
  refs?: Api.RecordView["refs"];
  restricted: ReadonlySet<string>;
  allowed?: Api.RecordAction[];
  heading: Api.FieldInfo[];
  sections: { title: string; fields: Api.FieldInfo[] }[];
  options: ReturnType<typeof useOptions>;
  parameters: Map<string, Api.Field>;
  title: string;
  onFailure: (reason: string) => void;
}) {
  const editor = useEditor(described, id, values, kept.revision);
  return (
    <>
      <Transitions
        described={described}
        id={id}
        values={values}
        revision={kept.revision}
        title={title}
        allowed={allowed}
        onFailure={onFailure}
      />
      {heading.length > 0 && (
        <HStack gap={2} vAlign="center" wrap="wrap">
          {heading.map((field) => {
            const value = editor.values[field.name];
            const parameter = parameters.get(field.name);
            return (
              <Fragment key={field.name}>
                <PropertyField
                  field={field}
                  parameter={parameter}
                  described={described.entity}
                  value={value}
                  form={editor.values}
                  name={refs?.[field.name]?.[String(value ?? "")]}
                  dot={options[field.name]?.find((option) => option.value === String(value ?? ""))?.dot}
                  status={editor.status[field.name]}
                  saving={editor.saving.has(field.name)}
                  labelHidden
                  hint={t(field.title)}
                  onChange={parameter && ((next: unknown) => editor.change(parameter, next))}
                  onCommit={() => editor.commit(field.name)}
                />
              </Fragment>
            );
          })}
        </HStack>
      )}
      <FieldGroup described={detail} editor={editor} sections={sections} direction="vertical" fold refs={refs} restricted={restricted} />
    </>
  );
}

export function RecordPeek({ type, id, name, isOpen, onOpenChange, hasScrim = true, surface = "drawer", label }: {
  type?: string;
  id?: string;
  name?: string;
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  hasScrim?: boolean;
  surface?: PeekSurfaceMode;
  label?: string;
}) {
  const [keptTarget, setKeptTarget] = useState<PeekTarget>();
  if (isOpen && type && id && (keptTarget?.type !== type || keptTarget?.id !== id || keptTarget?.name !== name)) {
    setKeptTarget({ type, id, name });
  }
  const active = isOpen && type && id ? { type, id, name } : keptTarget;
  const [refusal, setRefusal] = useState("");
  const activeKey = active ? `${active.type}:${active.id}` : "";
  useEffect(() => {
    setRefusal("");
  }, [activeKey]);

  const href = useRecordHref();
  const types = useRead<Api.EntityInfo[]>(isOpen && active ? "/v1/entities" : null);
  const actions = useRead<Api.Action[]>(isOpen && active ? "/v1/actions" : null);
  const read = useRead<Api.RecordView>(isOpen && active ? `/v1/records/${encodeURIComponent(active.type)}/${encodeURIComponent(active.id)}` : null);
  const entity = active && types.status === "ready" ? types.data.find((candidate) => candidate.type === active.type) : undefined;
  const described = useMemo(() => (entity && actions.status === "ready" ? describe(entity, actions.data) : undefined), [entity, actions]);
  const values = useMemo(
    () => (read.status === "ready" ? Object.fromEntries(Object.entries(read.data.values ?? {}).map(([key, value]) => [key, plain(value)])) : {}),
    [read],
  );
  const kept = read.status === "ready" ? (read.data.record as Kept) : undefined;
  const refs = read.status === "ready" ? read.data.refs : undefined;
  const restrictedFields = read.status === "ready" ? read.data.restrictedFields : undefined;
  const restricted = new Set(read.status === "ready" ? read.data.restricted : undefined);
  const detail = useMemo(
    () => (described && restrictedFields?.length ? describe(withRestrictedFields(described.entity, restrictedFields), actions.status === "ready" ? actions.data : []) : described),
    [actions, described, restrictedFields],
  );
  const state = described?.state(values);
  const laneKey = (described?.entity.view as { lanes?: string } | undefined)?.lanes;
  const laneField = (laneKey ? described?.field(laneKey) : undefined) ?? described?.lanes[0];
  const ownerField = described?.entity.fields.find((field) => field.type === "member");
  const heading = kept ? [laneField, ownerField].flatMap((field) => field && !restricted.has(field.name) ? [field] : []) : [];
  const headed = heading.map((field) => field.name).join(",");
  const body = useMemo(() => {
    if (!detail) return undefined;
    const rest = (fields: Api.FieldInfo[]) => fields.filter((field) => !headed.split(",").includes(field.name));
    return { ...detail, aside: rest(detail.aside), sections: detail.sections.map((section) => ({ ...section, fields: rest(section.fields) })) };
  }, [detail, headed]);
  const options = useOptions(described, laneField ? [laneField.name] : []);
  const parameters = new Map((described?.edit?.payload ?? []).filter(enterable).map((parameter) => [parameter.name, parameter]));
  const status = kept?.archived ? { tone: "neutral", title: "Archived" } : laneField && (described?.lifecycle?.states?.length ?? 0) <= 1 ? undefined : state;

  if (!active) return null;
  const title = described && kept ? described.name(values, active.id) : active.name || active.id;
  const fullHref = href(active.type, active.id);
  const sections = body ? [...body.sections, ...(body.aside.length ? [{ title: "Properties", fields: body.aside }] : [])] : [];
  const subtitle = (
    <HStack gap={1} vAlign="center" wrap="wrap">
      <Text type="code" color="secondary">{active.id}</Text>
      {status && (
        <>
          <Bullet />
          <HStack gap={2} vAlign="center">
            <StatusDot variant={dots[(status.tone ?? "neutral") as keyof typeof dots] ?? dots.neutral} label={t(status.title)} />
            <Text type="supporting">{t(status.title)}</Text>
          </HStack>
        </>
      )}
      {kept?.changed?.at && (
        <>
          <Bullet />
          <Text type="supporting" color="secondary">{t("Updated")}</Text>
          <Timestamp value={kept.changed.at} format="auto" />
        </>
      )}
    </HStack>
  );
  const loading = isOpen && (read.status === "loading" || types.status === "loading" || actions.status === "loading");
  const failed = read.status === "error" ? read.reason : types.status === "error" ? types.reason : actions.status === "error" ? actions.reason : "";
  const refused = read.status === "refused" ? read.reason : "";

  return (
    <PeekDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      label={label ?? title}
      title={title}
      subtitle={subtitle}
      hasScrim={hasScrim}
      surface={surface}
      footer={
        <HStack gap={2} hAlign="end">
          <Button label={t("Open full page")} variant="secondary" href={fullHref} onClick={() => onOpenChange(false)} />
        </HStack>
      }
    >
      <VStack gap={4}>
        {loading && (
          <VStack gap={2}>
            {[0, 1, 2, 3].map((index) => <Skeleton key={index} height={28} index={index} />)}
          </VStack>
        )}
        {failed && <Banner status="error" title={t("This record could not be loaded")} description={failed || t("Reload the page to try again.")} />}
        {refused && <EmptyState headingLevel={3} title={t("You cannot see this record")} description={refused || t("Ask an administrator of this workspace for access.")} />}
        {refusal && <Banner status="error" title={t("Not done")} description={refusal} />}
        {kept && described && detail && body && (
          <RecordPeekFields
            key={activeKey}
            described={described}
            detail={detail}
            id={active.id}
            values={values}
            kept={kept}
            refs={refs}
            restricted={restricted}
            allowed={read.status === "ready" ? read.data.actions : undefined}
            heading={heading}
            sections={sections}
            options={options}
            parameters={parameters}
            title={title}
            onFailure={setRefusal}
          />
        )}
      </VStack>
    </PeekDrawer>
  );
}
