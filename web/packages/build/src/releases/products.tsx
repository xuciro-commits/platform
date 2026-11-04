import { useHost, useRecordInventory, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Checkbox, Dialog, Input, Notice, PageHeader, Panel, PropertyList, Tag, t, useWorkspace } from "@platform/ui";
import { useState } from "react";
import { kindOfType } from "../projects/resources";
import { applicationDraftSelection, ownerDraft, releaseDraftsValue, type JointChoice, type ReleaseKind } from "./selection";

type ProductRecord = {
  id: string; revision: number; name: string; title: string; description?: string;
  project: string; namespace?: string; roots: Api.AssetRef[]; requires?: string[]; archived?: boolean;
};
type ProductVersion = {
  id: string; product: string; version: string; candidate: string;
  candidateDigest?: string; contentDigest?: string; descriptor: Api.PackageDescriptor;
};
type Project = { id: string; name: string; title: string; archived?: boolean };
type Form = { name: string; title: string; description: string; project: string; namespace: string; roots: Api.AssetRef[]; requires: string };

const emptyForm: Form = { name: "", title: "", description: "", project: "", namespace: "", roots: [], requires: "" };

function draftKind(resource: Api.ResourceEntry): ReleaseKind | undefined {
  if (resource.type === "build.app") return "app";
  if (resource.type === "build.testplan") return resource.variant === "evaluation-set" ? "evaluation-set" : "test-plan";
  const kind = kindOfType(resource.type ?? "");
  if (!kind || kind.kind === "automation" || kind.kind === "media-set") return undefined;
  return (kind.ref ?? kind.kind) as ReleaseKind;
}

function productDrafts(product: ProductRecord, resources: Api.ResourceEntry[]): JointChoice[] {
  return product.roots.flatMap(root => {
    const resource = resources.find(item => item.ref.app === root.app && item.ref.kind === root.kind && item.ref.name === root.name);
    const kind = resource && draftKind(resource);
    return resource?.draft && kind ? [{ kind, id: resource.draft }] : [];
  });
}

