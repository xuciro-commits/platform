// The ERP's UI (ADR-0024): journal entries drafted with their lines and posted
// from their page, the chart of accounts, periods, postings and the trial
// balance; purchase orders placed, received and billed from their page,
// partners, products, stock moves and what is on hand; production orders
// released to the plant, which confirms them through production.orders/1. Lists, pages and forms are generated from the declarations; the
// host decides who sees and does what.
import "./i18n";
import { GeneratedForm, Records, defineApp, newId, useHost, useRead } from "@platform/app";
import { Button, Dialog, Input, PageHeader, t } from "@platform/ui";
import { ArrowLeftRight, Factory, BookOpen, CalendarRange, Handshake, Landmark, ListTree, Package, Plus, Scale, ShoppingCart, Warehouse } from "lucide-react";
import { useState } from "react";

// Records whose ID is a code people choose: accounts ("1403") and products ("P-100").
function Coded({ type, label, placeholder }: { type: string; label: string; placeholder: string }) {
  const { can, decide } = useHost();
  const [creating, setCreating] = useState(false);
  const [code, setCode] = useState("");
  return (
    <>
      <Records type={type} covers={[`${type}.create`]} actions={can(`${type}.create`) && <Button variant="primary" onClick={() => setCreating(true)}><Plus />{label}</Button>} />
      <Dialog open={creating} onOpenChange={setCreating} title={label}>
        <div className="grid gap-3">
          <label className="grid gap-1 text-xs font-medium text-muted">{t("Code")} *<Input value={code} onChange={(e) => setCode(e.target.value.trim())} placeholder={placeholder} /></label>
          <GeneratedForm type={type} submitLabel={t("Create")} onCancel={() => setCreating(false)}
            onSubmit={async (v) => { if (code && await decide(`${type}.create`, { type, id: code }, v, { expectedRevision: 0 })) { setCreating(false); setCode(""); } }} />
        </div>
      </Dialog>
    </>
  );
}

// Records people draft in a form and then move along their lifecycle from their page.
function Drafted({ type, label, prefix, initial, wide }: { type: string; label: string; prefix: string; initial?: object; wide?: boolean }) {
  const { can, decide } = useHost();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <Records type={type} covers={[`${type}.create`]} actions={can(`${type}.create`) && <Button variant="primary" onClick={() => setCreating(true)}><Plus />{label}</Button>} />
      <Dialog wide={wide} open={creating} onOpenChange={setCreating} title={label}>
        <GeneratedForm type={type} record={initial as never} submitLabel={t("Save draft")} onCancel={() => setCreating(false)}
          onSubmit={async (v) => { if (await decide(`${type}.create`, { type, id: newId(prefix) }, v, { expectedRevision: 0 })) setCreating(false); }} />
      </Dialog>
    </>
  );
}

type Stock = { product: string; name: string; unit: string; quantity: number; value: number };

function OnHand() {
  const rows = useRead<Stock[]>("/v1/on-hand") ?? [];
  const cell = "px-2 py-1 text-right tabular-nums";
  return (
    <>
      <PageHeader title={t("On hand")} description={t("What is in stock: the sum of each product's moves, valued at standard cost.")} />
      <table className="w-full max-w-3xl text-sm">
        <thead className="text-xs text-muted"><tr>
          <th className="px-2 py-1 text-left">{t("Product")}</th><th className={cell}>{t("Quantity")}</th><th className={cell}>{t("Value")}</th></tr></thead>
        <tbody>{rows.map((r) => (
          <tr key={r.product} className="border-t border-border">
            <td className="px-2 py-1">{r.product} {r.name}</td><td className={cell}>{r.quantity.toLocaleString()} {r.unit}</td>
            <td className={cell}>{(r.value / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</td>
          </tr>))}</tbody>
      </table>
    </>
  );
}

function Periods() {
  const { can, decide } = useHost();
  const [month, setMonth] = useState(new Date().toISOString().slice(0, 7));
  return (
    <Records type="erp.period" covers={["erp.period.open"]} actions={can("erp.period.open") && (
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
    { id: "entries", title: () => t("Journal entries"), render: () => <Drafted type="erp.entry" label={t("New entry")} prefix="JE" wide
      initial={{ journal: "general", date: new Date().toISOString().slice(0, 10), lines: [{}, {}] }} /> },
    { id: "accounts", title: () => t("Accounts"), render: () => <Coded type="erp.account" label={t("New account")} placeholder="1403" /> },
    { id: "purchases", title: () => t("Purchase orders"), render: () => <Drafted type="erp.purchase" label={t("New purchase order")} prefix="PO" wide
      initial={{ date: new Date().toISOString().slice(0, 10), lines: [{}] }} /> },
    { id: "partners", title: () => t("Partners"), render: () => <Drafted type="erp.partner" label={t("New partner")} prefix="BP" /> },
    { id: "products", title: () => t("Products"), render: () => <Coded type="erp.product" label={t("New product")} placeholder="P-100" /> },
    { id: "moves", title: () => t("Stock moves"), render: () => <Records type="erp.move" /> },
    { id: "production", title: () => t("Production orders"), render: () => <Drafted type="erp.production" label={t("New production order")} prefix="MO" /> },
    { id: "on-hand", title: () => t("On hand"), render: () => <OnHand /> },
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
  }, {
    label: t("Purchasing and stock"), items: [
      { label: t("Production orders"), icon: <Factory />, route: { view: "production" } },
      { label: t("Purchase orders"), icon: <ShoppingCart />, route: { view: "purchases" } },
      { label: t("On hand"), icon: <Warehouse />, route: { view: "on-hand" } },
      { label: t("Stock moves"), icon: <ArrowLeftRight />, route: { view: "moves" } },
      { label: t("Products"), icon: <Package />, route: { view: "products" } },
      { label: t("Partners"), icon: <Handshake />, route: { view: "partners" } },
    ],
  }],
});
