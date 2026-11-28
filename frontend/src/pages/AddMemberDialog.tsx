import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { useEffect, useState } from "react";
import { type Api, reasonOf, reread, send, useRead, workspaceMemberAccounts } from "../host";
import { currentSession, requireSession } from "../host/session";
import { t } from "../i18n";

type Account = { subject: string; name: string };
type Outcome = { subject: string; added: boolean; reason?: string };

function accountsOf(members: Api.MemberView[]) {
  const accounts = new Map<string, Account>();
  for (const member of members) {
    if (member.agent || (member.status && member.status !== "active")) continue;
    for (const subject of member.subjects) {
      if (subject.startsWith("user:")) accounts.set(subject, { subject, name: member.profile?.displayName || member.id });
    }
  }
  return [...accounts.values()].sort((a, b) => a.name.localeCompare(b.name) || a.subject.localeCompare(b.subject));
}

export function AddMemberDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const members = useRead<Api.MemberView[]>("/v1/members");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [source, setSource] = useState("");
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [sourceError, setSourceError] = useState("");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [outcomes, setOutcomes] = useState<Outcome[]>([]);
  const [completed, setCompleted] = useState(false);
  const other = me.status === "ready" ? me.data.tenants.filter((tenant) => tenant.id !== me.data.tenantId) : [];
  const target = members.status === "ready" ? members.data : [];
  const existing = new Set(target.filter((member) => member.status !== "left").flatMap((member) => member.subjects));
  const actions = catalog.status === "ready" ? catalog.data : [];
  const add = actions.find((action) => action.schema === "platform.member.add");
  const profile = actions.find((action) => action.schema === "platform.profile.update");

  useEffect(() => {
    setAccounts([]);
    setSelected([]);
    setQuery("");
    setSourceError("");
    if (!source) { setLoading(false); return; }
    const controller = new AbortController();
    setLoading(true);
    workspaceMemberAccounts(source, controller.signal).then(
      (answer) => { if (!controller.signal.aborted) { setAccounts(accountsOf(answer)); setLoading(false); } },
      (problem: unknown) => { if (!controller.signal.aborted) { setSourceError(reasonOf(problem)); setLoading(false); } },
    );
    return () => controller.abort();
  }, [source]);

  const needle = query.trim().toLowerCase();
  const available = accounts.filter((account) => !existing.has(account.subject));
  const visible = available.filter((account) => `${account.name} ${account.subject}`.toLowerCase().includes(needle)).slice(0, 100);
  const subject = `user:${email.trim()}`;
  const manual = /^[^\s@]+@[^\s@]+$/.test(email.trim()) && !existing.has(subject);
  const chosen = source ? available.filter((account) => selected.includes(account.subject)) : manual ? [{ subject, name: name.trim() }] : [];
  const canAdd = Boolean(add && members.status === "ready" && chosen.length && !busy && !loading && !completed);
  const save = async () => {
    if (!canAdd || !add) return;
    const session = currentSession();
    setBusy(true);
    const used = new Set(target.map((member) => member.id));
    const results: Outcome[] = [];
    for (const account of chosen) {
      let added = false;
      try {
        requireSession(session);
        const identity = account.subject.slice(5);
        const slug = identity.split("@")[0].toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40) || "member";
        let id = slug;
        for (let suffix = 2; used.has(id); suffix++) id = `${slug}-${suffix}`;
        used.add(id);
        await send(add, id, { subject: account.subject, agent: false });
        added = true;
        requireSession(session);
        if (profile) await send(profile, id, { displayName: account.name || id, ... (identity.includes("@") ? { email: identity } : {}) });
        results.push({ subject: account.subject, added });
      } catch (problem) {
        results.push({ subject: account.subject, added, reason: reasonOf(problem) });
      }
    }
    reread();
    setOutcomes(results);
    setCompleted(true);
    setBusy(false);
    if (results.every((result) => !result.reason)) {
      toast({ body: t("Added {count} accounts.").replace("{count}", String(results.length)) });
      onClose();
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => !open && !busy && onClose()} width={560} purpose="form">
      <Layout height="fill" padding={4}
        header={<DialogHeader title={t("Add existing accounts")} onOpenChange={(open) => !open && !busy && onClose()} />}
        content={<LayoutContent><VStack gap={3}>
          <Text type="supporting" color="secondary">{t("Use an existing login. Access starts with no application roles; grant them in Members. Only names and login identities are copied.")}</Text>
          {completed ? <>
            <Banner status="warning" title={t("Some accounts need attention. Review Members before retrying.")} />
            {outcomes.map((outcome) => <Text key={outcome.subject} type="body">{outcome.subject.slice(5)} · {outcome.added ? t("Member added") : t("Not added")}{outcome.reason ? ` · ${outcome.reason}` : ""}</Text>)}
          </> : <>
            <FormLayout defaultOptionality="optional">
              <Selector label={t("Source workspace")} isDisabled={busy} value={source} options={[{ value: "", label: t("Enter an existing email") }, ...other.map((tenant) => ({ value: tenant.id, label: tenant.name || tenant.id }))]} onChange={setSource} />
              {source ? <TextInput label={t("Search accounts")} value={query} onChange={setQuery} isDisabled={busy || loading} /> : <>
                <TextInput label={t("Email")} isRequired value={email} onChange={setEmail} isDisabled={busy} />
                <TextInput label={t("Display name")} value={name} onChange={setName} isDisabled={busy} />
                {existing.has(subject) && <Banner status="warning" title={t("This account is already a member of this workspace.")} />}
              </>}
            </FormLayout>
            {source && <>
              {sourceError && <Banner status="error" title={sourceError} />}
              {loading && <Text type="supporting">{t("Loading accounts…")}</Text>}
              {!loading && !sourceError && <>
                <Text type="supporting">{t("{count} selected").replace("{count}", String(chosen.length))}</Text>
                {visible.length > 0 && <CheckboxInput label={t("Select visible accounts")} value={visible.every((account) => selected.includes(account.subject))} isDisabled={busy} onChange={(checked) => setSelected((previous) => checked ? [...new Set([...previous, ...visible.map((account) => account.subject)])] : previous.filter((id) => !visible.some((account) => account.subject === id)))} />}
                {visible.map((account) => <CheckboxInput key={account.subject} label={`${account.name} · ${account.subject.slice(5)}`} value={selected.includes(account.subject)} isDisabled={busy} onChange={(checked) => setSelected((previous) => checked ? [...previous, account.subject] : previous.filter((id) => id !== account.subject))} />)}
                {visible.length === 0 && <Text type="supporting">{t("No eligible accounts found.")}</Text>}
                {available.length > 100 && <Text type="supporting">{t("Showing up to 100 matching accounts. Search to find more.")}</Text>}
              </>}
            </>}
          </>}
        </VStack></LayoutContent>}
        footer={<LayoutFooter><HStack gap={2} vAlign="center"><StackItem size="fill" />
          <Button label={t(completed ? "Done" : "Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
          {!completed && <Button label={t("Add accounts")} variant="primary" isDisabled={!canAdd} isLoading={busy} onClick={() => void save()} />}
        </HStack></LayoutFooter>}
      />
    </Dialog>
  );
}
