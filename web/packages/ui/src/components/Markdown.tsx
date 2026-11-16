import { Markdown as AstryxMarkdown } from "@astryxdesign/core/Markdown";
import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import type { EditorProps } from "../fields/types";
import { ContentTabs } from "../layout/ContentTabs";
import { Textarea } from "../primitives/input";

export function Markdown({ content = "", className }: { content?: string; className?: string }) {
  if (!content) return null;
  return (
    <div className={className}>
      <AstryxMarkdown density="compact">{content}</AstryxMarkdown>
    </div>
  );
}

export function parseInline(text: string): ReactNode {
  const regex = /(`[^`]+`|\*\*[^*]+\*\*|\*[^*]+\*|\[[^\]]+\]\(https?:\/\/[^\s)]+\))/g;
  const parts = text.split(regex);

  return parts.map((part, idx) => {
    if (part.startsWith("`") && part.endsWith("`") && part.length >= 2) {
      return <code key={idx} className="rounded bg-muted/30 px-1 py-0.5 font-mono text-xs">{part.slice(1, -1)}</code>;
    }
    if (part.startsWith("**") && part.endsWith("**") && part.length >= 4) {
      return <strong key={idx} className="font-semibold">{part.slice(2, -2)}</strong>;
    }
    if (part.startsWith("*") && part.endsWith("*") && part.length >= 2) {
      return <em key={idx}>{part.slice(1, -1)}</em>;
    }
    const linkMatch = part.match(/^\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)$/);
    if (linkMatch) {
      return (
        <a key={idx} href={linkMatch[2]} target="_blank" rel="noreferrer" className="text-primary hover:underline">
          {linkMatch[1]}
        </a>
      );
    }
    return part;
  });
}

export function MarkdownEditor({
  id,
  value = "",
  onChange,
  invalid,
  describedBy,
  autoFocus,
  placeholder,
  rows = 8,
}: EditorProps<string> & { placeholder?: string; rows?: number }) {
  const [tab, setTab] = useState<"write" | "preview">("write");

  return (
    <div className="flex flex-col rounded-md border border-border bg-surface text-sm">
      <ContentTabs label={t("Editor tabs")} compact value={tab} onChange={(next) => setTab(next as "write" | "preview")}
        className="flex-none [&>div:first-child]:bg-muted/10"
        actions={<span className="pr-2 text-xs text-muted">{(value ?? "").length} {t("characters")}</span>}
        items={[{ id: "write", title: t("Write"), content: tab === "write" ? (
        <Textarea
          id={id}
          aria-label={t("Markdown")}
          value={value ?? ""}
          onChange={(e) => onChange(e.target.value || undefined)}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          autoFocus={autoFocus}
          placeholder={placeholder ?? t("Write markdown here…")}
          rows={rows}
          className="w-full font-mono text-xs"
        />
      ) : null }, { id: "preview", title: t("Preview"), content: tab === "preview" ? (
        <div className="min-h-[140px] max-h-[360px] overflow-y-auto p-3">
          {value ? (
            <Markdown content={value} />
          ) : (
            <p className="text-xs italic text-muted">{t("Nothing to preview.")}</p>
          )}
        </div>
      ) : null }]} />
    </div>
  );
}
