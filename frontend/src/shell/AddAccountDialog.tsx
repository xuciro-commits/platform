import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Divider } from "@astryxdesign/core/Divider";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import {
  type Api,
  HostRefusal,
  signInWithPassword,
  startSession,
  startSingleSignOn,
  useAccounts,
  useRead,
} from "../host";
import { t } from "../i18n";

function reasonOf(problem: unknown) {
  if (problem instanceof HostRefusal && (problem.status === 400 || problem.status === 401) && problem.code !== "state") {
    return t("The email or password is not right.");
  }
  return t("Sign-in did not finish. Try again.");
}

export function AddAccountDialog({ onClose }: { onClose: () => void }) {
  const options = useRead<Api.SignIn>("/v1/sign-in");
  const accounts = useAccounts();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const sign = options.status === "ready" ? options.data : undefined;
  const existing = new Set(accounts.map((account) => account.token));

  const submit = async () => {
    if (!sign || !email || !password || busy) return;
    setBusy(true);
    setFailure("");
    try {
      startSession(await signInWithPassword(sign, email, password));
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => { if (!open) onClose(); }} purpose="form" width={420} aria-label={t("Add account")}>
      <VStack gap={4} hAlign="stretch">
        <DialogHeader title={t("Add account")} onOpenChange={(open) => { if (!open) onClose(); }} />
        {options.status === "loading" && <Skeleton height={44} />}
        {(options.status === "error" || options.status === "refused") && (
          <Banner status="error" title={t("The platform could not be reached")} description={options.reason} />
        )}
        {sign?.issuer && (
          <VStack gap={3} hAlign="stretch">
            <TextInput
              label={t("Email")}
              type="email"
              autoComplete="username"
              placeholder={t("name@company.com")}
              value={email}
              onChange={setEmail}
            />
            <TextInput
              label={t("Password")}
              type="password"
              autoComplete="current-password"
              placeholder={t("Password")}
              value={password}
              onChange={(next: string) => { setPassword(next); setFailure(""); }}
              onKeyDown={(event) => { if (event.key === "Enter") void submit(); }}
              status={failure ? { type: "error", message: failure } : undefined}
            />
            <HStack gap={2} hAlign="end">
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Sign in")} variant="primary" isLoading={busy} isDisabled={!email || !password} onClick={() => void submit()} />
            </HStack>
            <Divider label={t("Or")} />
            <Button label={t("Continue with single sign-on")} variant="secondary" onClick={() => void startSingleSignOn(sign)} />
          </VStack>
        )}
        {sign && !sign.issuer && (
          <VStack gap={2} hAlign="stretch">
            <Text type="supporting" color="secondary">{t("Choose an account to add to this session.")}</Text>
            {(sign.identities ?? []).map((identity) => (
              <Button
                key={`${identity.tenant}/${identity.token}`}
                label={existing.has(identity.token) ? `${identity.member} · ${identity.tenant} (${t("Signed in")})` : `${identity.member} · ${identity.tenant}`}
                variant="secondary"
                isDisabled={existing.has(identity.token)}
                onClick={() => {
                  startSession({ token: identity.token, tenant: identity.tenant });
                  onClose();
                }}
              />
            ))}
          </VStack>
        )}
      </VStack>
    </Dialog>
  );
}
