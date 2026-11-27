import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { Token } from "@astryxdesign/core/Token";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { useToast } from "@astryxdesign/core/Toast";
import {
  CheckIcon,
  ClipboardDocumentIcon,
  DocumentIcon,
  EnvelopeIcon,
  GlobeAltIcon,
  MinusIcon,
  PhoneIcon,
  PhotoIcon,
} from "@heroicons/react/24/outline";
import { useState } from "react";
import { type Api, useRead, reasonOf, send } from "../host";
import { currentTimeZone, dateText, dayText, t, useFormatConfig } from "../i18n";
import { kindOf, MediaPreview } from "../pages/MediaPreview";
import { motionStyles } from "../pages/motion";
import { extractUrlDisplay, normalizeExternalUrl } from "./format";
import { iconSizeOf, type SemanticSize, type SemanticSurface, textTypeOf, tokenSizeOf } from "./types";

function CopyAction({ value, size = "sm" }: { value: string; size?: SemanticSize }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <IconButton
      label={copied ? t("Copied") : t("Copy")}
      tooltip={copied ? t("Copied") : t("Copy")}
      variant="ghost"
      size="sm"
      icon={
        <HStack key={copied ? "copied" : "copy"} xstyle={motionStyles.popEnter}>
          <Icon icon={copied ? CheckIcon : ClipboardDocumentIcon} color={copied ? "success" : undefined} size={iconSizeOf(size)} />
        </HStack>
      }
      onClick={(event) => {
        event.stopPropagation();
        copy();
      }}
      onPointerDown={(event) => event.stopPropagation()}
    />
  );
}

export function TemporalValue({
  value,
  kind = "datetime",
  mode,
  size = "sm",
  surface = "detail",
  color,
}: {
  value: unknown;
  kind?: "date" | "datetime";
  mode?: "relative" | "auto" | "absolute";
  size?: SemanticSize;
  surface?: SemanticSurface;
  color?: "primary" | "secondary";
}) {
  useFormatConfig();
  if (value == null || value === "") return null;
  const raw = String(value).trim();
  if (!raw) return null;
  const zone = currentTimeZone();
  const effectiveColor = color ?? (surface === "cell" || surface === "card" || size === "xs" ? "secondary" : "primary");

  if (kind === "date" && /^\d{4}-\d{2}-\d{2}$/.test(raw)) {
    const shown = dayText(raw);
    return (
      <Text type={textTypeOf(size)} color={effectiveColor} hasTabularNumbers maxLines={1}>
        {shown}
      </Text>
    );
  }

  const effectiveMode = mode ?? (surface === "cell" || surface === "card" || surface === "inline" ? "relative" : "absolute");
  if (effectiveMode === "relative" || effectiveMode === "auto") {
    return (
      <Text type={textTypeOf(size)} color={effectiveColor} hasTabularNumbers maxLines={1}>
        <Timestamp
          value={raw}
          format={effectiveMode === "relative" ? (size === "xs" ? "relative_short" : "relative") : "auto"}
          timezoneID={zone}
          type={textTypeOf(size)}
          color={effectiveColor}
        />
      </Text>
    );
  }

  const absolute = dateText(raw);
  return (
    <Text type={textTypeOf(size)} color={effectiveColor} hasTabularNumbers maxLines={1}>
      {absolute}
    </Text>
  );
}

export function BooleanValue({
  value,
  size = "sm",
  surface = "detail",
}: {
  value: unknown;
  size?: SemanticSize;
  surface?: SemanticSurface;
}) {
  if (value == null || value === "") return null;
  const checked = value === true || value === "true" || value === t("Yes");
  if (surface === "cell" || surface === "card") {
    return checked ? <Icon icon={CheckIcon} size={iconSizeOf(size)} label={t("Yes")} /> : null;
  }
  return (
    <HStack gap={1.5} vAlign="center">
      <Icon icon={checked ? CheckIcon : MinusIcon} size={iconSizeOf(size)} color={checked ? "success" : "secondary"} />
      <Text type={textTypeOf(size)} color={checked ? "primary" : "secondary"}>
        {checked ? t("Yes") : t("No")}
      </Text>
    </HStack>
  );
}

export function CodeValue({
  value,
  size = "sm",
  copyable = false,
  color = "secondary",
}: {
  value: unknown;
  size?: SemanticSize;
  copyable?: boolean;
  color?: "primary" | "secondary";
}) {
  if (value == null || value === "") return null;
  const text = String(value);
  if (!text) return null;
  const codeNode = (
    <Text type="code" size={size === "xs" ? "xsm" : undefined} color={color} maxLines={1}>
      {text}
    </Text>
  );
  if (!copyable) return codeNode;
  return (
    <HStack gap={1} vAlign="center">
      {codeNode}
      <CopyAction value={text} size={size} />
    </HStack>
  );
}

export function UrlValue({
  value,
  size = "md",
  surface = "detail",
  interactive = true,
}: {
  value: unknown;
  size?: SemanticSize;
  surface?: SemanticSurface;
  interactive?: boolean;
}) {
  if (value == null || value === "") return null;
  const raw = String(value).trim();
  if (!raw) return null;
  const href = normalizeExternalUrl(raw);
  const label = extractUrlDisplay(raw);
  if (!href) {
    return <Text type={textTypeOf(size)} maxLines={1}>{raw}</Text>;
  }
  return (
    <HStack
      gap={1.5}
      vAlign="center"
      onClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => event.stopPropagation()}
    >
      <Icon icon={GlobeAltIcon} size={iconSizeOf(size)} color="secondary" />
      <Tooltip content={href} hasHoverIndication={false}>
        <Link href={href} target="_blank" rel="noopener noreferrer" type={textTypeOf(size)} maxLines={1}>
          {label}
        </Link>
      </Tooltip>
      {interactive && surface === "detail" && <CopyAction value={href} size={size} />}
    </HStack>
  );
}

