import { Button } from "@astryxdesign/core/Button";
import { HStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { useEffect } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { type ListColumn, type ListShape, ListView } from "./ListPage";
import { Records } from "./RecordList";

type Directory = { resources: Api.ResourceEntry[] };
type Declared = Parameters<typeof Records>[0]["declared"];
type Catalog = { kind: string; title: string; about: string; author?: Declared; instances?: boolean };
type Scope = "business" | "platform" | "all";
type Row = {
  id: string; ref: Api.AssetRef; title: string; namespace: string; project: string; source: string;
  package: string; state: string; version: string; updated: string; href?: string;
  definition?: Api.Definition; canOpen: boolean; access: string;
  target?: string; sourceObject?: string; field?: string; inverse?: string; relationKind?: string;
};

const refKey = (ref: Api.AssetRef) => JSON.stringify([ref.app, ref.kind, ref.name]);
const namespace = (ref: Api.AssetRef) => ref.name.includes(".") ? ref.name.split(".")[0] : ref.app;
const options = (values: string[], translate = false) => [...new Set(values.filter(Boolean))].sort().map((value) => ({ value, label: translate ? t(value) : value }));
const titleOf = (def?: Api.Definition) => def?.entity?.title ?? def?.action?.title ?? def?.linkType?.title ?? def?.interface?.title ?? def?.propertyType?.title ?? def?.valueType?.title ?? def?.objectTypeGroup?.title;
const archived = (row: Api.ResourceEntry) => row.archived || row.state === "archived";

function catalogRows(resources: Api.ResourceEntry[], definitions: Api.Definition[], base: string, author?: Declared): Row[] {
  const registered = new Map(resources.map((row) => [refKey(row.ref), row]));
  const published = new Map(definitions.map((def) => [refKey(def.ref), def]));
  const projectNames = new Map(resources.filter((row) => row.ref.kind === "app" && row.draft).map((row) => [row.draft, row.title]));
  const keys = new Set([...registered.keys(), ...published.keys()]);
  return [...keys].flatMap((key) => {
    const resource = registered.get(key);
    const def = resource && !resource.canOpen ? undefined : published.get(key);
    if (resource && archived(resource)) return [];
    if (!resource && !def) return [];
    const ref = resource?.ref ?? def!.ref;
    const target = def?.action?.target;
    const parent = target ? registered.get(refKey({ app: ref.app, kind: "object", name: target })) : undefined;
    const generated = Boolean(def && def.source !== "code" && parent && (!resource || resource.managedBy === "platform" && !resource.draft));
    const owner = generated ? parent : resource;
    const managedBy = owner?.managedBy ?? def?.managedBy ?? "";
    const source = def?.source === "code" || managedBy === "platform" && !generated ? "Platform definition" : managedBy && managedBy !== "platform" ? "Package definition" : "Workspace definition";
    const project = owner?.project ?? def?.owningProject ?? "";
    const open = resource?.canOpen ?? Boolean(def);
    const href = open && author && resource?.type === author.type && resource.draft ? base + "/" + encodeURIComponent(resource.draft) : undefined;
    return [{
      id: key, ref, title: titleOf(def) || resource?.title || ref.name, namespace: namespace(ref),
      project: projectNames.get(project) ?? project, source, package: source === "Package definition" ? managedBy : "",
      state: resource?.state ?? (def ? "published" : ""), version: resource ? String(resource.version) : def?.version ?? "",
      updated: resource?.changed?.at ?? "", href, definition: def, canOpen: open,
      access: !open ? "Discover only" : resource?.canEdit && href ? "Editable definition" : "Read-only definition",
      target: target ?? def?.linkType?.parent.name, sourceObject: def?.linkType?.child.name,
      field: def?.linkType?.via, inverse: def?.linkType?.reverse, relationKind: ref.kind === "link-type" ? "Link type" : undefined,
    }];
  });
}

function relationships(rows: Row[]): Row[] {
  const explicit = new Set(rows.flatMap((row) => row.definition?.linkType?.via ? [JSON.stringify([row.definition.linkType.child.name, row.definition.linkType.via, row.definition.linkType.parent.name])] : []));
  return rows.filter((row) => row.ref.kind === "object").flatMap((row) => (row.definition?.entity?.fields ?? []).flatMap((field) => {
    if ((field.type !== "reference" && field.type !== "references") || !field.ref || explicit.has(JSON.stringify([row.ref.name, field.name, field.ref]))) return [];
    return [{ ...row, id: JSON.stringify([row.id, field.name]), title: field.title, href: undefined, definition: undefined,
      access: "Read-only definition", relationKind: "Reference field", sourceObject: row.ref.name, target: field.ref, field: field.name, inverse: field.inverse,
      ref: { ...row.ref, name: row.ref.name + "." + field.name }, state: "published", version: "", updated: "" }];
  }));
}

function CatalogList({ catalog, base, scope, relationshipView, changeScope, changeView }: {
  catalog: Catalog; base: string; scope: Scope; relationshipView: boolean; changeScope: (scope: Scope) => void; changeView: (mode: string) => void;
}) {
  const directory = useRead<Directory>("/v1/resources");
  const definitions = useRead<Api.Definition[]>("/v1/definitions");
  const actions = useRead<Api.Action[]>(catalog.author ? "/v1/actions" : null);
  const read: Read<Row[]> = directory.status !== "ready" ? directory : definitions.status !== "ready" ? definitions : (() => {
    const rows = catalogRows(directory.data.resources, definitions.data, base, catalog.author);
    const selected = rows.filter((row) => row.ref.kind === catalog.kind).concat(relationshipView ? relationships(rows) : []);
    const data = selected.filter((row) => (scope === "all" || (scope === "platform") === (row.source === "Platform definition")) && (!catalog.instances || row.definition?.entity));
    return { status: "ready", data };
  })();
  const rows = read.status === "ready" ? read.data : [];
  const column = (key: keyof Row, header: string, translate = false): ListColumn<Row> => ({
    key, header, text: (row) => translate ? t(String(row[key] ?? "")) : String(row[key] ?? ""), value: (row) => String(row[key] ?? ""), options: options(rows.map((row) => String(row[key] ?? "")), translate),
  });
  const columns: ListColumn<Row>[] = [
    { key: "title", header: catalog.author?.type === "build.object" ? "Label" : "Name", text: (row) => t(catalog.instances ? row.definition?.entity?.plural || row.title : row.title) },
    { key: "name", header: "API name", text: (row) => row.ref.name, kind: "code" },
    column("namespace", "Namespace"), column("project", "Project"), column("source", "Source", true),
    { key: "owner", header: "Owner", text: (row) => row.ref.app, options: options(rows.map((row) => row.ref.app)) },
    { ...column("package", "Package"), hiddenByDefault: true },
    { ...column("access", "Access", true), hiddenByDefault: true },
  ];
  if (catalog.kind === "action" || catalog.kind === "link-type") columns.splice(2, 0, { key: "target", header: "Target", text: (row) => row.target ?? "" });
  if (catalog.kind === "link-type") columns.splice(2, 0,
    { key: "relationKind", header: "Type", text: (row) => t(row.relationKind ?? ""), options: options(rows.map((row) => row.relationKind ?? ""), true), value: (row) => row.relationKind ?? "" },
    { key: "sourceObject", header: "Source object", text: (row) => row.sourceObject ?? "" },
    { key: "field", header: "Reference field", text: (row) => row.field ?? "" },
    { key: "inverse", header: "Inverse", text: (row) => row.inverse ?? "" });
  if (catalog.kind === "object") columns.push({ key: "properties", header: "Properties", kind: "number", text: (row) => row.definition?.entity ? String(row.definition.entity.fields.length) : "", value: (row) => row.definition?.entity?.fields.length });
  if (!catalog.instances) columns.push(
    { ...column("state", "State", true), dot: (row) => row.state === "active" || row.state === "published" ? "success" : row.state === "draft" ? "warning" : undefined },
    { key: "version", header: "Version", text: (row) => row.version },
    { key: "updated", header: "Updated", kind: "time", text: (row) => row.updated },
  );
  const shape: ListShape<Row[], Row> = {
    title: catalog.title, about: catalog.about, rows: (answer) => answer, id: (row) => row.id,
    link: (row) => catalog.instances ? base + "/" + encodeURIComponent(row.ref.name) : row.href ?? "",
    columns,
    groups: [
      { key: "namespace", label: "Namespace", of: (row) => row.namespace },
      { key: "project", label: "Project", of: (row) => row.project },
      { key: "source", label: "Source", of: (row) => t(row.source) },
      { key: "owner", label: "Owner", of: (row) => row.ref.app },
      ...(!catalog.instances ? [{ key: "state", label: "State", of: (row: Row) => t(row.state) }] : []),
      ...(catalog.kind === "link-type" ? [{ key: "relationKind", label: "Type", of: (row: Row) => t(row.relationKind ?? "") }, { key: "sourceObject", label: "Source object", of: (row: Row) => row.sourceObject ?? "" }] : []),
      ...(catalog.kind === "action" || catalog.kind === "link-type" ? [{ key: "target", label: "Target", of: (row: Row) => row.target ?? "" }] : []),
    ],
  };
  const canCreate = actions.status === "ready" && actions.data.some((action) => action.schema === catalog.author?.type + ".create");
  return <ListView definition={shape} read={read} primary={
    <HStack gap={2} vAlign="center" wrap="wrap">
      <SegmentedControl size="sm" label={t("Resource scope")} value={scope} onChange={(value) => changeScope(value as Scope)}>
        <SegmentedControlItem value="business" label={t("Business resources")} />
        <SegmentedControlItem value="platform" label={t("Platform resources")} />
        <SegmentedControlItem value="all" label={t("All resources")} />
      </SegmentedControl>
      {catalog.kind === "link-type" && <Button label={t(relationshipView ? "Link type definitions" : "Object relationships")} variant="secondary" onClick={() => changeView(relationshipView ? "links" : "")} />}
      {catalog.author && <Button label={t("Manage definitions")} variant="secondary" onClick={() => changeView("authoring")} />}
      {canCreate && <Button label={t("New")} variant="primary" onClick={() => changeView("create")} />}
    </HStack>
  } />;
}

export function ontologyCatalog(catalog: Catalog) {
  return Object.assign(function CatalogEntry() {
    const [, app, entry, id] = usePath().split("/");
    const base = "/" + app + "/" + entry;
    const params = new URLSearchParams(useSearch());
    const mode = params.get("mode");
    useEffect(() => {
      if (mode !== "create") return;
      const next = new URLSearchParams(location.search);
      next.set("mode", "authoring");
      void navigate(base + "?" + next, history.state, true);
    }, [base, mode]);
    const scope: Scope = params.get("scope") === "platform" ? "platform" : params.get("scope") === "all" ? "all" : "business";
    const changeScope = (next: Scope) => {
      params.set("scope", next);
      navigate(base + "?" + params);
    };
    const changeView = (next: string) => {
      const query = new URLSearchParams({ scope });
      if (next) query.set("mode", next);
      navigate(base + "?" + query);
    };
    if (catalog.author && (id || mode === "authoring" || mode === "create" || params.get("definitions") === "1")) {
      return <Records declared={catalog.author} listPath={base} id={id} initialCreate={mode === "create"}
        primaryExtra={<Button label={t("Resource catalog")} variant="secondary" onClick={() => changeView("")} />} />;
    }
    return <CatalogList catalog={catalog} base={base} scope={scope} relationshipView={catalog.kind === "link-type" && mode !== "links"} changeScope={changeScope} changeView={changeView} />;
  }, { recordType: catalog.author?.type });
}
