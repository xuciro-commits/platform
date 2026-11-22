import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";

type Mode = "install" | "manage";

export function PackageReview({ view, mode, close, done }: { view: Api.PackageView; mode: Mode; close: () => void; done: () => void }) {
  const id = view.descriptor.id;
  const version = view.descriptor.version;
  const review = useRead<Api.PackageReview>(`/v1/packages/${encodeURIComponent(id)}/review?version=${encodeURIComponent(version)}${mode === "manage" ? "&mode=uninstall" : ""}`);
  const preparation = useRead<Api.PackagePreparation>(mode === "install" ? `/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}` : null);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const named = mode === "install" && !view.installed && !!view.space;
  const [namespace, setNamespace] = useState(view.space ?? "");
  const actions = catalog.status === "ready" ? catalog.data : [];
  const installAction = actions.find((action) => action.schema === (view.rollback ? "platform.package.rollback" : view.update ? "platform.package.upgrade" : view.downgrade ? "platform.package.downgrade" : "platform.package.install"));
  const drainAction = actions.find((action) => action.schema === "platform.package.drain");
  const retireAction = actions.find((action) => action.schema === "platform.package.retire");
  const problems = [
    ...(mode === "install" ? [...(view.plan.problems ?? []), ...(view.precheck.problems ?? [])] : []),
    ...(review.status === "ready" ? review.data.problems ?? [] : review.status === "loading" ? [] : [review.reason]),
    ...(mode === "install" && (preparation.status === "error" || preparation.status === "refused") ? [preparation.reason] : []),
    ...(preparation.status === "ready" ? preparation.data.connections.filter((connection) => !connection.ready).map((connection) => `${connection.title}: ${t("Not configured")}`) : []),
    ...(preparation.status === "ready" ? preparation.data.ai.filter((requirement) => requirement.problem).map((requirement) => requirement.problem!) : []),
  ];
  const impact = review.status === "ready" ? review.data.impact : undefined;
  const installed = view.installed;
  const loading = review.status === "loading" || catalog.status === "loading" || (mode === "install" && preparation.status === "loading");
  const state = installed?.state ?? "";

  const run = async (action: Api.Action | undefined, payload: Record<string, unknown>) => {
    if (!action) return;
    setBusy(true);
    setFailure("");
    try {
      await send(action, id, payload);
      reread();
      done();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={close} width={760} purpose="form">
      <Layout
        padding={4}
        header={<DialogHeader title={`${mode === "install" ? t("Review and install") : t("Manage installation")}: ${view.descriptor.title || id}`} onOpenChange={close} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              {failure && <Banner status="error" title={t("Not done")} description={failure} />}
              {loading && <Text type="supporting">{t("Loading…")}</Text>}
              <Text type="body">{id} · {version}</Text>
              {view.descriptor.description && <Text type="body" color="secondary">{view.descriptor.description}</Text>}
              {problems.length > 0 && <Banner status="error" title={t("This version cannot be used as it stands")} description={problems.join(" · ")} />}
              {named && (
                <TextInput label={t("Project API name")} isRequired value={namespace} onChange={setNamespace}
                  description={t("The managed project, its types and its actions take this name. Choose another when this tenant already has a project with it.")} />
              )}
              {mode === "install" && review.status === "ready" && (
                <VStack gap={2}>
                  <Text type="body" weight="bold">{t("Installation scope")}</Text>
                  <Text type="body">{t("Applications")}: {preparation.status === "ready" && preparation.data.applications.length ? preparation.data.applications.map((app) => app.title || app.ref.name).join(", ") : review.data.changes.filter((change) => change.ref.kind === "app").length}</Text>
                  <Text type="body">{t("Dependencies")}: {impact?.dependencies.length ? impact.dependencies.join(", ") : t("None")}</Text>
                  <Text type="body">{t("Roles")}: {preparation.status === "ready" && preparation.data.roles.length ? preparation.data.roles.map((entry) => entry.role).join(", ") : t("None")}</Text>
                  <Text type="body">{t("Connections")}: {impact?.connections.length ?? 0}</Text>
                  <Text type="body">{t("Existing records affected")}: {impact?.outputs.reduce((sum, output) => sum + output.records, 0) ?? 0}</Text>
                </VStack>
              )}
              {mode === "manage" && (
                <VStack gap={2}>
                  <Text type="body" weight="bold">{t("Lifecycle")}</Text>
                  <Text type="body">{t("Installed version")}: {installed?.version ?? t("Not installed")}</Text>
                  <Text type="body">{t("State")}: {state || t("Unknown")}</Text>
                  <Text type="body">{t("References retained")}: {impact?.references.length ?? 0}</Text>
                  <Text type="body">{t("Business records retained")}: {impact?.outputs.reduce((sum, output) => sum + (output.retained ? output.records : 0), 0) ?? 0}</Text>
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={close} />
              {mode === "install" && (
                <Button
                  label={t("Install package")}
                  variant="primary"
                  isDisabled={busy || loading || !installAction || view.installed?.version === version || problems.length > 0 || (named && !namespace)}
                  onClick={() => run(installAction, named ? { id, version, namespace } : { id, version })}
                />
              )}
              {mode === "manage" && state !== "draining" && state !== "retired" && (
                <Button label={t("Mark for uninstall")} variant="secondary"
                  isDisabled={busy || loading || !drainAction || review.status !== "ready" || (impact?.dependencies.length ?? 0) > 0}
                  onClick={() => run(drainAction, { id })} />
              )}
              {mode === "manage" && state !== "retired" && (
                <Button label={t("Retire package")} variant="destructive" isDisabled={busy || loading || !retireAction || problems.length > 0} onClick={() => run(retireAction, { id })} />
              )}
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
