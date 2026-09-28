import { useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { t } from "../i18n";
import type { EditorProps } from "../fields/types";

/** Renders standard Markdown text safely into React elements without dangerous HTML. */
export function Markdown({ content = "", className }: { content?: string; className?: string }) {
  if (!content) return null;

  const lines = content.split("\n");
  const nodes: ReactNode[] = [];
  let inCodeBlock = false;
  let codeBlockLines: string[] = [];
  let listItems: ReactNode[] = [];
  let isOrderedList = false;

  const flushList = () => {
    if (listItems.length === 0) return;
    const key = `list-${nodes.length}`;
    if (isOrderedList) {
      nodes.push(<ol key={key} className="my-1.5 ml-4 list-decimal space-y-0.5 text-sm">{listItems}</ol>);
    } else {
      nodes.push(<ul key={key} className="my-1.5 ml-4 list-disc space-y-0.5 text-sm">{listItems}</ul>);
    }
    listItems = [];
    isOrderedList = false;
  };

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i]!;

    // Code blocks
    if (rawLine.startsWith("```")) {
      if (inCodeBlock) {
        nodes.push(
          <pre key={`code-${i}`} className="my-2 overflow-x-auto rounded border border-border/80 bg-muted/20 p-2.5 font-mono text-xs text-foreground">
            <code>{codeBlockLines.join("\n")}</code>
          </pre>
        );
        codeBlockLines = [];
        inCodeBlock = false;
      } else {
        flushList();
        inCodeBlock = true;
      }
      continue;
    }

    if (inCodeBlock) {
      codeBlockLines.push(rawLine);
      continue;
    }

    const trimmed = rawLine.trim();

    if (!trimmed) {
      flushList();
      continue;
    }

    // Headings
    if (trimmed.startsWith("### ")) {
      flushList();
      nodes.push(<h3 key={i} className="mb-1 mt-2.5 text-sm font-semibold">{parseInline(trimmed.slice(4))}</h3>);
      continue;
    }
    if (trimmed.startsWith("## ")) {
      flushList();
      nodes.push(<h2 key={i} className="mb-1 mt-3 text-base font-semibold">{parseInline(trimmed.slice(3))}</h2>);
      continue;
    }
    if (trimmed.startsWith("# ")) {
      flushList();
      nodes.push(<h1 key={i} className="mb-1.5 mt-3.5 text-lg font-bold">{parseInline(trimmed.slice(2))}</h1>);
      continue;
    }

    // Blockquote
    if (trimmed.startsWith("> ")) {
      flushList();
      nodes.push(
        <blockquote key={i} className="my-1.5 border-l-2 border-border pl-3 italic text-muted text-sm">
          {parseInline(trimmed.slice(2))}
        </blockquote>
      );
      continue;
    }

    // Unordered list (- or *)
    if (/^[-*]\s+/.test(trimmed)) {
      if (isOrderedList) flushList();
      isOrderedList = false;
      listItems.push(<li key={`li-${i}`}>{parseInline(trimmed.replace(/^[-*]\s+/, ""))}</li>);
      continue;
    }

    // Ordered list (1. 2. etc.)
    if (/^\d+\.\s+/.test(trimmed)) {
      if (!isOrderedList && listItems.length > 0) flushList();
      isOrderedList = true;
      listItems.push(<li key={`li-${i}`}>{parseInline(trimmed.replace(/^\d+\.\s+/, ""))}</li>);
      continue;
    }

    // Regular paragraph
    flushList();
    nodes.push(<p key={i} className="my-1 text-sm leading-relaxed">{parseInline(rawLine)}</p>);
  }

  flushList();
  if (inCodeBlock && codeBlockLines.length > 0) {
    nodes.push(
      <pre key="code-unclosed" className="my-2 overflow-x-auto rounded border border-border/80 bg-muted/20 p-2.5 font-mono text-xs text-foreground">
        <code>{codeBlockLines.join("\n")}</code>
      </pre>
    );
  }

  return <div className={cn("markdown-body text-sm", className)}>{nodes}</div>;
}

/** Parses inline markdown spans: `code`, **bold**, *italic*, [text](url). */
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

/** Rich tabbed editor for Markdown content with Write and Preview views. */
export function MarkdownEditor({
  id,
  value = "",
  onChange,
  invalid,
  autoFocus,
  placeholder,
  rows = 8,
}: EditorProps<string> & { placeholder?: string; rows?: number }) {
  const [tab, setTab] = useState<"write" | "preview">("write");

  return (
    <div className="flex flex-col rounded-md border border-border bg-surface text-sm">
      <div className="flex items-center justify-between border-b border-border bg-muted/10 px-2 py-1">
        <div className="flex gap-1" role="tablist" aria-label={t("Editor tabs")}>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "write"}
            onClick={() => setTab("write")}
            className={cn(
              "rounded px-2 py-0.5 text-xs font-medium transition-colors",
              tab === "write" ? "bg-surface text-foreground shadow-xs font-semibold" : "text-muted hover:text-foreground"
            )}
          >
            {t("Write")}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "preview"}
            onClick={() => setTab("preview")}
            className={cn(
              "rounded px-2 py-0.5 text-xs font-medium transition-colors",
              tab === "preview" ? "bg-surface text-foreground shadow-xs font-semibold" : "text-muted hover:text-foreground"
            )}
          >
            {t("Preview")}
          </button>
        </div>
        <span className="text-[11px] text-muted">
          {(value ?? "").length} {t("characters")}
        </span>
      </div>

      {tab === "write" ? (
        <textarea
          id={id}
          value={value ?? ""}
          onChange={(e) => onChange(e.target.value || undefined)}
          aria-invalid={invalid}
          autoFocus={autoFocus}
          placeholder={placeholder ?? t("Write markdown here…")}
          rows={rows}
          className="w-full resize-y rounded-b-md bg-transparent p-2.5 font-mono text-xs leading-relaxed outline-none focus-visible:ring-1 focus-visible:ring-ring"
        />
      ) : (
        <div className="min-h-[140px] max-h-[360px] overflow-y-auto p-3">
          {value ? (
            <Markdown content={value} />
          ) : (
            <p className="text-xs italic text-muted">{t("Nothing to preview.")}</p>
          )}
        </div>
      )}
    </div>
  );
}
