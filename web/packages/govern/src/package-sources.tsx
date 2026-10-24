import { useHost } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Disclosure, Input, Panel, PropertyList, Select, t, useWorkspace } from "@platform/ui";
import { BookOpen, Plus } from "lucide-react";
import { useRef, useState } from "react";

export function PackageSources({ items, reviewOnly = false, inspectPath = "/v1/packages/inspect" }: { items: Api.PackageView[]; reviewOnly?: boolean; inspectPath?: string }) {
  const { client, decide, can } = useHost(), { open } = useWorkspace();
  const [expanded, setExpanded] = useState(false), [source, setSource] = useState(reviewOnly ? "file" : "existing");
  const [address, setAddress] = useState(""), [file, setFile] = useState<File>();
  const [preview, setPreview] = useState<Api.PackagePreview>(), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const pending = useRef<string | undefined>(undefined);
  const inspect = async () => {
    setBusy(true); setError(""); setPreview(undefined); pending.current = undefined;
    try {
      if (source === "file" && (!file || file.size > 96 * 1024 * 1024)) { setError(t("Choose a package JSON file of at most 96 MiB.")); return; }
      const request = source === "online" ? { url: address.trim() } : { content: JSON.parse(await file!.text()) as unknown };
      const result = await client.call<Api.PackagePreview>("POST", inspectPath, request);
      if (!result.ok) { setError(apiErrorMessage(result.body) ?? t("The package could not be inspected.")); return; }
      setPreview(result.body);
    } catch (cause) { setError(apiErrorMessage(cause) ?? t("The package could not be inspected. Check the file or address and try again.")); }
    finally { setBusy(false); }
  };
  const add = async () => {
    if (!preview) return;
    setBusy(true); setError("");
    try {
      const ok = await decide("platform.package.import", { type: "platform.package", id: preview.descriptor.id }, { id: preview.descriptor.id, digest: preview.digest }, {
        originalKey: pending.current, onRefused: setError,
        onOutcome: entry => { pending.current = entry.submission.idempotencyKey; },
      });
      if (ok) { setExpanded(false); setPreview(undefined); pending.current = undefined; }
    } finally { setBusy(false); }
  };
  return <div className="mb-4 grid gap-3">
    <div className="flex flex-wrap gap-2">
      {(reviewOnly || can("platform.package.import")) && <Button variant="primary" aria-expanded={expanded} disabled={busy || !!pending.current} onClick={() => setExpanded(!expanded)}><Plus size={16} />{t(reviewOnly ? "Review a package" : "Add package")}</Button>}
      <Button onClick={() => open({ view: reviewOnly ? "host-package-help" : "package-help", params: { topic: "source" } })}><BookOpen size={16} />{t("Package guide")}</Button>
    </div>
    {expanded && <Panel title={t("Add package")} className="grid min-w-0 max-w-3xl gap-3">
      <p className="text-sm text-muted">{t("Choose a source, review its contents, then install an exact version. Adding a package to the library does not run it.")}</p>
      <label className="grid gap-1 text-sm">{t("Package source")}<Select aria-label={t("Package source")} disabled={busy || !!pending.current} value={source} onChange={event => { setSource(event.target.value); setPreview(undefined); setError(""); pending.current = undefined; }}>
        {!reviewOnly && <option value="existing">{t("Use an existing package")}</option>}<option value="online">{t("Download from a URL")}</option><option value="file">{t("Import a local file")}</option>
      </Select></label>
      {source === "existing" ? <p className="text-sm">{t(items.length ? "Available packages are listed below. Choose an exact version and review its requirements before installing." : "This library is empty. Download a package or import a local file to begin.")}</p> : <>
        {source === "online" ? <label className="grid gap-1 text-sm">{t("Package URL")}<Input aria-label={t("Package URL")} type="url" placeholder="https://example.com/package.json" value={address} disabled={busy || !!preview} onChange={event => setAddress(event.target.value)} /><span className="text-xs text-muted">{t("Use a direct HTTPS link to a platform package JSON file. Website pages and arbitrary plugin code are not supported.")}</span></label> : <label className="grid gap-1 text-sm">{t("Package file")}<Input aria-label={t("Package file")} type="file" accept=".json,application/json" disabled={busy || !!preview} onChange={event => setFile(event.target.files?.[0])} /><span className="text-xs text-muted">{t("A platform package descriptor or platform.package.v1 JSON archive, up to 96 MiB. Secrets belong in Credentials, never in a package.")}</span></label>}
        {!preview && <div><Button disabled={busy || (source === "online" ? !address.trim() : !file)} onClick={() => void inspect()}>{t(busy ? "Inspecting package…" : "Review package")}</Button></div>}
      </>}
      {reviewOnly && <p className="text-sm text-muted">{t("Maintenance review cannot install or activate a package. Restore trusted implementations here; use Packages after business access resumes.")}</p>}
      {preview && <div className="grid min-w-0 gap-3 border-t pt-3">
        <PropertyList items={[[t("Package"), preview.descriptor.title || preview.descriptor.id], [t("Version"), preview.descriptor.version], [t("Source"), preview.origin], [t("Reviewed content"), <span className="break-all font-mono text-xs">{preview.digest}</span>]]} />
        {preview.descriptor.description && <p className="text-sm">{preview.descriptor.description}</p>}
        <p className="text-sm">{t("Installation registers this version only. Publishing application definitions, granting access and creating business data are separate actions.")}</p>
        {!!preview.precheck.missing?.length && <p className="text-sm text-[var(--tone-warning)]">{t("Install required packages first: {packages}", { packages: preview.precheck.missing.join(", ") })}</p>}
        {preview.precheck.problems?.map(problem => <p key={problem} className="text-sm text-[var(--tone-warning)]">{problem}</p>)}
        {!preview.precheck.ok && <p className="text-sm">{t("This version cannot be installed here. Install its dependencies or obtain a compatible package.")}</p>}
        <Disclosure summary={t("Inspect all package contents")}><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify({ descriptor: preview.descriptor, artifacts: preview.artifacts }, null, 2)}</pre></Disclosure>
        <div className="flex flex-wrap gap-2">{!reviewOnly && <Button variant="primary" disabled={busy} onClick={() => void add()}>{t(busy ? "Adding…" : "Add reviewed version to library")}</Button>}<Button disabled={busy || !!pending.current} onClick={() => setPreview(undefined)}>{t("Choose another source")}</Button></div>
      </div>}
      {error && <p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{error}</p>}
    </Panel>}
  </div>;
}

