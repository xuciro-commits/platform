import { credentialSignIn, signIn, type OidcConfig } from "@platform/api";
import { Button, Card, ContentTabs, Input, language, languages, Notice, setLanguage, t, useLanguage } from "@platform/ui";
import { Boxes, Building2, History, KeyRound, Languages, LayoutGrid, Lock, Rocket, ScrollText, ShieldCheck, Workflow } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";

export function SignIn({ config }: { config: OidcConfig }) {
  useLanguage();
  const [tab, setTab] = useState("signin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState("");
  const [pending, setPending] = useState<"" | "sso" | "form">("");
  const next = languages[(languages.findIndex((entry) => entry.id === language()) + 1) % languages.length] ?? { id: "en", name: "English" };
  const features: [ReactNode, string, string][] = [
    [<Boxes />, t("Objects & Relations"), t("Model your business with flexible objects, relationships, and reusable schemas.")],
    [<Workflow />, t("Workflows & Policies"), t("Orchestrate processes, automate tasks, and enforce business policies.")],
    [<LayoutGrid />, t("Runtime Views"), t("Turn models into real applications with tailored views, actions, and data.")],
    [<Building2 />, t("Multi-tenant Governance"), t("Isolate workspaces with strong boundaries, RBAC, and centralized oversight.")],
  ];
  const assurance: [ReactNode, string][] = [[<KeyRound />, t("SSO")], [<ScrollText />, t("Audit")], [<ShieldCheck />, t("RBAC")], [<History />, t("History")], [<Rocket />, t("Activation")]];
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
  const unavailable = t("The identity service is unavailable. Retry, or continue with SSO.");
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
  return <div data-density="comfortable" className="flex min-h-dvh flex-col overflow-x-hidden bg-background text-foreground">
    <header className="mx-auto flex w-full max-w-[1200px] items-center gap-3 px-5 py-4 max-md:px-4">
      <img src="/favicon.svg" alt="" className="size-6" />
      <span className="font-semibold">{t("Platform")}</span>
      <span aria-hidden className="h-4 w-px bg-border" />
      <span className="truncate text-xs text-muted">{t("Build adaptive enterprise systems.")}</span>
      <Button variant="ghost" size="sm" className="ml-auto shrink-0" aria-label={t("Language")} onClick={() => setLanguage(next.id)}><Languages />{next.name}</Button>
    </header>
    <main className="mx-auto flex w-full max-w-[1200px] flex-1 flex-col justify-center gap-8 px-5 py-8 max-md:px-4 lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(380px,420px)] lg:items-center lg:gap-14">
      <section className="order-2 grid min-w-0 content-start gap-6 lg:order-1">
        <h1 className="max-w-2xl text-2xl font-semibold tracking-tight lg:text-[28px] lg:leading-9">{t("One platform for applications, workflows, and operations.")}</h1>
        <p className="max-w-xl text-sm text-muted">{t("Model your enterprise, orchestrate workflows, enforce permissions, and deliver secure, adaptable applications across your organization.")}</p>
        <div className="grid gap-3 sm:grid-cols-2">{features.map(([icon, title, description]) =>
          <Card key={title} className="grid content-start gap-2 p-4">
            <span className="grid size-8 place-items-center rounded-lg bg-primary/10 text-primary [&_svg]:size-4">{icon}</span>
            <span className="font-medium">{title}</span>
            <span className="text-xs text-muted">{description}</span>
          </Card>)}
        </div>
        <ProductFrame />
      </section>
      <Card className="order-1 grid content-start gap-4 p-5 shadow-lg lg:order-2">
        <div className="flex items-center gap-2">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-semibold">{t("Platform")}</span>
        </div>
        <div className="grid gap-1">
          <p className="text-sm font-medium">{t("Build adaptive enterprise systems.")}</p>
          <p className="text-xs text-muted">{t("Sign-in runs in the identity service and supports MFA and passkeys.")}</p>
        </div>
        {problem && <Notice tone="danger" message={problem} />}
        <ContentTabs label={t("Sign in")} value={tab} onChange={setTab} items={[
          { id: "signin", title: t("Sign in"), content: <form className="grid gap-3 pt-2" onSubmit={submit}>
            <Button type="button" variant="primary" size="lg" className="w-full" loading={pending === "sso"} disabled={pending === "form"} onClick={() => void continueWithSso()}><Lock />{t("Continue with SSO")}</Button>
            <p className="flex items-center gap-3 text-2xs text-muted"><span className="h-px flex-1 bg-border" />{t("or")}<span className="h-px flex-1 bg-border" /></p>
            <label className="grid gap-1 text-xs font-medium">{t("Email")}
              <Input type="email" required autoComplete="username" className="h-[var(--ctrl-h-lg)]" value={email} onChange={(event) => setEmail(event.target.value)} placeholder={t("you@company.com")} />
            </label>
            <label className="grid gap-1 text-xs font-medium">{t("Password")}
              <Input type="password" required autoComplete="current-password" className="h-[var(--ctrl-h-lg)]" value={password} onChange={(event) => setPassword(event.target.value)} placeholder={t("Enter your password")} />
            </label>
            <Button type="submit" variant="primary" size="lg" className="w-full" loading={pending === "form"} disabled={pending === "sso"}>{t("Sign in")}</Button>
            <p className="text-2xs text-muted">{t("Accounts with MFA or a passkey continue with SSO.")}</p>
          </form> },
          { id: "access", title: t("Request access"), content: <p className="pt-2 text-sm text-muted">{t("Access is granted by workspace administrators as seats. Sign in with your organization email; if you have no seat yet, the workspace shows the administrators you can contact.")}</p> },
        ]} />
      </Card>
    </main>
    <footer className="border-t border-border bg-surface">
      <div className="mx-auto flex w-full max-w-[1200px] flex-wrap items-center gap-x-6 gap-y-2 px-5 py-4 text-xs text-muted max-md:px-4">
        <span className="font-medium text-foreground">{t("Trusted in enterprise environments")}</span>
        {assurance.map(([icon, label]) => <span key={label} className="flex items-center gap-1.5 [&_svg]:size-3.5">{icon}{label}</span>)}
      </div>
    </footer>
  </div>;
}

function ProductFrame() {
  return <div aria-hidden className="relative hidden lg:block">
    <div className="absolute -top-10 right-6 size-56 rounded-full bg-primary/10" />
    <div className="absolute -left-10 top-32 size-40 rotate-12 rounded-3xl bg-primary/10" />
    <Card className="relative grid w-full max-w-[560px] grid-cols-[124px_minmax(0,1fr)] gap-3 p-3 shadow-lg">
      <div className="grid content-start gap-1 rounded-md bg-canvas p-2 text-2xs">
        <span className="rounded-sm bg-primary/10 px-2 py-1 font-medium text-primary">{t("Workspace")}</span>
        {[t("Models"), t("Workflows"), t("Policies"), t("Applications")].map((label) =>
          <span key={label} className="px-2 py-1">{label}</span>)}
      </div>
      <div className="grid min-w-0 content-start gap-2">
        <Card className="grid gap-2 p-3">
          <span className="text-xs font-medium">{t("Enterprise model")}</span>
          <span className="flex flex-wrap items-center gap-1 text-2xs">
            <span className="rounded-sm border border-border px-1.5 py-0.5">Customer</span>
            <span className="h-px w-3 bg-border" />
            <span className="rounded-sm border border-border px-1.5 py-0.5">Account</span>
            <span className="h-px w-3 bg-border" />
            <span className="rounded-sm bg-primary/10 px-1.5 py-0.5 text-primary">Order</span>
          </span>
        </Card>
        <Card className="grid gap-2 p-3">
          <span className="text-xs font-medium">{t("Workflows")}</span>
          <span className="flex flex-wrap items-center gap-1 text-2xs text-muted">
            <span className="rounded-sm bg-primary/10 px-1.5 py-0.5">Submit</span>
            <span>→</span>
            <span className="rounded-sm bg-primary/10 px-1.5 py-0.5">Review</span>
            <span>→</span>
            <span className="rounded-sm bg-[var(--tone-success)]/15 px-1.5 py-0.5 text-[var(--tone-success)]">Approve</span>
          </span>
        </Card>
        <Card className="grid gap-1.5 p-3">
          <span className="text-xs font-medium">{t("Runtime views")}</span>
          {[0, 1, 2].map((row) =>
            <span key={row} className="flex items-center gap-2">
              <span className="h-2 flex-1 rounded-sm bg-row-hover" />
              <span className="h-2 w-10 rounded-sm bg-[var(--tone-success)]/40" />
            </span>)}
        </Card>
      </div>
    </Card>
  </div>;
}
