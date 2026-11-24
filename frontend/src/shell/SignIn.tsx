import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { Divider } from "@astryxdesign/core/Divider";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Section } from "@astryxdesign/core/Section";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { radiusVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { CubeIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useRef, useState } from "react";
import {
  type Api, HostRefusal, finishSingleSignOn, returningFromSingleSignOn, signInWithPassword, startSession, startSingleSignOn, useRead,
} from "../host";
import { t } from "../i18n";
import { navigate } from "../router";

const stacked = "@container (max-width: 700px)";

const styles = stylex.create({
  grid: {
    containerType: "inline-size",
    padding: { default: spacingVars["--spacing-8"], [stacked]: spacingVars["--spacing-4"] },
  },
  form: { order: 0, minHeight: { default: 620, [stacked]: "auto" } },
  photo: {
    order: 1,
    overflow: "hidden",
    minHeight: 620,
    height: "min(76dvh, 720px)",
    borderRadius: radiusVars["--radius-container"],
    backgroundImage: "url(/media/sign-in.avif)",
    backgroundSize: "cover",
    backgroundPosition: "center",
    display: { default: "block", [stacked]: "none" },
  },
});

function reasonOf(problem: unknown) {
  if (problem instanceof HostRefusal && (problem.status === 400 || problem.status === 401) && problem.code !== "state") {
    return t("The email or password is not right.");
  }
  return t("Sign-in did not finish. Try again.");
}

function Credentials({ sign }: { sign: Api.SignIn }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!email || !password || busy) return;
    setBusy(true);
    setFailure("");
    try {
      startSession(await signInWithPassword(sign, email, password));
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };
  return (
    <VStack gap={4} hAlign="stretch" width="100%">
      <VStack gap={2}>
        <TextInput label={t("Email")} isLabelHidden type="email" autoComplete="username" placeholder={t("name@company.com")}
          value={email} onChange={setEmail} size="lg" />
        <TextInput label={t("Password")} isLabelHidden type="password" autoComplete="current-password" placeholder={t("Password")}
          value={password} onChange={(next: string) => { setPassword(next); setFailure(""); }} size="lg"
          onKeyDown={(event) => { if (event.key === "Enter") void submit(); }}
          status={failure ? { type: "error", message: failure } : undefined} />
      </VStack>
      <Button label={t("Sign in")} variant="primary" size="lg" isLoading={busy} isDisabled={!email || !password} onClick={submit} />
      <Divider label={t("Or")} />
      <Button label={t("Continue with single sign-on")} variant="secondary" size="lg" onClick={() => void startSingleSignOn(sign)} />
    </VStack>
  );
}

function Returning({ sign }: { sign: Api.SignIn }) {
  const [failure, setFailure] = useState("");
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    finishSingleSignOn(sign).then(({ session, back }) => {
      navigate(back, null, true);
      startSession(session);
    }, (problem: unknown) => {
      navigate("/", null, true);
      setFailure(reasonOf(problem));
    });
  }, [sign]);
  if (failure) return <Banner status="error" title={failure} />;
  return <HStack gap={2} vAlign="center"><Spinner size="sm" /><Text type="body" color="secondary">{t("Signing you in…")}</Text></HStack>;
}

export function SignIn() {
  const options = useRead<Api.SignIn>("/v1/sign-in");
  const [returning] = useState(returningFromSingleSignOn);
  const sign = options.status === "ready" ? options.data : undefined;
  return (
    <Center axis="both" padding={6} minHeight="100dvh">
      <Card padding={0} width="100%" maxWidth={1200}>
        <Grid columns={{ minWidth: 260, repeat: "fit" }} gap={8} align="stretch" xstyle={styles.grid}>
          <Section variant="transparent" padding={0} height="100%" xstyle={styles.form}>
            <VStack gap={5} height="100%" hAlign="stretch">
              <HStack gap={2} vAlign="center">
                <Icon icon={CubeIcon} />
                <Text type="body" weight="bold">{t("Platform")}</Text>
              </HStack>
              <StackItem size="fill">
                <Center axis="vertical" height="100%">
                  <VStack gap={5} hAlign="stretch" width="100%">
                    <VStack gap={1}>
                      <Text type="display-1" as="h1">{t("Welcome back")}</Text>
                      <Text type="body" color="secondary" size="sm">
                        {sign?.issuer ? t("Sign in to your Platform workspace") : t("Choose how to continue")}
                      </Text>
                    </VStack>
                    {options.status === "loading" && <Skeleton height={44} />}
                    {(options.status === "error" || options.status === "refused") && (
                      <Banner status="error" title={t("The platform could not be reached")} description={options.reason} />
                    )}
                    {sign?.issuer && (returning ? <Returning sign={sign} /> : <Credentials sign={sign} />)}
                    {sign && !sign.issuer && (
                      <VStack gap={3} hAlign="stretch">
                        {[...new Map((sign.identities ?? []).map((identity) => [identity.token, identity])).values()].map((identity) => (
                          <Button key={identity.token} label={identity.member}
                            variant="secondary" size="lg" onClick={() => startSession({ token: identity.token, tenant: identity.tenant })} />
                        ))}
                      </VStack>
                    )}
                  </VStack>
                </Center>
              </StackItem>
              <Text type="supporting" color="secondary">{t("Your workspace, your data, your control.")}</Text>
            </VStack>
          </Section>
          <Card variant="transparent" padding={0} xstyle={styles.photo} />
        </Grid>
      </Card>
    </Center>
  );
}
