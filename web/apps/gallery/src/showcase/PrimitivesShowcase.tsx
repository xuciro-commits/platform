import {
  Button, Checkbox, Dialog, Disclosure, Input, PageHeader, Select, Sheet, Textarea, Toggles, Tree,
} from "@platform/ui";
import { Check, ExternalLink, Plus, Search, Settings, Trash2 } from "lucide-react";
import { useState } from "react";
import { ShowcaseCard } from "./ShowcaseCard";

type OrgNode = { id: string; name: string; children?: OrgNode[] };
const orgTree: OrgNode[] = [
  {
    id: "corp", name: "Global HQ", children: [
      {
        id: "eng", name: "Engineering", children: [
          { id: "platform", name: "Platform Core" },
          { id: "apps", name: "Industry Applications" },
        ],
      },
      {
        id: "ops", name: "Manufacturing Operations", children: [
          { id: "plant-sz", name: "Plant Suzhou" },
          { id: "plant-pg", name: "Plant Penang" },
        ],
      },
    ],
  },
];

export function PrimitivesShowcase() {
  const [checked, setChecked] = useState(true);
  const [toggleValues, setToggleValues] = useState<string[]>(["active", "pinned"]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [wideDialogOpen, setWideDialogOpen] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [selectedTree, setSelectedTree] = useState<string>("platform");

  return (
    <div className="flex flex-col gap-6 pb-12">
      <PageHeader
        title="Primitives"
        description="Foundational UI building blocks: buttons, inputs, selection controls, trees, and accessible modal overlays."
      />

      {/* Button Matrix */}
      <div className="grid gap-4">
        <ShowcaseCard
          title="Buttons / Variants"
          description="Six semantic variants defined via class-variance-authority for distinct visual hierarchy."
        >
          <Button variant="default">Default</Button>
          <Button variant="primary"><Plus />Primary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger"><Trash2 />Danger</Button>
          <Button variant="link"><ExternalLink />Link button</Button>
          <Button disabled>Disabled</Button>
        </ShowcaseCard>

        <ShowcaseCard
          title="Buttons / Sizes & Icon Actions"
          description="Compact sm (24px) for dense data rows, standard md (28px) for forms and headers, and square icon sizes."
        >
          <Button size="sm">Small (24px)</Button>
          <Button size="sm" variant="primary"><Plus />New item</Button>
          <Button size="md">Medium (28px)</Button>
          <Button size="md" variant="primary">Submit</Button>
          <Button size="icon" variant="default" aria-label="Settings"><Settings /></Button>
          <Button size="icon" variant="ghost" aria-label="Confirm"><Check /></Button>
          <Button size="icon" variant="danger" aria-label="Delete"><Trash2 /></Button>
        </ShowcaseCard>
      </div>

      {/* Inputs & Form Elements */}
      <div className="grid gap-4 md:grid-cols-2">
        <ShowcaseCard
          title="Text Inputs & Search"
          description="Consistent typography, tabular figures support, and responsive focus rings."
          contentClassName="grid w-full gap-2.5"
        >
          <Input placeholder="Standard placeholder text" />
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
            <Input placeholder="Search with icon prefix…" className="pl-7" />
          </div>
          <Input defaultValue="Read-only field value" disabled />
          <Textarea placeholder="Multi-line comments or instructions…" rows={3} />
        </ShowcaseCard>

        <ShowcaseCard
          title="Select & Dropdown"
          description="Native accessible select with consistent border styling and tone matching."
          contentClassName="grid w-full gap-2.5"
        >
          <Select defaultValue="machining">
            <option value="machining">Machining Department (WC-CNC)</option>
            <option value="assembly">Assembly Line (WC-ASM)</option>
            <option value="quality">Quality Assurance (WC-QA)</option>
            <option value="logistics">Warehouse & Logistics</option>
          </Select>
          <Select disabled defaultValue="disabled">
            <option value="disabled">Locked by permission policy</option>
          </Select>
        </ShowcaseCard>
      </div>

      {/* Interactive Controls */}
      <div className="grid gap-4 md:grid-cols-2">
        <ShowcaseCard
          title="Selection Controls (Toggles & Checkbox)"
          description="Clickable filter pills for multiple states, and native-accented checkboxes."
          contentClassName="flex flex-col items-start gap-4"
        >
          <div>
            <span className="mb-1.5 block text-xs font-medium text-muted">Toggles (Multi-tag filter):</span>
            <Toggles
              options={[
                { value: "active", label: "Active" },
                { value: "archived", label: "Archived" },
                { value: "pinned", label: "Pinned" },
                { value: "draft", label: "Draft" },
              ]}
              value={toggleValues}
              onChange={setToggleValues}
            />
          </div>
          <div className="flex items-center gap-4 text-sm">
            <Checkbox checked={checked} onChange={setChecked}>
              Enable automatic synchronization
            </Checkbox>
            <Checkbox checked={false} onChange={() => {}} disabled>
              Restricted (Disabled)
            </Checkbox>
          </div>
        </ShowcaseCard>

        <ShowcaseCard
          title="Disclosure & Tree"
          description="Collapsible accordion sections and hierarchical unit navigation."
          contentClassName="flex flex-col items-start gap-3 w-full"
        >
          <Disclosure
            className="w-full rounded-md border border-border/80 bg-surface p-2 text-sm"
            summary={<span className="font-medium text-foreground">Advanced System Settings</span>}
          >
            <p className="mt-2 text-xs text-muted leading-relaxed">
              Diagnostic telemetry interval: 500ms. Heartbeat timeout threshold: 15s. All trace decisions logged to Ledger ADR-0010.
            </p>
          </Disclosure>

          <div className="w-full rounded-md border border-border/80 bg-surface p-2">
            <span className="mb-1.5 block text-xs font-medium text-muted">Organization Tree:</span>
            <Tree<OrgNode>
              roots={orgTree}
              children={(n) => n.children ?? []}
              id={(n) => n.id}
              row={(n) => <span className="font-mono text-xs">{n.name}</span>}
              selected={selectedTree}
              onSelect={(n) => setSelectedTree(n.id)}
            />
          </div>
        </ShowcaseCard>
      </div>

      {/* Overlays: Dialog & Sheet */}
      <ShowcaseCard
        title="Modals & Overlays (Dialog & Sheet)"
        description="Radix UI-backed dialogs with focus trapping, Escape cancellation, outside click handling, and sliding sheets."
      >
        <Button variant="default" onClick={() => setDialogOpen(true)}>
          Open Standard Dialog
        </Button>
        <Button variant="default" onClick={() => setWideDialogOpen(true)}>
          Open Wide Dialog
        </Button>
        <Button variant="primary" onClick={() => setSheetOpen(true)}>
          Open Side Sheet
        </Button>

        {/* Standard Dialog */}
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen} title="Confirm Operation">
          <div className="flex flex-col gap-3 py-2 text-sm">
            <p className="text-muted leading-relaxed">
              Are you sure you want to release production order <strong className="text-foreground">WO-000412</strong>? This will allocate 120 units of raw materials and lock routing RT-10.
            </p>
            <div className="mt-2 flex justify-end gap-2">
              <Button onClick={() => setDialogOpen(false)}>Cancel</Button>
              <Button variant="primary" onClick={() => setDialogOpen(false)}>Confirm Release</Button>
            </div>
          </div>
        </Dialog>

        {/* Wide Dialog */}
        <Dialog open={wideDialogOpen} onOpenChange={setWideDialogOpen} title="Batch Work Center Configuration" wide>
          <div className="grid gap-3 py-2 text-sm">
            <p className="text-xs text-muted">
              Configure parameters across multi-spindle CNC machining units in Plant Suzhou.
            </p>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs text-muted">Center Code</label>
                <Input defaultValue="WC-CNC-04" className="mt-1" />
              </div>
              <div>
                <label className="text-xs text-muted">Hourly Capacity</label>
                <Input defaultValue="24 pcs/h" className="mt-1" />
              </div>
            </div>
            <div className="mt-3 flex justify-end gap-2">
              <Button onClick={() => setWideDialogOpen(false)}>Close</Button>
              <Button variant="primary" onClick={() => setWideDialogOpen(false)}>Save Changes</Button>
            </div>
          </div>
        </Dialog>

        {/* Sheet */}
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen} title="Work Center Details" width={460}>
          <div className="flex flex-col gap-4 py-1 text-sm">
            <div>
              <span className="text-xs font-semibold uppercase tracking-wider text-muted">Identity</span>
              <p className="text-base font-medium text-foreground">WC-CNC-01 · 5-Axis Milling Machine</p>
              <p className="text-xs text-muted">Department: Machining · Bay 4, Plant Suzhou</p>
            </div>
            <div className="rounded-md border border-border bg-muted/10 p-3">
              <span className="text-xs font-medium text-muted">Current Work:</span>
              <p className="mt-1 font-mono text-xs">Pump housing A356 · WO-000108</p>
              <p className="text-xs text-muted">Elapsed: 42 min · Progress: 38 / 50 pcs</p>
            </div>
            <div className="mt-auto flex justify-end gap-2 pt-4 border-t border-border">
              <Button onClick={() => setSheetOpen(false)}>Done</Button>
              <Button variant="primary" onClick={() => setSheetOpen(false)}>Edit Properties</Button>
            </div>
          </div>
        </Sheet>
      </ShowcaseCard>
    </div>
  );
}
