import type { Api } from "./host";
import type { IconType } from "@astryxdesign/core/Icon";
import {
  AdjustmentsHorizontalIcon,
  ArchiveBoxIcon,
  ArrowUpTrayIcon,
  ArrowsRightLeftIcon,
  BeakerIcon,
  BellAlertIcon,
  BoltIcon,
  BookOpenIcon,
  BuildingOffice2Icon,
  BuildingOfficeIcon,
  BuildingStorefrontIcon,
  CalendarDaysIcon,
  ChartBarIcon,
  CircleStackIcon,
  ClipboardDocumentCheckIcon,
  CodeBracketIcon,
  Cog6ToothIcon,
  CommandLineIcon,
  CpuChipIcon,
  CubeIcon,
  CubeTransparentIcon,
  DocumentCheckIcon,
  DocumentTextIcon,
  EyeIcon,
  FolderIcon,
  HeartIcon,
  HomeIcon,
  InboxIcon,
  KeyIcon,
  LanguageIcon,
  LifebuoyIcon,
  LinkIcon,
  LockClosedIcon,
  MapIcon,
  PhotoIcon,
  PlayIcon,
  PresentationChartLineIcon,
  PuzzlePieceIcon,
  QueueListIcon,
  RectangleGroupIcon,
  RectangleStackIcon,
  ScaleIcon,
  ServerIcon,
  ServerStackIcon,
  ShareIcon,
  ShieldCheckIcon,
  SignalIcon,
  SparklesIcon,
  Square3Stack3DIcon,
  Squares2X2Icon,
  SquaresPlusIcon,
  SwatchIcon,
  TagIcon,
  TruckIcon,
  UserGroupIcon,
  UserIcon,
  UsersIcon,
  WindowIcon,
  WrenchScrewdriverIcon,
} from "@heroicons/react/24/outline";

export type NavItem = { id: string; label: string; icon: IconType; object?: string; description?: string; settings?: string[] };
export type NavSection = { title: string; items: NavItem[] };
export type RailGroup = "start" | "installed" | "build" | "run" | "manage";

const slug = (text: string) => text.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "settings";
export const settingsItemId = (title?: string) => `settings-${slug(title || "settings")}`;
export type App = { id: string; label: string; icon: IconType; badge?: boolean; group: RailGroup; sections: NavSection[]; dashboard?: Api.DashboardBlock[]; packageId?: string; hasDemo?: boolean; surface?: string };

