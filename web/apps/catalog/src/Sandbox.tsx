// The code sandbox (ADR-0054 D4): an IDE for HTML/CSS/JS prototypes. Left:
// snippets (presets and the member's own, kept in this browser). Main: the
// editor. Right: the live preview at a chosen device width. Dock: the console
// of the preview frame. Running, copying, saving and "copy as React component"
// are title-bar commands, not pages.
import { Button, PanelSection, StructureRow, Textarea, Workbench, language, notify, t } from "@platform/ui";
import { Copy, FileCode, Maximize2, Minimize2, Monitor, Play, Plus, RotateCcw, Save, Smartphone, Tablet, Trash2, Braces, Eraser } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import INDUSTRIAL_INSTRUMENT from "./presets/industrial-hmi.html?raw";
import HMI_CONSOLE from "./presets/hmi-console.html?raw";
import METAL_BUTTON from "./presets/metal-button.html?raw";
import RETRO_BUTTON from "./presets/retro-button.html?raw";

type Snippet = { id: string; title: string; code: string; own?: boolean };
const presets: () => Snippet[] = () => [
  { id: "industrial-instrument", title: t("Industrial instrument panel"), code: INDUSTRIAL_INSTRUMENT },
  { id: "hmi-console", title: t("Furnace HMI console"), code: HMI_CONSOLE },
  { id: "metal-button", title: t("Brushed metal button"), code: METAL_BUTTON },
  { id: "retro-button", title: t("Retro raised button"), code: RETRO_BUTTON },
];
const OWN_KEY = "sandbox.snippets";
const readOwn = (): Snippet[] => { try { return JSON.parse(localStorage.getItem(OWN_KEY) ?? "[]"); } catch { return []; } };
const writeOwn = (list: Snippet[]) => { try { localStorage.setItem(OWN_KEY, JSON.stringify(list)); } catch { /* storage unavailable */ } };
const widths = [["100%", Monitor, "Desktop"], ["768px", Tablet, "Tablet"], ["375px", Smartphone, "Phone"]] as const;

/** Forwards the frame's console and errors to the dock; injected before the member's markup. */
const bridge = `<script>(function(){var send=function(level,args){try{parent.postMessage({sandbox:true,level:level,text:Array.prototype.map.call(args,function(a){try{return typeof a==="string"?a:JSON.stringify(a)}catch(e){return String(a)}}).join(" ")},"*")}catch(e){}};
["log","info","warn","error"].forEach(function(l){var o=console[l];console[l]=function(){send(l,arguments);o&&o.apply(console,arguments)}});
window.addEventListener("error",function(e){send("error",[e.message+" ("+e.lineno+":"+e.colno+")"])});window.addEventListener("unhandledrejection",function(e){send("error",[String(e.reason)])})})()</script>`;
type Line = { at: string; level: string; text: string };

/** Wraps an HTML prototype as a React component skeleton for `@platform/ui` work. */
function asComponent(code: string): string {
  const body = /<body[^>]*>([\s\S]*?)<\/body>/i.exec(code)?.[1]?.trim() ?? code.trim();
  const style = [...code.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/gi)].map((m) => (m[1] ?? "").trim()).join("\n");
  return `// Generated from the sandbox: replace the markup with @platform/ui primitives before publishing.
export function Snippet() {
  return <>
    ${style ? `<style>{\`${style.replace(/`/g, "\\`")}\`}</style>\n    ` : ""}<div dangerouslySetInnerHTML={{ __html: \`${body.replace(/`/g, "\\`").replace(/\$\{/g, "\\${")}\` }} />
  </>;
}
`;
}