export function ContactValue({
  kind,
  value,
  size = "md",
  surface = "detail",
  recordType,
  recordId,
  recordRevision = 0,
  interactive = true,
}: {
  kind: "email" | "phone";
  value: unknown;
  size?: SemanticSize;
  surface?: SemanticSurface;
  recordType?: string;
  recordId?: string;
  recordRevision?: number;
  interactive?: boolean;
}) {
  const toast = useToast();
  const catalog = useRead<Api.Action[]>(kind === "phone" && interactive && surface === "detail" ? "/v1/actions" : null);
  if (value == null || value === "") return null;
  const text = String(value).trim();
  if (!text) return null;

  const dialAction = kind === "phone" && catalog.status === "ready"
    ? (catalog.data.find((action) => action.target === recordType && action.uses?.some((u) => u === "call/1#dial" || u.startsWith("call/1"))) ??
       catalog.data.find((action) => action.new && action.uses?.some((u) => u === "call/1#dial" || u.startsWith("call/1"))))
    : undefined;

  const launch = async () => {
    if (kind === "email") {
      window.location.href = `mailto:${text}`;
      return;
    }
    const digits = text.replace(/[^\d+]/g, "");
    if (dialAction) {
      try {
        const target = dialAction.new ? `CALL-${crypto.randomUUID()}` : (recordId || "");
        const payload: Record<string, unknown> = {};
        for (const param of dialAction.payload) {
          if (/number|phone|tel/i.test(param.name)) payload[param.name] = digits || text;
          else if (recordId && /about|record|source|contact/i.test(param.name)) payload[param.name] = recordId;
        }
        await send(dialAction, target, payload, dialAction.new ? 0 : recordRevision);
        return;
      } catch (problem) {
        toast({ body: reasonOf(problem), type: "error" });
        return;
      }
    }
    window.location.href = `tel:${digits}`;
  };

  if (surface === "cell" || surface === "card" || !interactive) {
    return (
      <Tooltip content={text} hasHoverIndication={false}>
        <Text type={textTypeOf(size)} hasTabularNumbers={kind === "phone"} maxLines={1}>
          {text}
        </Text>
      </Tooltip>
    );
  }

  return (
    <HStack gap={1} vAlign="center">
      <Text type={textTypeOf(size)} hasTabularNumbers={kind === "phone"} maxLines={1}>
        {text}
      </Text>
      <HStack gap={0} vAlign="center" onClick={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
        <CopyAction value={text} size={size} />
        <IconButton
          label={kind === "email" ? t("Send email") : t("Call")}
          tooltip={kind === "email" ? t("Send email") : t("Call")}
          variant="ghost"
          size="sm"
          icon={<Icon icon={kind === "email" ? EnvelopeIcon : PhoneIcon} size={iconSizeOf(size)} />}
          onClick={() => void launch()}
        />
      </HStack>
    </HStack>
  );
}

type AttachmentRef = { mediaSet?: string; item?: string };

export function parseAttachment(value: unknown): AttachmentRef | undefined {
  if (!value || typeof value !== "object") return undefined;
  const candidate = value as Record<string, unknown>;
  if (typeof candidate.item === "string" && candidate.item) {
    return {
      mediaSet: typeof candidate.mediaSet === "string" ? candidate.mediaSet : undefined,
      item: candidate.item,
    };
  }
  return undefined;
}

export function AttachmentValue({
  value,
  size = "sm",
}: {
  value: unknown;
  size?: SemanticSize;
}) {
  const toast = useToast();
  const [previewing, setPreviewing] = useState(false);
  const parsed = parseAttachment(value);
  const media = useRead<Api.RecordView>(parsed?.mediaSet ? `/v1/records/build.mediaset/${encodeURIComponent(parsed.mediaSet)}` : null);
  if (!parsed?.item) return null;
  const items = media.status === "ready" ? ((media.data.record as { items?: { hash: string; name: string }[] }).items ?? []) : [];
  const matched = items.find((entry) => entry.hash === parsed.item);
  const fileName = matched?.name ?? `${parsed.item.slice(0, 10)}…`;
  const mediaKind = kindOf({ name: fileName });
  const FileIcon = mediaKind === "image" ? PhotoIcon : DocumentIcon;

  return (
    <>
      <HStack
        gap={1}
        vAlign="center"
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Token
          size={tokenSizeOf(size)}
          color="default"
          icon={<Icon icon={FileIcon} size={iconSizeOf(size)} />}
          label={fileName}
          onClick={mediaKind ? () => setPreviewing(true) : undefined}
        />
      </HStack>
      {previewing && (
        <MediaPreview
          media={{ path: `/v1/files/${encodeURIComponent(parsed.item)}`, name: fileName }}
          onClose={() => setPreviewing(false)}
          onFailure={(reason) => toast({ body: reason, type: "error" })}
        />
      )}
    </>
  );
}
