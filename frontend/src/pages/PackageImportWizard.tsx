import { Banner } from "@astryxdesign/core/Banner";
import { FileInput } from "@astryxdesign/core/FileInput";
import { VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useEffect, useMemo, useRef, useState } from "react";
import { type Api, request, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { FormWizard, type WizardStep } from "./FormWizard";
import { PackageReviewContent } from "./PackageReviewContent";

type SourceMode = "file" | "url";

export type InitialPackageSource = {
  url: string;
  sha256?: string;
};

export function PackageImportWizard({
  initialSource,
  onClose,
  onDone,
}: {
  initialSource?: InitialPackageSource;
  onClose: () => void;
  onDone: () => void;
}) {
  const [step, setStep] = useState(0);
  const [sourceMode, setSourceMode] = useState<SourceMode>(initialSource ? "url" : "file");
  const [file, setFile] = useState<File | null>(null);
  const [url, setUrl] = useState(initialSource?.url ?? "");
  const [expectedSha256, setExpectedSha256] = useState(initialSource?.sha256 ?? "");
  const [preview, setPreview] = useState<Api.PackagePreview>();
  const [imported, setImported] = useState(false);
  const [namespace, setNamespace] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const autoInspected = useRef(false);

  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const actions = catalog.status === "ready" ? catalog.data : [];
  const importAction = actions.find((action) => action.schema === "platform.package.import");
  const installAction = actions.find((action) => action.schema === "platform.package.install");
  const upgradeAction = actions.find((action) => action.schema === "platform.package.upgrade");

  const importedView = useMemo(() => {
    if (!preview || packages.status !== "ready") return undefined;
    return packages.data.find((entry) => entry.descriptor.id === preview.descriptor.id && entry.descriptor.version === preview.descriptor.version);
  }, [packages, preview]);

  const id = preview?.descriptor.id ?? "";
  const version = preview?.descriptor.version ?? "";
  const review = useRead<Api.PackageReview>(imported && id && version ? `/v1/packages/${encodeURIComponent(id)}/review?version=${encodeURIComponent(version)}` : null);
  const preparation = useRead<Api.PackagePreparation>(imported && id && version ? `/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}` : null);

  const reviewProblems = [...new Set([
    ...(importedView?.plan.problems ?? []),
    ...(importedView?.precheck.problems ?? []),
    ...(review.status === "ready" ? review.data.problems ?? [] : review.status === "loading" ? [] : [review.reason]),
    ...(preparation.status === "error" || preparation.status === "refused" ? [preparation.reason] : []),
  ])];
  const setupNotices = preparation.status === "ready"
    ? [...new Set([
      ...preparation.data.connections.filter((connection) => !connection.ready).map((connection) => `${connection.title}: ${t("Not configured")}`),
      ...preparation.data.ai.filter((requirement) => requirement.problem).map((requirement) => requirement.problem!),
    ])]
    : [];

  const steps: WizardStep[] = [
    {
      id: "source",
      label: t("Package source"),
      description: t("Choose a local package file or an HTTPS address."),
      help: [
        t("Packages are declarative JSON artifacts up to 16 MiB."),
        t("Inspecting reads and validates the package without installing or executing code."),
        t("Provide an optional SHA-256 checksum to verify exact file integrity."),
      ],
      hasError: step === 0 && Boolean(error),
    },
    {
      id: "verify",
      label: t("Verify and import"),
      description: t("Check identity, origin and SHA-256 before adding to this library."),
      help: [
        t("Adding to the library records the verified artifact without changing business data."),
        t("Existing package versions are immutable and cannot be overwritten."),
      ],
      hasError: step === 1 && (Boolean(error) || (preview?.precheck.problems?.length ?? 0) > 0),
    },
    {
      id: "review",
      label: t("Review and configure"),
      description: t("Inspect applications, roles, dependencies and target namespace."),
      help: [
        t("Installing a package never grants data or action permissions automatically."),
        t("Choose another API name if this workspace already has a project with the same name."),
      ],
      hasError: step === 2 && reviewProblems.length > 0,
    },
    {
      id: "install",
      label: t("Confirm installation"),
      description: t("Apply the verified package version to this workspace."),
      help: [
        t("The package is already stored in your local library."),
        t("After installation, assign package roles to members under Admin → Members."),
      ],
      hasError: step === 3 && Boolean(error),
    },
  ];

  const inspectSource = async () => {
    setBusy(true);
    setError("");
    try {
      const body: Api.PackageInspectRequest = {};
      if (sourceMode === "file") {
        if (!file) throw new Error(t("Choose a package file first."));
        body.content = JSON.parse(await file.text());
      } else {
        if (!url.trim()) throw new Error(t("Enter an HTTPS package URL."));
        body.url = url.trim();
      }
      if (expectedSha256.trim()) body.expectedSha256 = expectedSha256.trim();
      const answer = await request<Api.PackagePreview>("/v1/packages/inspect", { body });
      setPreview(answer);
      setNamespace(answer.descriptor.namespace);
      setImported(false);
      setStep(1);
    } catch (problem) {
      setError(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    if (!initialSource?.url || autoInspected.current) return;
    autoInspected.current = true;
    void inspectSource();
  }, []);

  const importCandidate = async () => {
    if (!preview || !importAction) return;
    setBusy(true);
    setError("");
    try {
      await send(importAction, preview.descriptor.id, { id: preview.descriptor.id, digest: preview.digest });
      setImported(true);
      reread();
      setStep(2);
    } catch (problem) {
      setError(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const installImported = async () => {
    if (!preview) return;
    const action = importedView?.update ? upgradeAction : installAction;
    if (!action) return;
    setBusy(true);
    setError("");
    try {
      const named = !importedView?.installed && Boolean(namespace.trim());
      await send(action, preview.descriptor.id, named ? { id, version, namespace: namespace.trim() } : { id, version });
      reread();
      onDone();
    } catch (problem) {
      setError(reasonOf(problem));
      setBusy(false);
    }
  };

  const onNext = () => {
    if (step === 0) void inspectSource();
    else if (step === 1) void importCandidate();
    else if (step === 2) setStep(3);
    else void installImported();
  };

  const canProceed =
    step === 0 ? (sourceMode === "file" ? Boolean(file) : Boolean(url.trim()))
    : step === 1 ? Boolean(preview && importAction && (preview.precheck.problems?.length ?? 0) === 0)
    : step === 2 ? Boolean(importedView && review.status === "ready" && preparation.status !== "loading" && reviewProblems.length === 0 && (importedView.installed || namespace.trim()))
    : Boolean(preview && (importedView?.update ? upgradeAction : installAction));

  const primaryLabel =
    step === 0 ? t("Inspect package")
    : step === 1 ? t("Add to library")
    : step === 2 ? t("Continue")
    : importedView?.update ? t("Upgrade package") : t("Install package");

  return (
    <FormWizard
      title={t("Install from URL or file")}
      steps={steps}
      activeStep={step}
      onStepClick={(index) => { setError(""); setStep(index); }}
      onBack={() => { setError(""); setStep((current) => Math.max(0, current - 1)); }}
      onNext={onNext}
      onClose={onClose}
      primaryLabel={primaryLabel}
      canProceed={canProceed}
      busy={busy}
      error={error}
    >
      {step === 0 && (
        <VStack gap={4}>
          <SegmentedControl label={t("Source")} value={sourceMode} onChange={(next) => { setSourceMode(next as SourceMode); setError(""); }}>
            <SegmentedControlItem value="file" label={t("Local file")} />
            <SegmentedControlItem value="url" label={t("HTTPS address")} />
          </SegmentedControl>
          {sourceMode === "file" ? (
            <FileInput
              label={t("Package file")}
              description={t("Select a declarative platform package (.json), up to 16 MiB.")}
              accept=".json,application/json"
              maxSize={16 * 1024 * 1024}
              value={file}
              onChange={(picked) => setFile(Array.isArray(picked) ? picked[0] ?? null : picked)}
            />
          ) : (
            <TextInput
              label={t("Package URL")}
              isRequired
              placeholder={t("https://example.com/package.json")}
              value={url}
              onChange={setUrl}
            />
          )}
          <TextInput
            label={t("Expected SHA-256")}
            placeholder={t("sha256:...")}
            value={expectedSha256}
            onChange={setExpectedSha256}
          />
        </VStack>
      )}

      {step === 1 && preview && (
        <VStack gap={4}>
          <Banner status="warning" title={t("Local library")} description={t("Self-declared metadata from an external source")} />
          {(preview.precheck.problems?.length ?? 0) > 0 && (
            <Banner status="error" title={t("This version cannot be used as it stands")} description={preview.precheck.problems!.join(" · ")} />
          )}
          <MetadataList columns="single" label={{ position: "start", width: 140 }}>
            <MetadataListItem label={t("Title")}>
              <Text type="body" weight="medium">{t(preview.descriptor.title || preview.descriptor.id)}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Version")}>
              <Text type="body">{preview.descriptor.id} · {preview.descriptor.version}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Origin")}>
              <Text type="code" color="secondary">{preview.origin === "local-file" ? t("Local file") : preview.origin}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("File SHA-256")}>
              <Text type="code" color="secondary">{preview.artifactSha256 || preview.digest}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Namespace")}>
              <Text type="code" color="secondary">{preview.descriptor.namespace}</Text>
            </MetadataListItem>
          </MetadataList>
          {preview.descriptor.description && <Text type="supporting" color="secondary">{t(preview.descriptor.description)}</Text>}
        </VStack>
      )}

      {step === 2 && (
        importedView ? (
          <VStack gap={4}>
            {reviewProblems.length > 0 && (
              <Banner status="error" title={t("This version cannot be used as it stands")} description={reviewProblems.join(" · ")} />
            )}
            {setupNotices.length > 0 && (
              <Banner status="warning" title={t("Configuration needed after installation")} description={setupNotices.join(" · ")} />
            )}
            {!importedView.installed && (
              <TextInput
                label={t("Project API name")}
                isRequired
                value={namespace}
                onChange={setNamespace}
                description={t("The managed project, its types and its actions take this name. Choose another when this tenant already has a project with it.")}
              />
            )}
            <PackageReviewContent view={importedView} mode="install" review={review} preparation={preparation} />
          </VStack>
        ) : (
          <Text type="supporting" color="secondary">{t("Loading…")}</Text>
        )
      )}

      {step === 3 && preview && (
        <VStack gap={4}>
          <MetadataList columns="single" label={{ position: "start", width: 140 }}>
            <MetadataListItem label={t("Title")}>
              <Text type="body" weight="medium">{t(preview.descriptor.title || preview.descriptor.id)}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Version")}>
              <Text type="body">{preview.descriptor.id} · {preview.descriptor.version}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Origin")}>
              <Text type="code" color="secondary">{preview.origin === "local-file" ? t("Local file") : preview.origin}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("File SHA-256")}>
              <Text type="code" color="secondary">{preview.artifactSha256 || preview.digest}</Text>
            </MetadataListItem>
            <MetadataListItem label={t("Namespace")}>
              <Text type="code" color="secondary">{namespace || preview.descriptor.namespace}</Text>
            </MetadataListItem>
          </MetadataList>
          <Text type="supporting" color="secondary">
            {t("Packages are immutable declarative artifacts. No package code runs on the server, and installing never grants data or action permissions.")}
          </Text>
        </VStack>
      )}
    </FormWizard>
  );
}
