import type { IconType } from "@astryxdesign/core/Icon";
import {
  AcademicCapIcon, AdjustmentsHorizontalIcon, ArchiveBoxIcon, ArrowTrendingUpIcon, BanknotesIcon, BeakerIcon, BellIcon, BoltIcon, BookOpenIcon,
  BriefcaseIcon, BuildingLibraryIcon, BuildingOffice2Icon, BuildingOfficeIcon, BuildingStorefrontIcon, CalculatorIcon, CalendarDaysIcon,
  CalendarIcon, CameraIcon, ChartBarIcon, ChartPieIcon, ChatBubbleBottomCenterTextIcon, ChatBubbleLeftRightIcon, CheckCircleIcon,
  ClipboardDocumentCheckIcon, ClipboardDocumentIcon, ClipboardDocumentListIcon, ClockIcon, Cog6ToothIcon, CommandLineIcon, CpuChipIcon,
  CreditCardIcon, CubeIcon, CurrencyDollarIcon, DocumentIcon, DocumentTextIcon, EnvelopeIcon, FilmIcon, FireIcon, FlagIcon, FolderIcon, FunnelIcon,
  GiftIcon, GlobeAltIcon, HandRaisedIcon, HeartIcon, HomeIcon, HomeModernIcon, IdentificationIcon, InboxIcon, KeyIcon, LifebuoyIcon, LightBulbIcon,
  ListBulletIcon, MapIcon, MapPinIcon, MegaphoneIcon, MusicalNoteIcon, NewspaperIcon, NoSymbolIcon, PaperClipIcon, PencilSquareIcon, PhoneIcon,
  PhotoIcon, PresentationChartLineIcon, PuzzlePieceIcon, QueueListIcon, ReceiptPercentIcon, RectangleGroupIcon, RocketLaunchIcon, ScaleIcon,
  ServerIcon, ShieldCheckIcon, ShieldExclamationIcon, ShoppingBagIcon, ShoppingCartIcon, SparklesIcon, Squares2X2Icon, StarIcon, TagIcon, TicketIcon,
  TrophyIcon, TruckIcon, UserCircleIcon, UserGroupIcon, UserIcon, UserPlusIcon, UsersIcon, WalletIcon, WrenchScrewdriverIcon, XCircleIcon,
} from "@heroicons/react/24/outline";

const set: Record<string, IconType> = {
  AcademicCapIcon, AdjustmentsHorizontalIcon, ArchiveBoxIcon, ArrowTrendingUpIcon, BanknotesIcon, BeakerIcon, BellIcon, BoltIcon, BookOpenIcon,
  BriefcaseIcon, BuildingLibraryIcon, BuildingOffice2Icon, BuildingOfficeIcon, BuildingStorefrontIcon, CalculatorIcon, CalendarDaysIcon,
  CalendarIcon, CameraIcon, ChartBarIcon, ChartPieIcon, ChatBubbleBottomCenterTextIcon, ChatBubbleLeftRightIcon, CheckCircleIcon,
  ClipboardDocumentCheckIcon, ClipboardDocumentIcon, ClipboardDocumentListIcon, ClockIcon, Cog6ToothIcon, CommandLineIcon, CpuChipIcon,
  CreditCardIcon, CubeIcon, CurrencyDollarIcon, DocumentIcon, DocumentTextIcon, EnvelopeIcon, FilmIcon, FireIcon, FlagIcon, FolderIcon, FunnelIcon,
  GiftIcon, GlobeAltIcon, HandRaisedIcon, HeartIcon, HomeIcon, HomeModernIcon, IdentificationIcon, InboxIcon, KeyIcon, LifebuoyIcon, LightBulbIcon,
  ListBulletIcon, MapIcon, MapPinIcon, MegaphoneIcon, MusicalNoteIcon, NewspaperIcon, NoSymbolIcon, PaperClipIcon, PencilSquareIcon, PhoneIcon,
  PhotoIcon, PresentationChartLineIcon, PuzzlePieceIcon, QueueListIcon, ReceiptPercentIcon, RectangleGroupIcon, RocketLaunchIcon, ScaleIcon,
  ServerIcon, ShieldCheckIcon, ShieldExclamationIcon, ShoppingBagIcon, ShoppingCartIcon, SparklesIcon, Squares2X2Icon, StarIcon, TagIcon, TicketIcon,
  TrophyIcon, TruckIcon, UserCircleIcon, UserGroupIcon, UserIcon, UserPlusIcon, UsersIcon, WalletIcon, WrenchScrewdriverIcon, XCircleIcon,
};

const kebab = (name: string) => name.replace(/Icon$/, "").replace(/([a-z])([A-Z0-9])/g, "$1-$2").replace(/([A-Z0-9])([A-Z][a-z])/g, "$1-$2").toLowerCase();

export const icons: Record<string, IconType> = Object.fromEntries(Object.entries(set).map(([name, icon]) => [kebab(name), icon]));

export const iconOf = (name: string | undefined, fallback: IconType) => (name && icons[name]) || fallback;

export const iconField = (name: string, type: string) => name === "icon" && (type === "text" || type === "string");