function ProductEditor({ product, projects, resources, defaultProject, onClose, onSaved }: {
  product?: ProductRecord; projects: Project[]; resources: Api.ResourceEntry[];
  defaultProject?: string;
  onClose: () => void; onSaved: (id: string) => void;
}) {
  const { decide } = useHost();
  const [form, setForm] = useState<Form>(product ? {
    name: product.name, title: product.title, description: product.description ?? "", project: product.project,
    namespace: product.namespace ?? "", roots: product.roots ?? [], requires: (product.requires ?? []).join(", "),
  } : { ...emptyForm, project: defaultProject ?? "" });
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm(current => ({ ...current, [key]: value }));
  const roots = resources.filter(resource => resource.project === form.project && !resource.archived && resource.canEdit && draftKind(resource));
  const has = (ref: Api.AssetRef) => form.roots.some(root => root.app === ref.app && root.kind === ref.kind && root.name === ref.name);
  const save = async () => {
    setBusy(true); setError("");
    try {
      const payload = {
        name: form.name.trim(), title: form.title.trim(), description: form.description.trim(), project: form.project,
        namespace: form.namespace.trim(), roots: form.roots,
        requires: form.requires.split(",").map(value => value.trim()).filter(Boolean),
      };
      const saved = product
        ? await decide("build.product.edit", { type: "build.product", id: product.id }, payload, { expectedRevision: product.revision, onRefused: setError })
        : await decide("build.product.create", { type: "build.product", id: form.name.trim() }, payload, { onRefused: setError });
      if (saved) { onSaved(form.name.trim()); onClose(); }
    } catch (cause) {
      setError(apiErrorMessage(cause) ?? t("The product could not be saved."));
    } finally { setBusy(false); }
  };
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose(); }} title={t(product ? "Edit product" : "New product")} wide>
    <div className="grid gap-3">
      <p className="text-sm text-muted">{t("Choose the project and exact resources this product delivers. The contents become release drafts; versions freeze one saved candidate.")}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("Product name")}<Input value={form.name} disabled={!!product || busy} placeholder="inventory" onChange={event => set("name", event.target.value)} /></label>
        <label className="grid gap-1 text-sm">{t("Title")}<Input value={form.title} disabled={busy} onChange={event => set("title", event.target.value)} /></label>
      </div>
      <label className="grid gap-1 text-sm">{t("Description")}<Input value={form.description} disabled={busy} onChange={event => set("description", event.target.value)} /></label>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("Project")}<select className="rounded-md border border-border bg-input px-3 py-2" disabled={!!product || busy} value={form.project} onChange={event => { set("project", event.target.value); set("roots", []); }}>
          <option value="">{t("Choose a project")}</option>{projects.filter(project => !project.archived).map(project => <option key={project.id} value={project.id}>{project.title || project.name}</option>)}
        </select></label>
        <label className="grid gap-1 text-sm">{t("Namespace")}<Input value={form.namespace} disabled={busy} placeholder={t("Defaults to the product name")} onChange={event => set("namespace", event.target.value)} /></label>
      </div>
      <fieldset className="grid gap-2"><legend className="mb-1 text-sm font-medium">{t("Contents")}</legend>
        {!form.project && <p className="text-sm text-muted">{t("Choose a project to list the resources it can deliver.")}</p>}
        {form.project && !roots.length && <p className="text-sm text-muted">{t("This project has no releaseable resources yet.")}</p>}
        <div className="grid max-h-64 gap-1 overflow-auto rounded-md border border-border p-2 sm:grid-cols-2">
          {roots.map(resource => <Checkbox key={resource.id} checked={has(resource.ref)} onChange={checked => set("roots", checked ? [...form.roots, resource.ref] : form.roots.filter(root => root.app !== resource.ref.app || root.kind !== resource.ref.kind || root.name !== resource.ref.name))}>
            <span className="grid"><span>{resource.title}</span><span className="font-mono text-xs text-muted">{resource.ref.kind} · {resource.ref.name}</span></span>
          </Checkbox>)}
        </div>
      </fieldset>
      <label className="grid gap-1 text-sm">{t("Dependency products")}<Input value={form.requires} disabled={busy} placeholder="inventory@^1.0.0, identity@~2.1.0" onChange={event => set("requires", event.target.value)} /><span className="text-xs text-muted">{t("These resources stay in their own products and are resolved from exact product versions during installation.")}</span></label>
      {error && <Notice tone="danger" role="alert" message={error} />}
      <div className="flex justify-end gap-2"><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button><Button variant="primary" loading={busy} disabled={!form.name.trim() || !form.title.trim() || !form.project || !form.roots.length} onClick={() => void save()}>{t("Save product")}</Button></div>
    </div>
  </Dialog>;
}

function VersionPublisher({ product, onPublished }: { product: ProductRecord; onPublished: () => Promise<unknown> | void }) {
  const { decide } = useHost();
  const releases = useReadQuery<Api.ReleasePage>("/v1/releases/candidates?limit=100");
  const [version, setVersion] = useState("1.0.0"), [candidate, setCandidate] = useState("");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const publish = async () => {
    setBusy(true); setError("");
    try {
      const saved = await decide("build.productversion.create", { type: "build.productversion", id: `${product.id}@${version.trim()}` }, { product: product.id, version: version.trim(), candidate }, { onRefused: setError });
      if (saved) { setCandidate(""); await onPublished(); }
    } catch (cause) { setError(apiErrorMessage(cause) ?? t("The product version could not be published.")); }
    finally { setBusy(false); }
  };
  return <Panel title={t("Publish a version from a saved candidate")} className="grid gap-3">
    <p className="text-sm text-muted">{t("A version freezes the candidate closure of this product's contents. Candidate IDs come from DevOps Releases after Save immutable candidate.")}</p>
    <div className="grid gap-3 sm:grid-cols-2">
      <label className="grid gap-1 text-sm">{t("Semantic version")}<Input value={version} disabled={busy} placeholder="1.0.0" onChange={event => setVersion(event.target.value)} /></label>
      <label className="grid gap-1 text-sm">{t("Saved candidate")}<select aria-label={t("Saved candidate")} className="rounded-md border border-border bg-input px-3 py-2" value={candidate} disabled={busy} onChange={event => setCandidate(event.target.value)}>
        <option value="">{t("Choose a saved candidate")}</option>{(releases.data?.candidates ?? []).map(item => <option key={item.id} value={item.id}>{item.title} · {item.assets} {t("assets")} · {item.id.slice(0, 20)}…</option>)}
      </select></label>
    </div>
    {releases.isError && <Notice tone="danger" message={t("Saved candidates could not be read.")} />}
    {error && <Notice tone="danger" role="alert" message={error} />}
    <div><Button variant="primary" loading={busy} disabled={!version.trim() || !candidate} onClick={() => void publish()}>{t("Publish product version")}</Button></div>
  </Panel>;
}