export function SandboxView() {
  const library = useMemo(presets, [language()]);
  const [own, setOwn] = useState<Snippet[]>(readOwn);
  const [current, setCurrent] = useState<string>(library[0]!.id);
  const [code, setCode] = useState(library[0]!.code);
  const [running, setRunning] = useState(code);
  const [width, setWidth] = useState<(typeof widths)[number][0]>("100%");
  const [big, setBig] = useState(false);
  const [lines, setLines] = useState<Line[]>([]);
  const all = [...own, ...library];
  const dirty = code !== (all.find((s) => s.id === current)?.code ?? "");
  useEffect(() => {
    const onMessage = (event: MessageEvent) => { const m = event.data; if (m && m.sandbox) setLines((old) => [...old.slice(-199), { at: new Date().toLocaleTimeString(), level: m.level, text: m.text }]); };
    addEventListener("message", onMessage);
    return () => removeEventListener("message", onMessage);
  }, []);
  const run = useCallback(() => { setLines([]); setRunning(code); }, [code]);
  const choose = (snippet: Snippet) => { setCurrent(snippet.id); setCode(snippet.code); setRunning(snippet.code); setLines([]); };
  const save = useCallback(() => {
    const existing = own.find((s) => s.id === current);
    const next = existing ? own.map((s) => s.id === current ? { ...s, code } : s)
      : [{ id: `own-${Date.now()}`, title: `${t("Snippet")} ${own.length + 1}`, code, own: true }, ...own];
    setOwn(next); writeOwn(next);
    if (!existing) setCurrent(next[0]!.id);
    notify.success(t("Snippet saved in this browser."));
  }, [own, current, code]);
  const rename = (snippet: Snippet) => { const title = prompt(t("Snippet name"), snippet.title)?.trim(); if (!title) return; const next = own.map((s) => s.id === snippet.id ? { ...s, title } : s); setOwn(next); writeOwn(next); };
  const remove = (snippet: Snippet) => { const next = own.filter((s) => s.id !== snippet.id); setOwn(next); writeOwn(next); if (current === snippet.id) choose(library[0]!); };
  const copy = async (text: string, done: string) => { try { await navigator.clipboard.writeText(text); notify.success(done); } catch { notify.error(t("Clipboard unavailable. Select and copy the code below.")); } };
  const preview = <div className="flex h-full min-h-0 items-start justify-center overflow-auto bg-surface p-3">
    <iframe title={t("Sandbox preview")} srcDoc={bridge + running} sandbox="allow-scripts" style={{ width, minHeight: "100%" }} className="h-full min-h-[24rem] rounded border border-border bg-white shadow-sm transition-[width] duration-200" />
  </div>;
  const toolbar = <div className="flex items-center gap-1 border-b border-border px-2 py-1 text-xs">
    {widths.map(([w, Icon, label]) => <Button key={w} size="sm" variant={width === w ? "primary" : "ghost"} aria-label={t(label)} title={t(label)} onClick={() => setWidth(w)}><Icon />{w}</Button>)}
    <span className="ml-auto" /><Button size="sm" variant="ghost" aria-label={big ? t("Back to the editor") : t("Expand preview")} title={big ? t("Back to the editor") : t("Expand preview")} onClick={() => setBig((v) => !v)}>{big ? <Minimize2 /> : <Maximize2 />}</Button>
  </div>;
  const snippetRow = (snippet: Snippet) => <StructureRow key={snippet.id} icon={<FileCode />} label={snippet.title} selected={snippet.id === current} onClick={() => choose(snippet)}
    actions={snippet.own ? <><Button size="sm" variant="ghost" aria-label={t("Rename")} title={t("Rename")} onClick={(e) => { e.stopPropagation(); rename(snippet); }}><Braces /></Button>
      <Button size="sm" variant="ghost" aria-label={t("Delete")} title={t("Delete")} onClick={(e) => { e.stopPropagation(); remove(snippet); }}><Trash2 /></Button></> : undefined} />;

  return <Workbench storageKey="sandbox" mainLabel={t("Editor")} crumbs={[{ label: t("Sandbox") }, { label: all.find((s) => s.id === current)?.title ?? "" }]}
    saving={dirty ? "dirty" : "idle"}
    onKeyDown={(event) => { if (!(event.metaKey || event.ctrlKey)) return; if (event.key === "Enter") { event.preventDefault(); run(); } if (event.key.toLowerCase() === "s") { event.preventDefault(); save(); } }}
    actions={<>
      <Button size="sm" variant="primary" onClick={run} title="⌘↩"><Play />{t("Run")}</Button>
      <Button size="sm" variant="ghost" onClick={save} title="⌘S"><Save />{t("Save snippet")}</Button>
      <Button size="sm" variant="ghost" onClick={() => void copy(code, t("Code copied."))}><Copy />{t("Copy")}</Button>
      <Button size="sm" variant="ghost" onClick={() => void copy(asComponent(code), t("Component skeleton copied."))} title={t("Copies a React component skeleton wrapping this markup.")}><Braces />{t("Copy as component")}</Button>
      <Button size="sm" variant="ghost" disabled={!dirty} onClick={() => { const s = all.find((x) => x.id === current); if (s) choose(s); }}><RotateCcw />{t("Reset")}</Button>
    </>}
    left={{ label: t("Snippets"), content: <div className="grid content-start gap-1 p-2">
      <PanelSection title={t("My snippets")} actions={<Button size="sm" variant="ghost" aria-label={t("New snippet")} title={t("New snippet")} onClick={() => { setCurrent(""); setCode("<!doctype html>\n<html>\n<body>\n\n</body>\n</html>\n"); setRunning(""); }}><Plus /></Button>}>
        {own.length ? own.map(snippetRow) : <p className="px-2 py-1 text-xs text-muted">{t("Save the editor's code to keep it here. Snippets stay in this browser.")}</p>}
      </PanelSection>
      <PanelSection title={t("Presets")}>{library.map(snippetRow)}</PanelSection>
    </div> }}
    right={big ? undefined : { label: t("Preview"), min: 280, max: 900, content: <div className="flex h-full min-h-0 flex-col">{toolbar}{preview}</div> }}
    dock={{ label: t("Sandbox dock"), tabs: [{ id: "console", title: t("Console"), badge: lines.filter((l) => l.level === "error").length || undefined, content: <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between border-b border-border px-2 py-0.5 text-xs text-muted"><span>{t("Messages from the preview frame")}</span><Button size="sm" variant="ghost" onClick={() => setLines([])}><Eraser />{t("Clear")}</Button></div>
      <ol className="min-h-0 flex-1 overflow-auto font-mono text-xs">{lines.map((l, i) => <li key={i} className={`flex gap-2 border-b border-border/50 px-2 py-0.5 ${l.level === "error" ? "text-danger" : l.level === "warn" ? "text-warning" : ""}`}><span className="text-muted">{l.at}</span><span className="w-10 uppercase text-muted">{l.level}</span><span className="whitespace-pre-wrap break-all">{l.text}</span></li>)}
        {!lines.length && <li className="px-2 py-1 text-muted">{t("Nothing logged yet. console.log in the snippet shows up here.")}</li>}</ol>
    </div> }] }}>
    {big ? <div className="flex min-h-0 flex-1 flex-col">{toolbar}{preview}</div>
      : <Textarea aria-label={t("Code")} value={code} spellCheck={false} onChange={(e) => setCode(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Tab") { e.preventDefault(); const el = e.currentTarget, start = el.selectionStart, end = el.selectionEnd; const next = `${code.slice(0, start)}  ${code.slice(end)}`; setCode(next); requestAnimationFrame(() => el.setSelectionRange(start + 2, start + 2)); } }}
        className="min-h-0 flex-1 resize-none rounded-none border-0 font-mono text-xs leading-5 focus-visible:ring-0" />}
  </Workbench>;
}