export function PackageHelp({ topic = "start", maintenance = false }: { topic?: string; maintenance?: boolean }) {
  const { open } = useWorkspace();
  const topics = [
    ["Start with a package", "A package is a versioned collection of capabilities or application definitions. Open Packages and select Add package. You can use the existing library, download from a direct HTTPS address, or import a local JSON file. Review the name, exact version, source, requirements and contents before adding it."],
    ["Install and start using it", "Adding to the library makes a package available. Install registers its contributions after checking dependencies and compatibility. For an application blueprint, choose Create application drafts, then review and publish in Application Studio. Installation never grants roles, publishes drafts, initializes business data or enables device writes."],
    ["Understand the source", "Online and local files use the same review and installation checks. The reviewed digest identifies the saved contents; installation does not download them again. A version cannot be replaced with different contents. Downloading a descriptor does not install arbitrary executable code; protocol binaries and foundational implementations require a trusted deployment."],
    ["Uninstall safely", "Choose Prepare to uninstall to stop new use, then Uninstall when dependencies and work in flight allow it. Dependent packages must be removed first. Uninstall removes live contributions, while business records, history, technical receipts and retained artifacts remain. It cannot undo messages already sent or physical device actions."],
    ["Remove a source or reinstall", "Remove source only removes an uninstalled imported version from the available library. It does not delete retained history or sealed artifacts. To use a package again, import or select exactly the same version and contents, install it, then follow its usage guide. Deployment-provided sources are managed by the host administrator."],
    ["When installation is blocked", "Select a package in the library and read its details for missing dependencies or incompatible versions. Install dependencies first, or obtain a compatible package. A failed download or review installs nothing. After a connection failure, refresh the inventory and retry the original request before choosing a different version."],
    ["Foundational capabilities", "Rules, authorization, storage and kernel implementations must satisfy the platform contracts. A foundational change requires maintenance and may require a restart or verified storage handover. Removing an implementation must not erase durable business state. Ask a host administrator to perform these operations through the supported deployment process."],
  ];
  const ids = ["start", "install", "source", "uninstall", "remove", "errors", "foundations"];
  const selected = Math.max(0, ids.indexOf(topic));
  return <div className="grid max-w-3xl gap-4">
    <div className="flex flex-wrap items-center justify-between gap-2"><h1 className="text-xl font-semibold">{t("Package guide")}</h1>{!maintenance && <Button onClick={() => open({ view: "apps" })}>{t("Open Packages")}</Button>}</div>
    <p className="text-sm text-muted">{t("Add, use and remove packages without losing your application data.")}</p>
    <nav aria-label={t("Guide topics")} className="flex flex-wrap gap-2">{topics.map(([title], index) => <Button key={title} size="sm" variant={index === selected ? "primary" : "default"} aria-current={index === selected ? "page" : undefined} onClick={() => open({ view: maintenance ? "host-package-help" : "package-help", params: { topic: ids[index]! } })}>{t(title!)}</Button>)}</nav>
    <Panel title={t(topics[selected]![0]!)}><p className="text-sm leading-relaxed">{t(topics[selected]![1]!)}</p></Panel>
    <div className="flex flex-wrap gap-2">{!maintenance && <><Button onClick={() => open({ view: "credentials" })}>{t("Credentials")}</Button><Button onClick={() => open({ view: "matrix" })}>{t("Capability matrix")}</Button></>}<Button onClick={() => open({ view: "host-overview" })}>{t("Host Console")}</Button></div>
  </div>;
}
