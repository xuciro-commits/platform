import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { Divider } from "@astryxdesign/core/Divider";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Section } from "@astryxdesign/core/Section";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { CubeIcon, EnvelopeIcon } from "@heroicons/react/24/outline";
import { useState } from "react";
import {
  enterVerifiedSession,
  reasonOf,
  requestEmailCode,
  verifyEmailCode,
} from "../host";
import { t } from "../i18n";
import { navigate, useSearch } from "../router";

function isValidEmail(email: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
}

type Step = "email" | "verify";

export function SignUpPage({ mode: fixedMode }: { mode?: "signup" | "signin" }) {
  const search = useSearch();
  const mode = fixedMode ?? (new URLSearchParams(search).get("mode") === "signin" ? "signin" : "signup");
  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const emailValid = isValidEmail(email);

  const sendCode = async () => {
    if (!emailValid || busy) return;
    setBusy(true);
    setFailure("");
    try {
      await requestEmailCode(email.trim());
      setStep("verify");
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const verify = async () => {
    const clean = code.trim();
    if (!emailValid || clean.length !== 6 || busy) return;
    setBusy(true);
    setFailure("");
    try {
      const answer = await verifyEmailCode(email.trim(), clean);
      enterVerifiedSession(answer);
      void navigate("/", null, true);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const resetEmail = () => {
    setStep("email");
    setCode("");
    setFailure("");
  };

  return (
    <Center axis="both" padding={6} minHeight="100dvh">
      <Card padding={8} width="100%" maxWidth={420}>
        <VStack gap={5} hAlign="stretch">
          <HStack gap={2} vAlign="center">
            <Icon icon={CubeIcon} />
            <Link href="/welcome" isStandalone color="primary" weight="bold">{t("Platform")}</Link>
          </HStack>
          {step === "email" ? (
            <VStack gap={4} hAlign="stretch">
              <VStack gap={1}>
                <Heading level={1}>{mode === "signin" ? t("Sign in with email") : t("Create your account")}</Heading>
                <Text type="body" color="secondary" size="sm">
                  {t("Enter your work email to receive a one-time verification code.")}
                </Text>
              </VStack>
              {failure && <Banner status="error" title={failure} />}
              <TextInput
                label={t("Work email")}
                isLabelHidden
                type="email"
                autoComplete="email"
                placeholder={t("name@company.com")}
                value={email}
                onChange={(next: string) => { setEmail(next); setFailure(""); }}
                size="lg"
                onKeyDown={(event) => { if (event.key === "Enter") void sendCode(); }}
              />
              <Button
                label={t("Send verification code")}
                variant="primary"
                size="lg"
                isLoading={busy}
                isDisabled={!emailValid}
                onClick={() => void sendCode()}
              />
              <Divider />
              <VStack hAlign="center">
                <Text type="supporting" color="secondary">
                  {mode === "signin" ? t("New to Platform?") : t("Already have an account?")}{" "}
                  <Link href={mode === "signin" ? "/signup" : "/sign-in"} type="supporting">
                    {mode === "signin" ? t("Create account") : t("Sign in")}
                  </Link>
                </Text>
              </VStack>
            </VStack>
          ) : (
            <VStack gap={4} hAlign="stretch">
              <VStack gap={1}>
                <Heading level={1}>{t("Check your email")}</Heading>
                <Text type="body" color="secondary" size="sm">
                  {t("Enter the 6-digit code sent to {email}. Codes expire in 10 minutes.").replace("{email}", email.trim().toLowerCase())}
                </Text>
              </VStack>
              <Card padding={0}>
                <Section variant="muted" padding={3}>
                  <HStack gap={2} vAlign="center">
                    <Icon icon={EnvelopeIcon} color="secondary" />
                    <Text type="supporting">{email.trim().toLowerCase()}</Text>
                  </HStack>
                </Section>
              </Card>
              {failure && <Banner status="error" title={failure} />}
              <TextInput
                label={t("Verification code")}
                isLabelHidden
                placeholder="000000"
                value={code}
                onChange={(next: string) => { setCode(next.replace(/\D+/g, "").slice(0, 6)); setFailure(""); }}
                size="lg"
                onKeyDown={(event) => { if (event.key === "Enter") void verify(); }}
              />
              <VStack gap={2} hAlign="stretch">
                <Button
                  label={t("Verify and continue")}
                  variant="primary"
                  size="lg"
                  isLoading={busy}
                  isDisabled={code.trim().length !== 6}
                  onClick={() => void verify()}
                />
                <Button label={t("Resend code")} variant="secondary" size="lg" isDisabled={busy} onClick={() => void sendCode()} />
                <Button label={t("Use a different email")} variant="ghost" size="lg" isDisabled={busy} onClick={resetEmail} />
              </VStack>
            </VStack>
          )}
        </VStack>
      </Card>
    </Center>
  );
}
