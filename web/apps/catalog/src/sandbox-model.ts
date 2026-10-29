export type Snippet = { id: string; title: string; code: string; own?: boolean };
export type ConsoleMessage = { level: "log" | "info" | "warn" | "error"; text: string; truncated: boolean };

export function decodeSnippets(text: string | null): Snippet[] {
  const value: unknown = JSON.parse(text ?? "[]");
  if (!Array.isArray(value)) throw new Error("Invalid snippet library");
  const ids = new Set<string>();
  return value.map((item) => {
    if (!item || typeof item !== "object" || typeof item.id !== "string" || !item.id
      || typeof item.title !== "string" || typeof item.code !== "string" || ids.has(item.id)
      || Object.keys(item).some((key) => !["id", "title", "code", "own"].includes(key))) throw new Error("Invalid snippet library");
    ids.add(item.id);
    return { id: item.id, title: item.title, code: item.code, own: true };
  });
}

export function consoleMessage(value: unknown): ConsoleMessage | undefined {
  if (!value || typeof value !== "object") return undefined;
  const item = value as Record<string, unknown>;
  if (item.sandbox !== true || typeof item.text !== "string" || typeof item.level !== "string" || !["log", "info", "warn", "error"].includes(item.level)) return undefined;
  return { level: item.level as ConsoleMessage["level"], text: item.text.slice(0, 65536), truncated: item.text.length > 65536 };
}

export function asComponent(code: string): string {
  const body = /<body[^>]*>([\s\S]*?)<\/body>/i.exec(code)?.[1]?.trim() ?? code.trim();
  const style = [...code.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/gi)].map((match) => (match[1] ?? "").trim()).join("\n");
  return `// Generated from the sandbox: replace the markup with @platform/ui primitives before publishing.
export function Snippet() {
  return <>
    ${style ? `<style>{${JSON.stringify(style)}}</style>\n    ` : ""}<div dangerouslySetInnerHTML={{ __html: ${JSON.stringify(body)} }} />
  </>;
}
`;
}

export function previewDocument(code: string, bridge: string): string {
  if (/<head(?:\s[^>]*)?>/i.test(code)) return code.replace(/<head(?:\s[^>]*)?>/i, (head) => head + bridge);
  const doctype = /^\s*<!doctype[^>]*>/i.exec(code)?.[0];
  return doctype ? doctype + bridge + code.slice(doctype.length) : bridge + code;
}
