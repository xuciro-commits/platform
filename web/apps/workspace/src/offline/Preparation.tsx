import { useState } from "react";
import { useHost } from "@platform/app";
import { Button, Dialog, Disclosure, InspectorSection, Notice, Panel, Textarea, t } from "@platform/ui";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { prepareShell } from "./shell";

export function OfflinePreparation({ application, release, open, onOpenChange }: { application?: string; release?: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const host = useHost(), client = host.client;
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [saved, setSaved] = useState(""), [, setRevision] = useState(0);
  const definition = host.definitions.find(item => item.ref.kind === "app" && item.application && `${item.ref.app}:${item.application.name}` === application && item.source !== "code");
  let applications: ReturnType<NonNullable<typeof client.prepared>["applications"]> = [], drafts: ReturnType<NonNullable<typeof client.prepared>["forms"]> = [], storageError = "";
  try { if (open) { applications = client.prepared?.applications() ?? []; drafts = client.prepared?.forms() ?? []; } } catch (problem) { storageError = String(problem instanceof Error ? problem.message : problem); }
  const refresh = () => setRevision(value => value + 1);
  const prepare = async () => {
    if (!definition?.application || !release || busy) return;
    setBusy(true); setError(""); setSaved("");
    try {
      const paths = new Set<string>(), objects = new Set<string>(), references = new Set<string>();
      for (const page of host.definitions.filter(item => item.ref.kind === "page" && item.ref.app === definition.ref.app && definition.application!.pages.includes(item.ref.name))) {
        if (page.page?.object.name) objects.add(page.page.object.name);
        if (page.contentVersion) paths.add(`/v1/pages/${encodeURIComponent(page.ref.app)}/${encodeURIComponent(page.ref.name)}/${encodeURIComponent(page.contentVersion)}`);
      }
      for (const type of objects) { paths.add(client.recordsPath(type, { sort: ["-changed"], limit: 100 })); for (const field of host.source.entity(type)?.fields ?? []) if (field.type === "reference" && field.ref && field.ref !== "enterprise.element") references.add(field.ref); }
      for (const type of references) paths.add(client.recordsPath(type, { limit: 25 }));
      await prepareShell();
      const prepared = await client.prepareApplication({ id: application!, title: definition.application.title, binding: { ref: definition.ref, sourceVersion: definition.version }, release, route: location.hash }, [...paths]);
      setSaved(t("Prepared at {time}.", { time: new Date(prepared.at).toLocaleString() })); refresh();
    } catch (problem) { setError(t(problem instanceof Error ? problem.message : String(problem))); } finally { setBusy(false); }
  };
  return <Dialog open={open} onOpenChange={value => !busy && onOpenChange(value)} title={t("Offline applications")}>
    <VStack gap={4}>
      <Text as="p">{t("Prepare this published application, its first record window and the tasks currently open. Other records and tasks require a connection.")}</Text>
      <Text as="p" type="supporting">{t("Saved content is a dated copy. Queued requests wait for the host's current permissions and business decision.")}</Text>
      {definition && <Panel title={definition.application!.title}>
        <VStack gap={2}>
          <Text type="supporting">{t("Application version {version}", { version: definition.version })}</Text>
          <Button variant="primary" loading={busy} disabled={!release || navigator.onLine === false || !!storageError} onClick={() => void prepare()}>{t("Prepare for offline")}</Button>
        </VStack>
      </Panel>}
      {!definition && <Text as="p" type="supporting">{t("Open a published business application to prepare it.")}</Text>}
      {(error || storageError) && <Notice tone="danger" message={error || t(storageError)} />}
      {saved && <Text as="p" role="status">{saved}</Text>}
      <InspectorSection title={t("Prepared applications")}>
        {!applications.length && !storageError && <Text as="p" type="supporting">{t("No application is prepared for this identity.")}</Text>}
        {applications.map(app => <Panel key={app.id} title={app.title}>
          <VStack gap={2}>
            <Text type="supporting">{t("Version {version} · saved {time}", { version: app.binding.sourceVersion, time: new Date(app.at).toLocaleString() })}</Text>
            <HStack gap={2} wrap="wrap">
              <Button disabled={busy} onClick={() => void (async () => { client.prepared?.activate(app.id); await client.refreshDeclarations(); await host.refresh?.(); location.hash = app.route; onOpenChange(false); })().catch(problem => setError(t(problem instanceof Error ? problem.message : String(problem))))}>{t("Open prepared application")}</Button>
              <Button disabled={busy} onClick={() => { try { client.prepared?.remove(app.id); refresh(); } catch (problem) { setError(t(String(problem instanceof Error ? problem.message : problem))); } }}>{t("Remove preparation")}</Button>
            </HStack>
          </VStack>
        </Panel>)}
      </InspectorSection>
      <InspectorSection title={t("Local form drafts")}>
        <Text as="p" type="supporting">{t("Open the same create or record action to continue a saved draft. Earlier definitions retain their original inputs here.")}</Text>
        {drafts.map(draft => <Disclosure key={draft.key} summary={`${host.catalog.find(action => action.schema === draft.schema)?.title ?? draft.schema} · ${draft.target.id}`}>
          <VStack gap={2}>
            <Text type="supporting">{t("Version {version} · saved {time}", { version: draft.schemaVersion?.version ?? draft.binding.sourceVersion, time: new Date(draft.at).toLocaleString() })}</Text>
            <Textarea aria-label={t("Saved draft inputs")} readOnly rows={6} value={JSON.stringify(draft.values, null, 2)} />
            <Button disabled={busy} onClick={() => { try { client.prepared?.removeForm(draft.key); refresh(); } catch (problem) { setError(t(String(problem instanceof Error ? problem.message : problem))); } }}>{t("Discard local draft")}</Button>
          </VStack>
        </Disclosure>)}
      </InspectorSection>
    </VStack>
  </Dialog>;
}