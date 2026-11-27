import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, request, reread, reasonOf, send } from "../host";
import { t } from "../i18n";

export function InlineCredentialDialog({
  saveAction,
  kind,
  consumer,
  origin,
  managedAvailable,
  onClose,
  onCreated,
}: {
  saveAction: Api.Action;
  kind: string;
  consumer: string;
  origin: string;
  managedAvailable: boolean;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [source, setSource] = useState<"managed" | "deployment">(managedAvailable ? "managed" : "deployment");
  const [secretValue, setSecretValue] = useState("");
  const [reference, setReference] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const cleanId = id.trim();
  const cleanTitle = title.trim();
  const ready = Boolean(
    cleanId && cleanTitle && origin && (source === "managed" ? secretValue.trim() : reference.trim()),
  );

  const submit = async () => {
    if (!ready || busy) return;
    setBusy(true);
    setFailure("");
    try {
      const base = {
        id: cleanId,
        title: cleanTitle,
        kind: kind || "bearer",
        source,
        bindings: [{ consumer, origin }],
        expectedVersion: 0,
      };
      const payload = source === "managed"
        ? await request<Record<string, unknown>>("/v1/credentials/seal", {
          body: { ...base, value: secretValue.trim() },
        })
        : { ...base, reference: reference.trim() };
      await send(saveAction, cleanId, payload);
      reread();
      onCreated(cleanId);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={onClose} width={540} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("New credential")} onOpenChange={onClose} />}
        content={
          <LayoutContent>
            <FormLayout defaultOptionality="optional">
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <Text type="supporting" color="secondary">
                {t("Bound to {consumer} at {origin}.").replace("{consumer}", consumer).replace("{origin}", origin)}
              </Text>
              <TextInput
                label={t("Credential ID")}
                isRequired
                placeholder={t("e.g. crm-gateway-token")}
                value={id}
                onChange={setId}
              />
              <TextInput
                label={t("Title")}
                isRequired
                placeholder={t("Connector credential")}
                value={title}
                onChange={setTitle}
              />
              <SegmentedControl
                label={t("Source")}
                value={source}
                onChange={(next) => setSource(next as "managed" | "deployment")}
              >
                <SegmentedControlItem value="managed" label={t("Managed secret")} isDisabled={!managedAvailable} />
                <SegmentedControlItem value="deployment" label={t("Deployment reference")} />
              </SegmentedControl>
              {source === "managed" ? (
                <TextInput
                  label={t("Secret value")}
                  isRequired
                  type="password"
                  value={secretValue}
                  onChange={setSecretValue}
                  description={t("Encrypted on the server before saving; never stored in plaintext.")}
                />
              ) : (
                <TextInput
                  label={t("Reference")}
                  isRequired
                  placeholder={t("e.g. ENV_SECRET_NAME")}
                  value={reference}
                  onChange={setReference}
                  description={t("Name of the secret provided by the host deployment environment.")}
                />
              )}
            </FormLayout>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Save credential")} variant="primary" isDisabled={!ready} isLoading={busy} onClick={() => void submit()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
