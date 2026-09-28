import {
  Button, EntityCard, Inbox, MarkdownEditor, PageHeader, PropertyList, RecordLookup, StatusTag, defineStatuses, notify,
  type EntityInfo, type InboxTask, type RecordSource,
} from "@platform/ui";
import { Check, Clock, Eye, X } from "lucide-react";
import { useState } from "react";
import { ShowcaseCard } from "./ShowcaseCard";

const equipStatus = defineStatuses({
  operational: { label: "Operational", tone: "success" },
  maintenance: { label: "Scheduled Maint", tone: "warning" },
  offline: { label: "Emergency Stop", tone: "danger" },
});

const defaultMarkdown = `# CNC Milling Spindle Maintenance SOP

**Code**: SOP-CNC-04 · **Rev**: 3.2 · **Classification**: Level-2 Safety

## Pre-Shift Inspection Checklist
1. Verify pneumatic pressure is between **0.6 – 0.8 MPa**.
2. Clean spindle taper with solvent-soaked cloth; ensure zero chip debris.
3. Check tool holder clamping force with gauge \`TG-500\`.

> **Safety Warning**: Eye protection and hearing guards are mandatory within 3 meters.

\`\`\`ts
// Automated interlock check routine
if (spindleTemp > 75.0 || vibrationRms > 4.2) {
  emergencyHalt("Spindle parameter out of envelope");
}
\`\`\`
`;

const sampleTasks: InboxTask[] = [
  {
    id: "task-101",
    revision: 1,
    created: { at: new Date().toISOString(), by: "system" },
    changed: { at: new Date().toISOString(), by: "system" },
    title: "Review urgent non-conformance disposition (SFC-00412)",
    body: "Diameter undersized by 0.035mm on Bearing Journal. Engineering review required for concession.",
    due: new Date(Date.now() - 3600000 * 5).toISOString(),
    assignee: "Lin (Quality Engineer)",
    candidates: ["qa_engineers"],
    app: "mes",
    state: "open",
  },
  {
    id: "task-102",
    revision: 1,
    created: { at: new Date().toISOString(), by: "system" },
    changed: { at: new Date().toISOString(), by: "system" },
    title: "Quarterly preventative maintenance sign-off: WC-CNC-02",
    body: "Spindle bearing lubrication and belt tension verified by external service vendor.",
    due: new Date(Date.now() + 3600000 * 18).toISOString(),
    assignee: "Ada (Supervisor)",
    candidates: ["supervisors"],
    app: "mes",
    state: "open",
  },
];

const mockLookupSource: RecordSource = {
  entity: () => ({
    name: "Account",
    plural: "Accounts",
    title: "Customer Account",
    type: "crm.account",
    display: "name",
    primary: "id",
    fields: [
      { name: "id", title: "ID", type: "text", required: true },
      { name: "name", title: "Account Name", type: "text", required: true },
    ],
    standard: [],
    app: "crm",
  } as EntityInfo),
  list: async (_type, query) => {
    const accounts = [
      { id: "ACC-0101", name: "Suzhou Precision Machining Ltd", revision: 1, created: { at: "", by: "" }, changed: { at: "", by: "" } },
      { id: "ACC-0102", name: "Penang Advanced Aerospace Parts", revision: 1, created: { at: "", by: "" }, changed: { at: "", by: "" } },
      { id: "ACC-0103", name: "Nordic Hydraulic Systems AB", revision: 1, created: { at: "", by: "" }, changed: { at: "", by: "" } },
      { id: "ACC-0104", name: "Rotary Turbine Dynamics GmbH", revision: 1, created: { at: "", by: "" }, changed: { at: "", by: "" } },
      { id: "ACC-0105", name: "Apex Global Casting Solutions", revision: 1, created: { at: "", by: "" }, changed: { at: "", by: "" } },
    ];
    const q = (query.search ?? "").toLowerCase();
    const records = accounts.filter((a) => !q || a.name.toLowerCase().includes(q) || a.id.toLowerCase().includes(q));
    return { records, total: records.length };
  },
  get: async () => ({} as never),
};

