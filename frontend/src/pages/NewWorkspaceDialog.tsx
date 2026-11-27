import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { useState } from "react";
import { type Api, reread, request, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf } from "./act";

const validId = /^[a-z][a-z0-9-]{1,31}$/;

function adminSubject(raw: string) {
  const trimmed = raw.trim();
  if (!trimmed) return "";
  return trimmed.startsWith("user:") || trimmed.startsWith("client:") ? trimmed : `user:${trimmed}`;
}

export function NewWorkspaceDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const templates = useRead<Api.TenantTemplate[]>("/v1/host/templates");
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [template, setTemplate] = useState("");
  const [admin, setAdmin] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const offered = templates.status === "ready" ? templates.data : [];
  const chosen = template || offered[0]?.name || "";
  const subject = adminSubject(admin);
  const idProblem = id && !validId.test(id) ? t("Use 2 to 32 lower-case letters, digits and dashes, starting with a letter.") : undefined;
  const canCreate = validId.test(id) && Boolean(subject) && !busy;

  const create = async () => {
    if (!canCreate) return;
    setBusy(true);
    setFailure("");
    try {
      const body: Api.CreateTenantRequest = { id, name: name.trim() || id, admin: subject, ...(chosen ? { template: chosen } : {}) };
      await request<Api.HostTenantView>("/v1/host/tenants", { body });
      reread();
      toast({ body: t("Workspace {id} created.").replace("{id}", id) });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={560} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("New workspace")} onOpenChange={(open) => !open && onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("A new workspace starts from a template with one administrator, who can then install apps and invite colleagues.")}
              </Text>
              {failure && <Banner status="error" title={t("Not created")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <TextInput label={t("Workspace ID")} isRequired value={id} onChange={(value) => setId(value.trim().toLowerCase())} status={idProblem ? { type: "error", message: idProblem } : undefined} />
                <TextInput label={t("Name")} value={name} onChange={setName} />
                {offered.length > 0 && (
                  <Selector
                    label={t("Template")}
                    value={chosen}
                    options={offered.map((entry) => ({ value: entry.name, label: t(entry.title || entry.name) }))}
                    onChange={setTemplate}
                  />
                )}
                <TextInput label={t("First administrator")} placeholder={t("Email")} isRequired value={admin} onChange={setAdmin} />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Create")} variant="primary" isDisabled={!canCreate} isLoading={busy} onClick={() => void create()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