export function Products({ project, id }: { project?: string; id?: string }) {
  const { holds, client } = useHost(), { open } = useWorkspace();
  const builder = holds("build", "builder"), mayRelease = builder || holds("build", "publisher");
  const products = useRecordInventory<ProductRecord>("build.product", 1000, mayRelease);
  const versions = useRecordInventory<ProductVersion>("build.productversion", 1000, mayRelease);
  const projects = useRecordInventory<Project>("build.app", 1000, mayRelease);
  const resources = useReadQuery<Api.ResourceDirectory>("/v1/resources", undefined, mayRelease);
  const [editing, setEditing] = useState<ProductRecord | "new" | undefined>();
  const [selected, setSelected] = useState(id ?? "");
  const [candidateBusy, setCandidateBusy] = useState(false), [candidateError, setCandidateError] = useState("");
  const chosen = products.data?.records.find(product => product.id === selected && !product.archived);
  const startCandidate = async (drafts: JointChoice[]) => {
    setCandidateBusy(true); setCandidateError("");
    try {
      const expanded = await applicationDraftSelection(drafts, async root => {
        const result = await client.call<Api.ReleaseDraftClosure>("POST", "/v1/releases/drafts/referenced", ownerDraft(root));
        if (!result.ok) throw new Error(apiErrorMessage(result.body) ?? t("Draft dependencies could not be read."));
        return result.body.drafts.map(draft => ({ kind: draft.kind as ReleaseKind, id: draft.id }));
      });
      open({ view: "release-review", params: { drafts: releaseDraftsValue(expanded) } });
    } catch (cause) {
      setCandidateError(cause instanceof Error ? cause.message : t("Draft dependencies could not be read."));
    } finally { setCandidateBusy(false); }
  };
  if (!mayRelease) return <PageHeader title={t("Products")} description={t("A builder or publisher role is required to define products.")} />;
  return <>
    <PageHeader title={t("Products")} description={t("Choose project contents, create a release candidate, then freeze immutable versions for Marketplace.")} actions={<Button variant="primary" disabled={!mayRelease} onClick={() => setEditing("new")}>{t("New product")}</Button>} />
    <div className="grid min-w-0 items-start gap-5 md:grid-cols-[minmax(14rem,18rem)_minmax(0,1fr)]">
      <Panel variant="plain" aria-label={t("Product list")}>
        {products.isError && <div role="alert"><p>{t("Products could not be loaded.")}</p><Button onClick={() => void products.refetch()}>{t("Retry")}</Button></div>}
        {products.isPending && <p role="status">{t("Loading products…")}</p>}
        {!products.isPending && !products.isError && !(products.data?.records ?? []).some(product => !product.archived) && <p className="text-sm text-muted">{t("No products yet. A product binds project contents to a release and version history.")}</p>}
        <div className="grid gap-1">{(products.data?.records ?? []).filter(product => !product.archived).map(product => <Button key={product.id} variant="row" aria-current={selected === product.id ? "true" : undefined} className={`h-auto w-full justify-start whitespace-normal rounded-md p-3 text-left ${selected === product.id ? "bg-row-selected" : ""}`} onClick={() => setSelected(product.id)}>
          <span className="grid min-w-0 gap-1"><span>{product.title}</span><span className="truncate font-mono text-xs text-muted">{product.name} · {product.project}</span></span>
        </Button>)}</div>
      </Panel>
      <Panel variant="plain">
        {!chosen ? <p className="text-sm text-muted">{t("Select a product to inspect its contents and versions, or create a new product.")}</p> : (() => {
          const saved = (versions.data?.records ?? []).filter(version => version.product === chosen.name).sort((a, b) => a.version.localeCompare(b.version, undefined, { numeric: true }));
          const drafts = productDrafts(chosen, resources.data?.resources ?? []);
          const canCandidate = drafts.length === chosen.roots.length && drafts.length > 0;
          return <div className="grid gap-4">
            <div className="flex flex-wrap items-center gap-2"><div className="mr-auto"><h2 className="text-lg font-semibold">{chosen.title}</h2><p className="font-mono text-xs text-muted">{chosen.id}</p></div>{mayRelease && <Button onClick={() => setEditing(chosen)}>{t("Edit contents")}</Button>}</div>
            {chosen.description && <p className="text-sm">{chosen.description}</p>}
            <PropertyList items={[[t("Project"), chosen.project], [t("Namespace"), chosen.namespace || chosen.name], [t("Dependencies"), chosen.requires?.join(", ") || t("None")], [t("Versions"), saved.length]]} />
            <Panel title={t("Contents")} className="grid gap-2">
              <p className="text-sm text-muted">{t("Contents are the roots of every candidate and version. Dependencies remain owned by their own products.")}</p>
              <ul className="grid gap-1 text-sm">{chosen.roots.map(root => <li key={`${root.app}/${root.kind}/${root.name}`} className="flex flex-wrap gap-2"><span className="font-mono text-xs">{root.app}/{root.kind}/{root.name}</span></li>)}</ul>
              {resources.isError && <Notice tone="danger" message={t("Project resources could not be read.")} />}
              <div className="flex flex-wrap gap-2"><Button variant="primary" loading={candidateBusy} disabled={!canCandidate || candidateBusy} onClick={() => void startCandidate(drafts)}>{t("Create release candidate")}</Button><span className="text-xs text-muted">{t("Review the dependency closure, save the candidate, then return here to publish a semantic version.")}</span></div>
              {candidateError && <Notice tone="danger" role="alert" message={candidateError} />}
            </Panel>
            <Panel title={t("Versions")} className="grid gap-2">
              {!saved.length && <p className="text-sm text-muted">{t("No versions yet. Save a release candidate, then freeze it here.")}</p>}
              <div className="grid gap-2">{saved.map(version => <div key={version.id} className="grid gap-1 rounded-md border border-border p-3 text-sm">
                <div className="flex flex-wrap items-center gap-2"><strong>{version.version}</strong><Tag label={t("Immutable")} tone="success" /><span className="ml-auto font-mono text-xs text-muted">{version.descriptor.blueprint?.assets.length ?? 0} {t("assets")}</span></div>
                <PropertyList items={[[t("Candidate"), <span className="break-all font-mono text-xs">{version.candidate}</span>], [t("Content digest"), <span className="break-all font-mono text-xs">{version.contentDigest || version.descriptor.artifactDigest || "—"}</span>]]} />
              </div>)}</div>
            </Panel>
            {mayRelease && <VersionPublisher product={chosen} onPublished={async () => { await versions.refetch(); }} />}
          </div>;
        })()}
      </Panel>
    </div>
    {editing && <ProductEditor product={editing === "new" ? undefined : editing} defaultProject={project} projects={projects.data?.records ?? []} resources={resources.data?.resources ?? []} onClose={() => setEditing(undefined)} onSaved={id => { setSelected(id); void products.refetch(); }} />}
  </>;
}
