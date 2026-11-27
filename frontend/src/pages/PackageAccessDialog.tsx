import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { useMemo, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { navigate } from "../router";
import { reasonOf, send } from "./act";
import { roleTitle } from "./rebac/shared";

type Unheld = { app: string; title: string; roles: string[] };

function spaceOf(view: Api.PackageView) {
  return view.installed?.space || view.installed?.namespace || view.descriptor.namespace;
}

export function PackageAccessDialog({ ids, onClose }: { ids: string[]; onClose: () => void }) {
  const toast = useToast();
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const members = useRead<Api.MemberView[]>("/v1/members");
  const me = useRead<Api.MeView>("/v1/me");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [people, setPeople] = useState<string[] | null>(null);
  const [picked, setPicked] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const grant = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.member.grant") : undefined;
  const spaces = useMemo(() => {
    const out = new Set<string>();
    if (packages.status === "ready") {
      for (const view of packages.data) {
        const space = spaceOf(view);
        if (ids.includes(view.descriptor.id) && view.installed && space) out.add(space);
      }
    }
    return out;
  }, [packages, ids]);
  const unheld = useMemo<Unheld[]>(() => {
    if (permissions.status !== "ready") return [];
    return permissions.data
      .filter((entry) => spaces.has(entry.app))
      .map((entry) => ({ app: entry.app, title: t(entry.title || entry.app), roles: entry.roles.filter((item) => item.role !== "*" && item.holders === 0).map((item) => item.role) }))
      .filter((entry) => entry.roles.length > 0);
  }, [permissions, spaces]);
  const settled = [packages, permissions, members, me, catalog].every((read) => read.status !== "loading");

  if (!settled || unheld.length === 0) return null;

  const self = me.status === "ready" ? me.data.principalId : "";
  const chosen = people ?? (self ? [self] : []);
  const roleFor = (entry: Unheld) => picked[entry.app] ?? entry.roles[0];
  const names = unheld.map((entry) => entry.title).join(", ");
  const options = members.status === "ready"
    ? members.data
      .filter((member) => !member.agent && member.status !== "left")
      .map((member) => ({ value: member.id, label: `${member.profile.displayName || member.id}${member.id === self ? ` (${t("you")})` : ""}` }))
    : [];
  const grants = unheld.filter((entry) => roleFor(entry) !== "");
  const canGrant = Boolean(grant) && chosen.length > 0 && grants.length > 0 && !busy;

  const skip = () => {
    toast({ body: t("{apps} stays hidden from anyone without a role. Grant roles later under Admin → Members.").replace("{apps}", names) });
    onClose();
  };

  const grantAll = async () => {
    if (!grant || !canGrant) return;
    setBusy(true);
    setFailure("");
    try {
      for (const member of chosen) {
        for (const entry of grants) await send(grant, member, { app: entry.app, role: roleFor(entry) });
      }
      reread();
      toast({ body: t("Granted {apps} to {count} people.").replace("{apps}", names).replace("{count}", String(chosen.length)) });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && !busy && skip()} width={560} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("Who can use it")} onOpenChange={(open) => !open && !busy && skip()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("Installing grants no access. Nobody holds a role in {apps} yet, so it stays hidden from everyone, you included, until someone gets one.").replace("{apps}", names)}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              {grant ? (
                <FormLayout defaultOptionality="optional">
                  <MultiSelector label={t("People")} value={chosen} options={options} onChange={setPeople} hasSearch triggerDisplay="badges" />
                  {unheld.map((entry) => (
                    <Selector
                      key={entry.app}
                      label={t("Role in {app}").replace("{app}", entry.title)}
                      value={roleFor(entry)}
                      options={[...entry.roles.map((role) => ({ value: role, label: roleTitle(role) })), { value: "", label: t("No role") }]}
                      onChange={(role) => setPicked((previous) => ({ ...previous, [entry.app]: role }))}
                    />
                  ))}
                </FormLayout>
              ) : (
                <Banner
                  status="info"
                  title={t("You cannot grant roles")}
                  description={t("Ask a workspace administrator to grant a role in {apps} under Admin → Members.").replace("{apps}", names)}
                />
              )}
              {grant && (
                <HStack gap={2} vAlign="center" wrap="wrap">
                  <Text type="supporting" color="secondary">{t("You can skip this and grant roles later under Admin → Members.")}</Text>
                  <Button label={t("Open members")} variant="ghost" size="sm" isDisabled={busy} onClick={() => { onClose(); navigate("/admin/members"); }} />
                </HStack>
              )}
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              {grant ? (
                <>
                  <Button label={t("Skip for now")} variant="secondary" isDisabled={busy} onClick={skip} />
                  <Button label={t("Grant access")} variant="primary" isDisabled={!canGrant} isLoading={busy} onClick={() => void grantAll()} />
                </>
              ) : (
                <Button label={t("Done")} variant="primary" onClick={onClose} />
              )}
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