export function KnowledgeShowcase() {
  const [docContent, setDocContent] = useState<string | undefined>(defaultMarkdown);
  const [selectedAccount, setSelectedAccount] = useState<string | undefined>("ACC-0101");
  const [tasks, setTasks] = useState(sampleTasks);

  return (
    <div className="flex flex-col gap-6 pb-12">
      <PageHeader
        title="Enterprise Patterns & Knowledge"
        description="Markdown documentation editors, action-oriented task inboxes, record detail cards, and asynchronous reference lookups."
      />

      {/* Markdown & Knowledge Editor */}
      <ShowcaseCard
        title="Markdown Knowledge Editor (MarkdownEditor & Markdown)"
        description="Rich tabbed editor with Write/Preview toggle, character counter, safe HTML rendering, and syntax-highlighted code blocks."
        contentClassName="w-full block"
      >
        <div className="w-full max-w-3xl">
          <MarkdownEditor
            value={docContent ?? ""}
            onChange={(val) => setDocContent(val)}
            rows={10}
            placeholder="Type standard markdown documentation here…"
          />
        </div>
      </ShowcaseCard>

      {/* Record Cards & Properties */}
      <div className="grid gap-4 md:grid-cols-2">
        <ShowcaseCard
          title="EntityCard & PropertyList"
          description="Standardized record headers with badges, key-value property grids, and action toolbars."
          contentClassName="w-full block"
        >
          <EntityCard
            title="5-Axis Milling Machine CNC-01"
            subtitle="EQUIP-WC-001 · Plant Suzhou Bay 4"
            status={<StatusTag status="operational" registry={equipStatus} />}
            properties={[
              ["Asset Tag", "AT-2024-0091"],
              ["Controller", "Heidenhain TNC 640"],
              ["Spindle Hours", <span className="tabular-nums font-mono text-xs">4,812 h</span>],
              ["Next Service", <span className="flex items-center gap-1 text-xs text-muted"><Clock className="size-3" />in 14 days</span>],
            ]}
            actions={
              <>
                <Button size="sm" variant="ghost"><Eye />Telemetry</Button>
                <Button size="sm" variant="primary">Schedule Maintenance</Button>
              </>
            }
          />
          <div className="mt-3 rounded-md border border-border bg-muted/10 p-3">
            <span className="mb-2 block text-xs font-semibold uppercase tracking-wider text-muted">Standalone PropertyList</span>
            <PropertyList items={[
              ["IP Address", "192.168.10.42"],
              ["Gateway Sim", "Connected (0 packet loss)"],
              ["PLC Firmware", "v4.18.2-rt"],
            ]} />
          </div>
        </ShowcaseCard>

        {/* Asynchronous Reference Lookup */}
        <ShowcaseCard
          title="Asynchronous Reference Selection (RecordLookup)"
          description="Typeahead search combobox over scoped server entity records, with debounced search and keyboard navigation."
          contentClassName="w-full block"
        >
          <div className="max-w-md">
            <label className="mb-1 block text-xs font-medium text-muted">
              Select Business Partner / Supplier (CRM Account):
            </label>
            <RecordLookup
              source={mockLookupSource}
              type="crm.account"
              value={selectedAccount}
              onChange={(id) => {
                setSelectedAccount(id);
                notify.success(id ? `Selected account ${id}` : `Cleared account selection`);
              }}
            />
            {selectedAccount && (
              <p className="mt-2 text-xs text-muted">
                Active foreign key link: <strong className="font-mono text-foreground">{selectedAccount}</strong>
              </p>
            )}
          </div>
        </ShowcaseCard>
      </div>

      {/* Task Inbox */}
      <ShowcaseCard
        title="Member Task Inbox (Inbox)"
        description="ADR-0017 task distribution view with overdue flags, due dates, assignee metadata, and inline response buttons."
        contentClassName="w-full block"
      >
        <div className="w-full max-w-3xl">
          <Inbox
            tasks={tasks}
            onOpen={(t) => notify(`Opened detail view for task ${t.id}`)}
            actions={(t) => (
              <div className="flex items-center gap-1">
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label="Approve"
                  onClick={(e) => {
                    e.stopPropagation();
                    setTasks((prev) => prev.filter((item) => item.id !== t.id));
                    notify.success(`Task ${t.id} completed`);
                  }}
                >
                  <Check className="size-3 text-[var(--tone-success)]" />
                  Approve
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label="Dismiss"
                  onClick={(e) => {
                    e.stopPropagation();
                    setTasks((prev) => prev.filter((item) => item.id !== t.id));
                    notify.warning(`Task ${t.id} rejected`);
                  }}
                >
                  <X className="size-3 text-[var(--tone-danger)]" />
                  Reject
                </Button>
              </div>
            )}
          />
        </div>
      </ShowcaseCard>
    </div>
  );
}
