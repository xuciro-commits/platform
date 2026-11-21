import type { IconType } from "@astryxdesign/core/Icon";
import {
  AcademicCapIcon, ArchiveBoxIcon, BanknotesIcon, BeakerIcon, BellIcon, BoltIcon, BookOpenIcon, BriefcaseIcon, BuildingOfficeIcon, BuildingStorefrontIcon,
  CalendarDaysIcon, CalendarIcon, CameraIcon, ChartBarIcon, ChartPieIcon, ChatBubbleLeftRightIcon, CheckCircleIcon, ClipboardDocumentCheckIcon,
  ClipboardDocumentListIcon, ClockIcon, Cog6ToothIcon, CommandLineIcon, CpuChipIcon, CreditCardIcon, CubeIcon, CurrencyDollarIcon, DocumentIcon,
  DocumentTextIcon, EnvelopeIcon, FilmIcon, FireIcon, FlagIcon, FolderIcon, GiftIcon, GlobeAltIcon, HeartIcon, HomeIcon, InboxIcon, KeyIcon,
  LifebuoyIcon, LightBulbIcon, ListBulletIcon, MapPinIcon, MegaphoneIcon, MusicalNoteIcon, NewspaperIcon, PhoneIcon, PhotoIcon,
  PresentationChartLineIcon, PuzzlePieceIcon, RectangleGroupIcon, RocketLaunchIcon, ScaleIcon, ServerIcon, ShieldCheckIcon, ShoppingBagIcon,
  ShoppingCartIcon, SparklesIcon, Squares2X2Icon, StarIcon, TagIcon, TicketIcon, TrophyIcon, TruckIcon, UserGroupIcon, UserIcon, UsersIcon,
  WrenchScrewdriverIcon,
} from "@heroicons/react/24/outline";

const set: Record<string, IconType> = {
  AcademicCapIcon, ArchiveBoxIcon, BanknotesIcon, BeakerIcon, BellIcon, BoltIcon, BookOpenIcon, BriefcaseIcon, BuildingOfficeIcon, BuildingStorefrontIcon,
  CalendarDaysIcon, CalendarIcon, CameraIcon, ChartBarIcon, ChartPieIcon, ChatBubbleLeftRightIcon, CheckCircleIcon, ClipboardDocumentCheckIcon,
  ClipboardDocumentListIcon, ClockIcon, Cog6ToothIcon, CommandLineIcon, CpuChipIcon, CreditCardIcon, CubeIcon, CurrencyDollarIcon, DocumentIcon,
  DocumentTextIcon, EnvelopeIcon, FilmIcon, FireIcon, FlagIcon, FolderIcon, GiftIcon, GlobeAltIcon, HeartIcon, HomeIcon, InboxIcon, KeyIcon,
  LifebuoyIcon, LightBulbIcon, ListBulletIcon, MapPinIcon, MegaphoneIcon, MusicalNoteIcon, NewspaperIcon, PhoneIcon, PhotoIcon,
  PresentationChartLineIcon, PuzzlePieceIcon, RectangleGroupIcon, RocketLaunchIcon, ScaleIcon, ServerIcon, ShieldCheckIcon, ShoppingBagIcon,
  ShoppingCartIcon, SparklesIcon, Squares2X2Icon, StarIcon, TagIcon, TicketIcon, TrophyIcon, TruckIcon, UserGroupIcon, UserIcon, UsersIcon,
  WrenchScrewdriverIcon,
};

const kebab = (name: string) => name.replace(/Icon$/, "").replace(/([a-z0-9])([A-Z])/g, "$1-$2").replace(/([A-Z])([A-Z][a-z])/g, "$1-$2").toLowerCase();

export const icons: Record<string, IconType> = Object.fromEntries(Object.entries(set).map(([name, icon]) => [kebab(name), icon]));

export const iconOf = (name: string | undefined, fallback: IconType) => (name && icons[name]) || fallback;

export const iconField = (name: string, type: string) => name === "icon" && (type === "text" || type === "string");
