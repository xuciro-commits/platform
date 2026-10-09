// The enterprise drawing surface: the shared relationship canvas with UAF meaning —
// an icon per stereotype and kind (SAP/ArchiMate-style pictograms), placement
// edges as the tree, the other relationships as plain links. The modeler owns
// the positions and the decisions. Stereotype icons come from the host profile
// (ADR-0090 D2); kinds stay here as icon names, drawn by the kit's one glyph.
import { IconGlyph, RelationCanvas, t, type CanvasAction, type RelationEdge, type RelationNode } from "@platform/ui";
import { FILLS_POST, MEMBERSHIP, PLACEMENT, type Element, type Relationship } from "./model";

export type Positions = Record<string, [number, number]>;
export const STEREOTYPE_DROP = "application/x-uaf-stereotype";

/** A record pinned on a view beside the element it names (ADR-0085 D3): the
 * drawing then holds the apps that work with the model, not only the model. */
export type CanvasPin = { id: string; label: string; caption?: string; detail?: string; anchor: string; anchorName?: string };

/** The glyph each kind takes — data, not elements (ADR-0090 D2). A plant is
 * still not a committee; the lookup is just a table the kit can draw. */
const kindIcons: Record<string, string> = {
  group: "landmark", holding: "landmark", company: "building-2", subsidiary: "building-2", "business unit": "layers", "business group": "layers", division: "layers", region: "globe",
  plant: "factory", factory: "factory", workshop: "factory", line: "workflow", station: "wrench", cell: "wrench",
  hotel: "hotel", property: "hotel", warehouse: "warehouse", "distribution centre": "warehouse", store: "store", shop: "store", office: "building", branch: "building",
  department: "briefcase", "shared services": "network", "cost centre": "scale", team: "users", crew: "users", shift: "users",
  board: "crown", committee: "users", partner: "handshake", supplier: "truck", customer: "handshake",
  machine: "cog", equipment: "cog", vehicle: "truck", tool: "wrench", room: "door", floor: "layers", site: "map-pin", zone: "map-pin", aisle: "map-pin", bin: "boxes", dock: "truck",
};
/** The fallback when the host's profile names no icon: the plain box. */
const DEFAULT_ICON = "boxes";
/** What each stereotype shows when it has no icon of its own (ADR-0090 D2: the
 * tone table is presentation vocabulary, so it stays with the view). */
const tones: Record<string, string> = { ActualOrganization: "info", ActualPost: "neutral", ActualPerson: "success", ActualLocation: "warning", ActualResource: "neutral", ActualProject: "info", Capability: "success", EnterpriseGoal: "danger" };

/** The icon an element draws under: its kind, then the stereotype's icon as the
 * host's profile declared it (ADR-0090 D2), then the plain box. */
export const elementIconName = (el: Pick<Element, "stereotype" | "kind">, icon?: (stereotype: string) => string | undefined): string =>
  (el.kind && kindIcons[el.kind.toLowerCase()]) ?? icon?.(el.stereotype) ?? DEFAULT_ICON;

export function Canvas({ elements, relationships, pins = [], positions, onPrepared, selected, linking, admin, label, title, icon, onPositions, onSelect, onDrop, onLink, facts, nodeActions, edgeActions, onReconnect, viewId, propertyLinks = [] }: {
  propertyLinks?: { id: string; source: string; target: string; label: string }[];
  viewId: string; elements: Element[]; relationships: Relationship[]; pins?: CanvasPin[]; positions: Positions; selected?: string; linking: boolean; admin: boolean;
  label: (r: Relationship) => string; title: (stereotype: string) => string;
  /** The host profile's icon for a stereotype (ADR-0090 D2); absent means none. */
  icon?: (stereotype: string) => string | undefined;
  onPositions: (next: Positions) => void; onSelect: (id?: string) => void;
  onDrop: (stereotype: string, at: [number, number]) => void; onLink: (source: string, target: string) => void;
  /** What the details panel shows for an element: UAF type, kind, validity, where it sits. */
  onPrepared?: (positions: Record<string, [number, number]>) => void;
  facts: (el: Element) => { label: string; value: string }[];
  /** Operations on the selected element and the selected relationship (ADR-0084 D3). */
  nodeActions: (id: string) => CanvasAction[]; edgeActions: (id: string) => CanvasAction[];
  onReconnect: (id: string, source: string, target: string) => void;
}) {
  const nodes: RelationNode[] = [
    ...elements.map((el) => ({ id: el.id, label: el.name, caption: el.kind ? t(el.kind) : title(el.stereotype), icon: <IconGlyph name={elementIconName(el, icon)} />, tone: tones[el.stereotype],
      dim: !!el.until, flag: el.published ? "shared" : el.owner ? el.owner : undefined, detail: `${title(el.stereotype)} · ${el.id}`, facts: facts(el) })),
    // A pinned record is drawn beside the element it names, and says which app it comes from.
    ...pins.map((p) => ({ id: p.id, label: p.label, caption: p.caption ?? t("record"), icon: <IconGlyph name="file-text" />, tone: "warning", linkable: false, detail: p.detail ?? p.id,
      facts: [{ label: t("Pinned record"), value: p.detail ?? p.id }, ...(p.anchorName ? [{ label: t("Names"), value: p.anchorName }] : [])] })),
  ];
  const edges: RelationEdge[] = [
    ...propertyLinks.map((p) => ({ ...p, dashed: true, reconnectable: false, directed: true })),
    ...relationships.map((r) => ({ id: r.id, source: r.source, target: r.target, label: label(r), tree: r.stereotype === PLACEMENT,
      dashed: r.stereotype === MEMBERSHIP || r.stereotype === FILLS_POST, directed: r.stereotype !== PLACEMENT })),
    ...pins.filter((p) => elements.some((e) => e.id === p.anchor)).map((p) => ({ id: `pin:${p.id}`, source: p.id, target: p.anchor, label: t("names"), reconnectable: false, dashed: true, directed: true, tone: "warning" })),
  ];
  const pos = Object.fromEntries(Object.entries(positions).map(([id, [x, y]]) => [id, { x, y }]));
  return <RelationCanvas viewportKey={viewId} nodes={nodes} edges={edges} positions={pos} selected={selected} editable={admin} linking={linking} height="100%" dropType={STEREOTYPE_DROP}
    storeKey={admin ? undefined : `uaf:${viewId}`}
    onLayoutReady={(next) => onPrepared?.(Object.fromEntries(Object.entries(next).map(([id, p]) => [id, [p.x, p.y] as [number, number]])))}
    onSelect={onSelect} onLink={onLink} onDrop={(st, at) => onDrop(st, [at.x, at.y])}
    nodeActions={admin ? nodeActions : undefined} edgeActions={admin ? edgeActions : undefined} onReconnect={admin ? onReconnect : undefined}
    onPositionsChange={admin ? (next) => onPositions({ ...positions, ...Object.fromEntries(Object.entries(next).map(([id, p]) => [id, [p.x, p.y] as [number, number]])) }) : undefined} />;
}
