import type { IconType } from "@astryxdesign/core/Icon";
import {
  AdjustmentsHorizontalIcon,
  ArrowsRightLeftIcon,
  BoltIcon,
  BookOpenIcon,
  BuildingStorefrontIcon,
  CalendarDaysIcon,
  ChartBarIcon,
  CircleStackIcon,
  ClipboardDocumentCheckIcon,
  ClockIcon,
  CodeBracketIcon,
  Cog6ToothIcon,
  CommandLineIcon,
  CubeIcon,
  CubeTransparentIcon,
  DocumentDuplicateIcon,
  DocumentTextIcon,
  FolderIcon,
  HeartIcon,
  HomeIcon,
  InboxIcon,
  KeyIcon,
  LinkIcon,
  PhotoIcon,
  PlayIcon,
  PuzzlePieceIcon,
  RectangleGroupIcon,
  RectangleStackIcon,
  ServerStackIcon,
  ShareIcon,
  ShieldCheckIcon,
  SparklesIcon,
  Squares2X2Icon,
  SwatchIcon,
  TableCellsIcon,
  TagIcon,
  TruckIcon,
  UserGroupIcon,
  UsersIcon,
  ViewColumnsIcon,
  WrenchScrewdriverIcon,
} from "@heroicons/react/24/outline";

export type NavItem = { id: string; label: string; icon: IconType };
export type NavSection = { title: string; items: NavItem[] };
export type App = { id: string; label: string; icon: IconType; sections: NavSection[] };

export const apps: App[] = [
  {
    id: "home",
    label: "Home",
    icon: HomeIcon,
    sections: [
      {
        title: "Workspace",
        items: [
          { id: "overview", label: "Overview", icon: Squares2X2Icon },
          { id: "work", label: "My work", icon: InboxIcon },
          { id: "recent", label: "Recent", icon: ClockIcon },
          { id: "projects", label: "Projects", icon: FolderIcon },
        ],
      },
      {
        title: "Official templates",
        items: [
          { id: "sample-table", label: "Filterable table", icon: TableCellsIcon },
          { id: "sample-board", label: "Kanban board", icon: ViewColumnsIcon },
        ],
      },
    ],
  },
  {
    id: "data",
    label: "Data",
    icon: CircleStackIcon,
    sections: [
      {
        title: "Ingest",
        items: [
          { id: "connections", label: "Connections", icon: LinkIcon },
          { id: "sources", label: "Sources", icon: ServerStackIcon },
        ],
      },
      {
        title: "Transform",
        items: [
          { id: "datasets", label: "Datasets", icon: RectangleStackIcon },
          { id: "pipelines", label: "Pipelines", icon: ArrowsRightLeftIcon },
          { id: "schedules", label: "Schedules", icon: CalendarDaysIcon },
          { id: "media", label: "Media sets", icon: PhotoIcon },
        ],
      },
      { title: "Trace", items: [{ id: "lineage", label: "Lineage", icon: ShareIcon }] },
    ],
  },
  {
    id: "ontology",
    label: "Ontology",
    icon: CubeIcon,
    sections: [
      {
        title: "Define",
        items: [
          { id: "object-types", label: "Object types", icon: CubeIcon },
          { id: "link-types", label: "Link types", icon: LinkIcon },
          { id: "action-types", label: "Action types", icon: BoltIcon },
          { id: "interfaces", label: "Interfaces", icon: PuzzlePieceIcon },
          { id: "shared-properties", label: "Shared properties", icon: SwatchIcon },
          { id: "value-types", label: "Value types", icon: AdjustmentsHorizontalIcon },
        ],
      },
      { title: "Explore", items: [{ id: "objects", label: "Objects", icon: CubeTransparentIcon }] },
    ],
  },
  {
    id: "logic",
    label: "Logic",
    icon: CodeBracketIcon,
    sections: [
      {
        title: "Build",
        items: [
          { id: "functions", label: "Functions", icon: CommandLineIcon },
          { id: "automations", label: "Automations", icon: PlayIcon },
        ],
      },
    ],
  },
  {
    id: "apps",
    label: "Apps",
    icon: RectangleGroupIcon,
    sections: [
      {
        title: "Build",
        items: [
          { id: "modules", label: "Modules", icon: RectangleGroupIcon },
          { id: "templates", label: "Templates", icon: DocumentDuplicateIcon },
        ],
      },
    ],
  },
  {
    id: "ai",
    label: "AI",
    icon: SparklesIcon,
    sections: [
      {
        title: "Build",
        items: [
          { id: "agents", label: "Agents", icon: SparklesIcon },
          { id: "knowledge", label: "Knowledge", icon: BookOpenIcon },
          { id: "evaluations", label: "Evaluations", icon: ClipboardDocumentCheckIcon },
        ],
      },
    ],
  },
  {
    id: "delivery",
    label: "Delivery",
    icon: TruckIcon,
    sections: [
      {
        title: "Release",
        items: [
          { id: "products", label: "Products", icon: CubeIcon },
          { id: "versions", label: "Versions", icon: TagIcon },
        ],
      },
      {
        title: "Install",
        items: [
          { id: "marketplace", label: "Marketplace", icon: BuildingStorefrontIcon },
          { id: "installations", label: "Installations", icon: WrenchScrewdriverIcon },
        ],
      },
    ],
  },
  {
    id: "operations",
    label: "Operations",
    icon: ChartBarIcon,
    sections: [
      {
        title: "Observe",
        items: [
          { id: "runs", label: "Runs", icon: PlayIcon },
          { id: "logs", label: "Logs", icon: DocumentTextIcon },
          { id: "audit", label: "Audit", icon: ShieldCheckIcon },
          { id: "health", label: "Health", icon: HeartIcon },
        ],
      },
    ],
  },
  {
    id: "admin",
    label: "Admin",
    icon: Cog6ToothIcon,
    sections: [
      {
        title: "People",
        items: [
          { id: "members", label: "Members", icon: UsersIcon },
          { id: "teams", label: "Teams", icon: UserGroupIcon },
        ],
      },
      {
        title: "Access",
        items: [
          { id: "roles", label: "Roles", icon: KeyIcon },
          { id: "markings", label: "Markings", icon: TagIcon },
        ],
      },
      { title: "Workspace", items: [{ id: "settings", label: "Settings", icon: Cog6ToothIcon }] },
    ],
  },
];

export function locate(path: string) {
  const [, appId, itemId] = path.split("/");
  const app = apps.find((candidate) => candidate.id === appId) ?? apps[0];
  const items = app.sections.flatMap((section) => section.items);
  const item = items.find((candidate) => candidate.id === itemId) ?? items[0];
  return { app, item };
}

export function pathOf(app: App, item?: NavItem) {
  return `/${app.id}/${(item ?? app.sections[0].items[0]).id}`;
}