export const apps: App[] = [
  {
    id: "home",
    group: "start",
    label: "Home",
    icon: HomeIcon,
    sections: [
      {
        title: "Workspace",
        items: [
          { id: "overview", label: "Overview", icon: Squares2X2Icon },
          { id: "inbox", label: "Inbox", icon: InboxIcon },
          { id: "proposals", label: "Proposals", icon: DocumentCheckIcon },
          { id: "projects", label: "Projects", icon: FolderIcon },
        ],
      },
    ],
  },
  {
    id: "data",
    group: "build",
    label: "Data",
    icon: CircleStackIcon,
    sections: [
      {
        title: "Ingest",
        items: [
          { id: "connections", label: "Connections", icon: LinkIcon },
          { id: "sources", label: "Sources", icon: ServerStackIcon },
          { id: "listeners", label: "Listeners", icon: SignalIcon },
          { id: "writebacks", label: "Writebacks", icon: ArrowUpTrayIcon },
          { id: "observation-sources", label: "Observation sources", icon: EyeIcon },
        ],
      },
      {
        title: "Transform",
        items: [
          { id: "datasets", label: "Datasets", icon: RectangleStackIcon },
          { id: "pipelines", label: "Pipelines", icon: ArrowsRightLeftIcon },
          { id: "schedules", label: "Schedules", icon: CalendarDaysIcon },
          { id: "matching-rules", label: "Matching rules", icon: ScaleIcon },
          { id: "media-sets", label: "Media sets", icon: PhotoIcon },
        ],
      },
      {
        title: "Trace",
        items: [
          { id: "lineage", label: "Lineage", icon: ShareIcon },
        ],
      },
    ],
  },
  {
    id: "ontology",
    group: "build",
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
          { id: "object-type-groups", label: "Object type groups", icon: Square3Stack3DIcon },
          { id: "contracts", label: "Capability contracts", icon: ArrowsRightLeftIcon },
        ],
      },
      {
        title: "Explore",
        items: [
          { id: "objects", label: "Objects", icon: CubeTransparentIcon },
        ],
      },
    ],
  },
  {
    id: "logic",
    group: "build",
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
    group: "build",
    label: "Apps",
    icon: RectangleGroupIcon,
    sections: [
      {
        title: "Build",
        items: [
          { id: "modules", label: "Modules", icon: RectangleGroupIcon },
          { id: "object-views", label: "Object views", icon: WindowIcon },
        ],
      },
    ],
  },
  {
    id: "ai",
    group: "build",
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
    group: "run",
    label: "Delivery",
    icon: TruckIcon,
    sections: [
      {
        title: "Release",
        items: [
          { id: "products", label: "Products", icon: CubeIcon },
          { id: "versions", label: "Versions", icon: TagIcon },
          { id: "test-plans", label: "Test plans", icon: BeakerIcon },
          { id: "sample-sets", label: "Sample sets", icon: QueueListIcon },
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
    group: "run",
    label: "Operations",
    icon: ChartBarIcon,
    sections: [
      {
        title: "Observe",
        items: [
          { id: "runs", label: "Runs", icon: PlayIcon },
          { id: "logs", label: "Logs", icon: DocumentTextIcon },
          { id: "traces", label: "Traces", icon: MapIcon },
          { id: "metrics", label: "Metrics", icon: PresentationChartLineIcon },
          { id: "alerts", label: "Alerts", icon: BellAlertIcon },
          { id: "health", label: "Health", icon: HeartIcon },
          { id: "audit", label: "Audit", icon: ShieldCheckIcon },
        ],
      },
    ],
  },
  {
    id: "admin",
    group: "manage",
    label: "Admin",
    icon: Cog6ToothIcon,
    sections: [
      {
        title: "People",
        items: [
          { id: "organization", label: "Organization", icon: BuildingOffice2Icon },
          { id: "people", label: "People", icon: UserIcon },
          { id: "members", label: "Members", icon: UsersIcon },
          { id: "teams", label: "Teams", icon: UserGroupIcon },
        ],
      },
      {
        title: "Data",
        items: [{ id: "master-data", label: "Master data", icon: ArchiveBoxIcon }],
      },
      {
        title: "Access",
        items: [
          { id: "roles", label: "Business roles", icon: KeyIcon },
          { id: "markings", label: "Markings", icon: TagIcon },
          { id: "support-grants", label: "Support grants", icon: LifebuoyIcon },
        ],
      },
      {
        title: "Workspace",
        items: [
          { id: "capabilities", label: "Capabilities", icon: SquaresPlusIcon },
          { id: "models", label: "Models", icon: CpuChipIcon },
          { id: "vocabulary", label: "Language and vocabulary", icon: LanguageIcon },
          { id: "credentials", label: "Credentials", icon: LockClosedIcon },
          { id: "settings", label: "Settings", icon: Cog6ToothIcon },
        ],
      },
    ],
  },
  {
    id: "host",
    group: "manage",
    label: "Host",
    icon: ServerIcon,
    sections: [
      {
        title: "Operate",
        items: [
          { id: "tenants", label: "Tenants", icon: BuildingOfficeIcon },
        ],
      },
    ],
  },
];

export function locate(path: string, available: App[] = apps) {
  const [, appId, itemId] = path.split("/");
  const app = available.find((candidate) => candidate.id === appId) ?? available[0];
  const items = app.sections.flatMap((section) => section.items);
  const item = items.find((candidate) => candidate.id === itemId) ?? items[0];
  return { app, item, found: (!appId || app.id === appId) && (!itemId || item.id === itemId) };
}

export function pathOf(app: App, item?: NavItem) {
  return `/${app.id}/${(item ?? app.sections[0].items[0]).id}`;
}
