// The ERP's UI (ADR-0024): journal entries drafted with their lines and posted
// from their page, the chart of accounts, periods, postings and the trial
// balance. Lists, pages and forms are generated from the declarations; the
// host decides who sees and does what.
import "./i18n";
import { GeneratedForm, Records, defineApp, newId, useHost, useRead } from "@platform/app";
import { Button, Dialog, Input, PageHeader, t } from "@platform/ui";
import { BookOpen, CalendarRange, Landmark, ListTree, Plus, Scale } from "lucide-react";
import { useState } from "react";

function Entries() {
  const { can, decide } = useHost();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <Records type="erp.entry" actions={can("erp.entry.create") && <Button variant="primary" onClick={() => setCreating(true)}><Plus />{t("New entry")}</Button>} />
      <Dialog wide open={creating} onOpenChange={setCreating} title={t("New entry")}>
        <GeneratedForm type="erp.entry" record={{ journal: "general", date: new Date().toISOString().slice(0, 10), lines: [{}, {}] } as never}
          submitLabel={t("Save draft")} onCancel={() => setCreating(false)}
          onSubmit={async (v) => { if (await decide("erp.entry.create", { type: "erp.entry", id: newId("JE") }, v, { expectedRevision: 0 })) setCreating(false); }} />
      </Dialog>
    </>
  );
}

function Accounts() {
  const { can, decide } = useHost();
  const [creating, setCreating] = useState(false);
  const [code, setCode] = useState("");
  return (
    <>
      <Records type="erp.account" actions={can("erp.account.create") && <Button variant="primary" onClick={() => setCreating(true)}><Plus />{t("New account")}</Button>} />
      <Dialog open={creating} onOpenChange={setCreating} title={t("New account")}>
        <div className="grid gap-3">
          <label className="grid gap-1 text-xs font-medium text-muted">{t("Code")} *<Input value={code} onChange={(e) => setCode(e.target.value.trim())} placeholder="1403" /></label>
          <GeneratedForm type="erp.account" submitLabel={t("Create")} onCancel={() => setCreating(false)}
            onSubmit={async (v) => { if (code && await decide("erp.account.create", { type: "erp.account", id: code }, v, { expectedRevision: 0 })) { setCreating(false); setCode(""); } }} />
        </div>
      </Dialog>
    </>
  );
}

function Periods() {
  const { can, decide } = useHost();
  const [month, setMonth] = useState(new Date().toISOString().slice(0, 7));
  return (
    <Records type="erp.period" actions={can("erp.period.open") && (
      <span className="flex gap-2">
        <Input type="month" aria-label={t("Month")} value={month} onChange={(e) => setMonth(e.target.value)} className="w-40" />
        <Button variant="primary" onClick={() => decide("erp.period.open", { type: "erp.period", id: month }, {}, { expectedRevision: 0 })}><Plus />{t("Open period")}</Button>
      </span>)} />
  );
}

type Balance = { account: string; name: string; kind: string; debit: number; credit: number; balance: number };

function TrialBalance() {
  const rows = useRead<Balance[]>("/v1/trial-balance") ?? [];
  const amount = (minor: number) => (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const total = (k: "debit" | "credit") => rows.reduce((s, r) => s + r[k], 0);
  const cell = "px-2 py-1 text-right tabular-nums";
  return (
    <>
      <PageHeader title={t("Trial balance")} description={t("Every account with postings: the sums of its debits and credits, and its balance. Debits equal credits when the books balance.")} />
      <table className="w-full max-w-3xl text-sm">
        <thead className="text-xs text-muted"><tr>
          <th className="px-2 py-1 text-left">{t("Account")}</th><th className="px-2 py-1 text-left">{t("Kind")}</th>
          <th className={cell}>{t("Debit")}</th><th className={cell}>{t("Credit")}</th><th className={cell}>{t("Balance")}</th></tr></thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.account} className="border-t border-border">
              <td className="px-2 py-1">{r.account} {r.name}</td><td className="px-2 py-1">{t(r.kind)}</td>
              <td className={cell}>{amount(r.debit)}</td><td className={cell}>{amount(r.credit)}</td><td className={cell}>{amount(r.balance)}</td>
            </tr>
          ))}
          <tr className="border-t-2 border-border font-medium">
            <td className="px-2 py-1" colSpan={2}>{t("Total")}</td><td className={cell}>{amount(total("debit"))}</td><td className={cell}>{amount(total("credit"))}</td><td />
          </tr>
        </tbody>
      </table>
    </>
  );
}

export default defineApp({
  id: "erp",
  title: "ERP",
  icon: <Landmark />,
  home: { view: "entries" },
  views: [
    { id: "entries", title: () => t("Journal entries"), render: () => <Entries /> },
    { id: "accounts", title: () => t("Accounts"), render: () => <Accounts /> },
    { id: "periods", title: () => t("Periods"), render: () => <Periods /> },
    { id: "postings", title: () => t("Postings"), render: () => <Records type="erp.posting" /> },
    { id: "trial-balance", title: () => t("Trial balance"), render: () => <TrialBalance /> },
  ],
  nav: () => [{
    label: t("Accounting"), items: [
      { label: t("Journal entries"), icon: <BookOpen />, route: { view: "entries" } },
      { label: t("Postings"), icon: <ListTree />, route: { view: "postings" } },
      { label: t("Trial balance"), icon: <Scale />, route: { view: "trial-balance" } },
      { label: t("Accounts"), icon: <Landmark />, route: { view: "accounts" } },
      { label: t("Periods"), icon: <CalendarRange />, route: { view: "periods" } },
    ],
  }],
});
