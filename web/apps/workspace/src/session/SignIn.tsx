import { credentialSignIn, signIn, type OidcConfig } from "@platform/api";
import { Button, ContentTabs, Input, Notice, languages, setLanguage, t, useLanguage } from "@platform/ui";
import { Card } from "@astryxdesign/core/Card";
import { Divider } from "@astryxdesign/core/Divider";
import { Grid } from "@astryxdesign/core/Grid";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { Boxes, Building2, History, KeyRound, Languages, LayoutGrid, Lock, Rocket, ScrollText, ShieldCheck, Workflow } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";

export function SignIn({ config }: { config: OidcConfig }) {
  const spoken = useLanguage();
  const [tab, setTab] = useState("signin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState("");
  const [pending, setPending] = useState<"" | "sso" | "form">("");
  const next = languages[(languages.findIndex((entry) => entry.id === spoken) + 1) % languages.length]!;
  const unavailable = t("The identity service is unavailable. Retry, or continue with SSO.");
  const features: [ReactNode, string, string][] = [
    [<Icon icon={Boxes} />, t("Objects & Relations"), t("Model every business entity once and reuse it across applications.")],
    [<Icon icon={Workflow} />, t("Workflows & Policies"), t("Turn work into guarded transitions with clear ownership and audit.")],
    [<Icon icon={LayoutGrid} />, t("Runtime Views"), t("Build the shared views your teams use every day.")],
    [<Icon icon={Building2} />, t("Multi-tenant Governance"), t("Keep every tenant, project and permission boundary explicit.")],
  ];
  const assurance: [ReactNode, string][] = [[<Icon icon={KeyRound} />, t("SSO")], [<Icon icon={ScrollText} />, t("Audit")], [<Icon icon={ShieldCheck} />, t("RBAC")], [<Icon icon={History} />, t("History")], [<Icon icon={Rocket} />, t("Activation")]];
  const continueWithSso = async () => {
    setPending("sso");
    setProblem("");
    try {
      await signIn(config, email.trim() ? { hint: email.trim() } : undefined);
    } catch {
      setProblem(unavailable);
      setPending("");
    }
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setPending("form");
    setProblem("");
    try {
      const result = await credentialSignIn(config, email.trim(), password);
      if (result.session) return location.reload();
      setProblem(result.problem === "credentials"
        ? t("The email or password does not match an account in the identity service. Use SSO if the account has MFA or a passkey.")
        : unavailable);
    } catch {
      setProblem(unavailable);
    }
    setPending("");
  };
  return <VStack gap={0} height="100dvh">
    <HStack gap={3} padding={4} hAlign="center">
      <HStack gap={2} hAlign="between" maxWidth={1200} width="100%">
        <HStack gap={2}>
          <img src="/favicon.svg" alt="" width={24} height={24} />
          <Text weight="semibold">{t("Platform")}</Text>
          <Divider orientation="vertical" />
          <Text type="supporting" maxLines={1}>{t("Build adaptive enterprise systems.")}</Text>
        </HStack>
        <Button variant="ghost" size="sm" aria-label={t("Language")} onClick={() => setLanguage(next.id)}><Icon icon={Languages} />{next.name}</Button>
      </HStack>
    </HStack>
    <VStack isScrollable gap={0}>
      <HStack hAlign="center" paddingBlock={6} paddingInline={6}>
        <Grid columns={{ minWidth: 360, max: 2 }} gap={8} width="100%" maxWidth={1200} align="center">
          <VStack gap={6} justify="center">
            <VStack gap={3}>
              <Heading level={1} type="display-3">{t("One platform for applications, workflows, and operations.")}</Heading>
              <Text type="body">{t("Model your enterprise, orchestrate workflows, enforce permissions, and deliver secure, adaptable applications across your organization.")}</Text>
            </VStack>
            <Grid columns={{ minWidth: 220, max: 2 }} gap={3}>
              {features.map(([icon, title, description]) => <Card key={title} padding={4}>
                <VStack gap={2}><Text color="accent">{icon}</Text><Text weight="medium">{title}</Text><Text type="supporting">{description}</Text></VStack>
              </Card>)}
            </Grid>
            <ProductFrame />
          </VStack>
          <Card padding={5}>
            <VStack gap={4}>
              <HStack gap={2}><img src="/favicon.svg" alt="" width={24} height={24} /><Text weight="semibold">{t("Platform")}</Text></HStack>
              <VStack gap={1}><Text weight="medium">{t("Build adaptive enterprise systems.")}</Text><Text type="supporting">{t("Sign-in runs in the identity service and supports MFA and passkeys.")}</Text></VStack>
              {problem && <Notice tone="danger" message={problem} />}
              <ContentTabs label={t("Sign in")} value={tab} onChange={setTab} items={[
                { id: "signin", title: t("Sign in"), content: <form onSubmit={submit}>
                  <VStack gap={3}>
                    <Button variant="primary" width="100%" loading={pending === "sso"} disabled={pending !== ""} onClick={() => void continueWithSso()}><Icon icon={Lock} />{t("Continue with SSO")}</Button>
                    <HStack gap={2} vAlign="center"><Divider /><Text type="supporting">{t("or")}</Text><Divider /></HStack>
                    <label><VStack gap={1}>
                      <Text type="label" weight="medium">{t("Email")}</Text>
                      <Input type="email" required autoComplete="username" value={email} placeholder={t("you@company.com")} onChange={(event) => setEmail(event.target.value)} />
                    </VStack></label>
                    <label><VStack gap={1}>
                      <Text type="label" weight="medium">{t("Password")}</Text>
                      <Input type="password" required autoComplete="current-password" value={password} placeholder={t("Enter your password")} onChange={(event) => setPassword(event.target.value)} />
                    </VStack></label>
                    <Button type="submit" variant="primary" width="100%" loading={pending === "form"} disabled={pending !== ""}>{t("Sign in")}</Button>
                    <Text type="supporting">{t("Accounts with MFA or a passkey continue with SSO.")}</Text>
                  </VStack>
                </form> },
                { id: "access", title: t("Request access"), content: <VStack gap={2}>
                  <Text type="body">{t("Access is granted by workspace administrators as seats. Sign in with your organization email; if you have no seat yet, the workspace shows the administrators you can contact.")}</Text>
                </VStack> },
              ]} />
            </VStack>
          </Card>
        </Grid>
      </HStack>
      <HStack gap={3} padding={4} wrap="wrap" hAlign="center">
        {assurance.map(([icon, label]) => <HStack key={label} gap={1}>{icon}<Text type="supporting">{label}</Text></HStack>)}
      </HStack>
    </VStack>
  </VStack>;
}

function ProductFrame() {
  return <Card variant="muted" padding={3}>
    <VStack gap={2}>
      <Text type="label" weight="semibold">{t("Workspace")}</Text>
      <Grid columns={{ minWidth: 150, max: 3 }} gap={2}>
        <Card padding={2}><VStack gap={1}><Text type="label">{t("Enterprise model")}</Text><Text type="supporting">Customer → Account → Order</Text></VStack></Card>
        <Card padding={2}><VStack gap={1}><Text type="label">{t("Workflows")}</Text><Text type="supporting">Submit → Review → Approve</Text></VStack></Card>
        <Card padding={2}><VStack gap={1}><Text type="label">{t("Runtime views")}</Text><Text type="supporting">{t("Applications")}</Text></VStack></Card>
      </Grid>
    </VStack>
  </Card>;
}