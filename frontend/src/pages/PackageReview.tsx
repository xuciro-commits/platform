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
import { PackageReviewContent } from "./PackageReviewContent";

type Mode = "install" | "manage";

export function PackageReview({ view, mode, close, done }: { view: Api.PackageView; mode: Mode; close: () => void; done: () => void }) {
  const id = view.descriptor.id;
  const version = view.descriptor.version;
  const review = useRead<Api.PackageReview>(`/v1/packages/${encodeURIComponent(id)}/review?version=${encodeURIComponent(version)}${mode === "manage" ? "&mode=uninstall" : ""}`);
  const preparation = useRead<Api.PackagePreparation>(mode === "install" ? `/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}` : null);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const installed = view.installed;
  const retired = installed?.state === "retired";
  const named = mode === "install" && !installed && !!view.space;
  const [namespace, setNamespace] = useState(view.space ?? "");
  const actions = catalog.status === "ready" ? catalog.data : [];
  const installSchema = view.rollback ? "platform.package.rollback" : view.update ? "platform.package.upgrade" : view.downgrade ? "platform.package.downgrade" : "platform.package.install";
  const installAction = actions.find((action) => action.schema === installSchema);
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
  const loading = review.status === "loading" || catalog.status === "loading" || (mode === "install" && preparation.status === "loading");
  const state = installed?.state ?? "";
  const primaryLabel = view.rollback
    ? t("Roll back package")
    : view.update
      ? t("Upgrade package")
      : view.downgrade
        ? t("Downgrade package")
        : retired && installed?.version === version
          ? t("Reinstall package")
          : t("Install package");

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
    <Dialog isOpen onOpenChange={close} width={820} purpose="form">
      <Layout
        padding={4}
        header={<DialogHeader title={`${mode === "install" ? t("Review and install") : t("Manage installation")}: ${view.descriptor.title || id}`} onOpenChange={close} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              {failure && <Banner status="error" title={t("Not done")} description={failure} />}
              {loading && <Text type="supporting">{t("Loading…")}</Text>}
              {problems.length > 0 && <Banner status="error" title={t("This version cannot be used as it stands")} description={problems.join(" · ")} />}
              {named && (
                <TextInput label={t("Project API name")} isRequired value={namespace} onChange={setNamespace}
                  description={t("The managed project, its types and its actions take this name. Choose another when this tenant already has a project with it.")} />
              )}
              <PackageReviewContent view={view} mode={mode} review={review} preparation={preparation} />
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
                  label={primaryLabel}
                  variant="primary"
                  isDisabled={busy || loading || !installAction || (!retired && installed?.version === version) || problems.length > 0 || (named && !namespace)}
                  onClick={() => run(installAction, named ? { id, version, namespace } : { id, version })}
                />
              )}
              {mode === "manage" && state !== "draining" && state !== "retired" && (
                <Button label={t("Mark for uninstall")} variant="secondary"
                  isDisabled={busy || loading || !drainAction || review.status !== "ready" || (impact?.dependencies.length ?? 0) > 0}
                  onClick={() => run(drainAction, { id })} />
              )}
              {mode === "manage" && state !== "retired" && (
                <Button label={t("Retire package")} variant="destructive" isDisabled={busy || loading || !retireAction || state !== "draining" || problems.length > 0} onClick={() => run(retireAction, { id })} />
              )}
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
