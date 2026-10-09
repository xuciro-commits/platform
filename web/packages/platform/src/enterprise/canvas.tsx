// The enterprise drawing surface: the shared relationship canvas with UAF meaning —
// an icon per stereotype and kind (SAP/ArchiMate-style pictograms), placement
// edges as the tree, the other relationships as plain links. The modeler owns
// the positions and the decisions.
import type { ReactNode } from "react";
import { RelationCanvas, t, type CanvasAction, type RelationEdge, type RelationNode } from "@platform/ui";
import { Award, Boxes, Briefcase, Building, Building2, Cog, Crown, DoorOpen, Factory, FileText, FolderKanban, Globe2, Handshake, Hotel, IdCard, Landmark, Layers, MapPin, Network, Scale, Store, Target, Truck, UserRound, Users, Warehouse, Workflow, Wrench } from "lucide-react";
import { FILLS_POST, MEMBERSHIP, PLACEMENT, type Element, type Relationship } from "./model";

export type Positions = Record<string, [number, number]>;
export const STEREOTYPE_DROP = "application/x-uaf-stereotype";

/** A record pinned on a view beside the element it names (ADR-0085 D3): the
 * drawing then holds the apps that work with the model, not only the model. */
export type CanvasPin = { id: string; label: string; caption?: string; detail?: string; anchor: string; anchorName?: string };

// Kinds first (a plant is not a committee), stereotypes as the fallback.
const kindIcons: Record<string, ReactNode> = {
  group: <Landmark />, holding: <Landmark />, company: <Building2 />, subsidiary: <Building2 />, "business unit": <Layers />, "business group": <Layers />, division: <Layers />, region: <Globe2 />,
  plant: <Factory />, factory: <Factory />, workshop: <Factory />, line: <Workflow />, station: <Wrench />, cell: <Wrench />,
  hotel: <Hotel />, property: <Hotel />, warehouse: <Warehouse />, "distribution centre": <Warehouse />, store: <Store />, shop: <Store />, office: <Building />, branch: <Building />,
  department: <Briefcase />, "shared services": <Network />, "cost centre": <Scale />, team: <Users />, crew: <Users />, shift: <Users />,
  board: <Crown />, committee: <Users />, partner: <Handshake />, supplier: <Truck />, customer: <Handshake />,
  machine: <Cog />, equipment: <Cog />, vehicle: <Truck />, tool: <Wrench />, room: <DoorOpen />, floor: <Layers />, site: <MapPin />, zone: <MapPin />, aisle: <MapPin />, bin: <Boxes />, dock: <Truck />,
};
const stereotypeIcons: Record<string, ReactNode> = {
  ActualOrganization: <Building2 />, ActualPost: <IdCard />, ActualPerson: <UserRound />, ActualLocation: <MapPin />, ActualResource: <Cog />,
  ActualProject: <FolderKanban />, Capability: <Award />, EnterpriseGoal: <Target />, ActualResponsibility: <Briefcase />, OperationalActivity: <Workflow />, ServiceSpecification: <Boxes />,
};
const tones: Record<string, string> = { ActualOrganization: "info", ActualPost: "neutral", ActualPerson: "success", ActualLocation: "warning", ActualResource: "neutral", ActualProject: "info", Capability: "success", EnterpriseGoal: "danger" };
export const elementIcon = (el: Pick<Element, "stereotype" | "kind">): ReactNode => (el.kind && kindIcons[el.kind.toLowerCase()]) ?? stereotypeIcons[el.stereotype] ?? <Boxes />;

export function Canvas({ elements, relationships, pins = [], positions, selected, linking, admin, label, title, onPositions, onSelect, onDrop, onLink, facts, nodeActions, edgeActions, onReconnect, viewId, propertyLinks = [] }: {
  propertyLinks?: { id: string; source: string; target: string; label: string }[];
  viewId: string; elements: Element[]; relationships: Relationship[]; pins?: CanvasPin[]; positions: Positions; selected?: string; linking: boolean; admin: boolean;
  label: (r: Relationship) => string; title: (stereotype: string) => string;
  onPositions: (next: Positions) => void; onSelect: (id?: string) => void;
  onDrop: (stereotype: string, at: [number, number]) => void; onLink: (source: string, target: string) => void;
  /** What the details panel shows for an element: UAF type, kind, validity, where it sits. */
  facts: (el: Element) => { label: string; value: string }[];
  /** Operations on the selected element and the selected relationship (ADR-0084 D3). */
  nodeActions: (id: string) => CanvasAction[]; edgeActions: (id: string) => CanvasAction[];
  onReconnect: (id: string, source: string, target: string) => void;
}) {
  const nodes: RelationNode[] = [
    ...elements.map((el) => ({ id: el.id, label: el.name, caption: el.kind ? t(el.kind) : title(el.stereotype), icon: elementIcon(el), tone: tones[el.stereotype],
      dim: !!el.until, flag: el.published ? "shared" : el.owner ? el.owner : undefined, detail: `${title(el.stereotype)} · ${el.id}`, facts: facts(el) })),
    // A pinned record is drawn beside the element it names, and says which app it comes from.
    ...pins.map((p) => ({ id: p.id, label: p.label, caption: p.caption ?? t("record"), icon: <FileText />, tone: "warning", linkable: false, detail: p.detail ?? p.id,
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
    onSelect={onSelect} onLink={onLink} onDrop={(st, at) => onDrop(st, [at.x, at.y])}
    nodeActions={admin ? nodeActions : undefined} edgeActions={admin ? edgeActions : undefined} onReconnect={admin ? onReconnect : undefined}
    onPositionsChange={(next) => onPositions({ ...positions, ...Object.fromEntries(Object.entries(next).map(([id, p]) => [id, [p.x, p.y] as [number, number]])) })} />;
}
