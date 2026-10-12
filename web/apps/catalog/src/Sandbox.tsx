// The code sandbox (ADR-0054 D4): one local prototype editor. Snippets are
// browser data, never published components, business records or credentials.
import { HostContext } from "@platform/app";
import { Button, PanelSection, StructureRow, Textarea, Workbench, language, notify, t, useUnsavedChanges, useViewVisible } from "@platform/ui";
import { Copy, FileCode, Maximize2, Minimize2, Monitor, Play, Plus, RotateCcw, Save, Smartphone, Tablet, Trash2, Braces, Eraser } from "lucide-react";
import { useCallback, useContext, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { asComponent, consoleMessage, decodeSnippets, previewDocument, type ConsoleMessage, type Snippet } from "./sandbox-model";
import INDUSTRIAL_INSTRUMENT from "./presets/industrial-hmi.html?raw";
import HMI_CONSOLE from "./presets/hmi-console.html?raw";
import METAL_BUTTON from "./presets/metal-button.html?raw";
import RETRO_BUTTON from "./presets/retro-button.html?raw";

const presets: () => Snippet[] = () => [
  { id: "industrial-instrument", title: t("Industrial instrument panel"), code: INDUSTRIAL_INSTRUMENT },
  { id: "hmi-console", title: t("Furnace HMI console"), code: HMI_CONSOLE },
  { id: "metal-button", title: t("Brushed metal button"), code: METAL_BUTTON },
  { id: "retro-button", title: t("Retro raised button"), code: RETRO_BUTTON },
];
const OWN_KEY = "sandbox.snippets";
type Library = { items: Snippet[]; raw: string | null; error?: string };
const readOwn = (key: string): Library => {
  try { const raw = localStorage.getItem(key); return { items: decodeSnippets(raw), raw }; }
  catch { return { items: [], raw: null, error: t("Stored snippets are unavailable or invalid. Nothing was overwritten.") }; }
};
const widths = [["100%", Monitor, "Desktop"], ["768px", Tablet, "Tablet"], ["375px", Smartphone, "Phone"]] as const;

const bridge = `<script>(function(){var send=function(level,args){try{parent.postMessage({sandbox:true,level:level,text:Array.prototype.map.call(args,function(a){try{return typeof a==="string"?a:JSON.stringify(a)}catch(e){return String(a)}}).join(" ")},"*")}catch(e){}};
["log","info","warn","error"].forEach(function(l){var o=console[l];console[l]=function(){send(l,arguments);o&&o.apply(console,arguments)}});
window.addEventListener("error",function(e){send("error",[e.message+" ("+e.lineno+":"+e.colno+")"])});window.addEventListener("unhandledrejection",function(e){send("error",[String(e.reason)])})})()</script>`;
type Line = ConsoleMessage & { at: string };

/** Hidden workbench panes do not keep executing prototype scripts. Reopening
 * restarts the last run, while the editor and its unsaved code stay mounted. */
function PreviewFrame({ document, width, frame }: { document: string; width: string; frame: RefObject<HTMLIFrameElement | null> }) {
  const visible = useViewVisible();
  return visible ? <iframe ref={frame} title={t("Sandbox preview")} srcDoc={document} sandbox="allow-scripts"
    style={{ width, minHeight: "100%" }} className="h-full min-h-[24rem] rounded border border-border bg-white shadow-sm transition-[width] duration-200" /> : null;
}

export function SandboxView() {
  const me = useContext(HostContext)?.me;
  // Legacy browser-only prototypes remain in the offline library. They are
  // not silently assigned to whichever tenant/member signs in first.
  const key = me ? `${OWN_KEY}:${JSON.stringify([me.tenantId, me.principalId])}` : OWN_KEY;
  return <SnippetWorkbench key={key} storageKey={key} />;
}

function SnippetWorkbench({ storageKey }: { storageKey: string }) {
  const library = useMemo(presets, [language()]);
  const [saved, setSaved] = useState<Library>(() => readOwn(storageKey));
  const [current, setCurrent] = useState(library[0]!.id);
  const [code, setCode] = useState(library[0]!.code);
  const [running, setRunning] = useState(code), [runRevision, setRunRevision] = useState(0);
  const [width, setWidth] = useState<(typeof widths)[number][0]>("100%");
  const [big, setBig] = useState(false), [lines, setLines] = useState<Line[]>([]);
  const frame = useRef<HTMLIFrameElement>(null);
  const visible = useViewVisible();
  const own = saved.items, all = [...own, ...library], selected = all.find((snippet) => snippet.id === current);
  const dirty = code !== (selected?.code ?? "");
  const restore = () => { const original = selected?.code ?? ""; setCode(original); setRunning(original); setRunRevision((revision) => revision + 1); setLines([]); };
  const { confirmDiscard, markSaved } = useUnsavedChanges(dirty, restore);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (!visible || !frame.current?.contentWindow || event.source !== frame.current.contentWindow) return;
      const message = consoleMessage(event.data);
      if (message) setLines((old) => [...old.slice(-199), { ...message, at: new Date().toLocaleTimeString() }]);
    };
    addEventListener("message", onMessage);
    return () => removeEventListener("message", onMessage);
  }, [visible]);
  const run = useCallback(() => { setLines([]); setRunning(code); setRunRevision((revision) => revision + 1); }, [code]);
  const choose = (snippet: Snippet) => {
    setCurrent(snippet.id); setCode(snippet.code); setRunning(snippet.code); setLines([]); setRunRevision((revision) => revision + 1);
  };
  const store = (next: Snippet[]) => {
    if (saved.error) { notify.error(saved.error); return false; }
    try {
      if (localStorage.getItem(storageKey) !== saved.raw) {
        const error = t("The snippet library changed in another tab. Reload it before saving.");
        setSaved((previous) => ({ ...previous, error })); notify.error(error); return false;
      }
      const raw = JSON.stringify(next);
      localStorage.setItem(storageKey, raw);
      setSaved({ items: next, raw });
      return true;
    } catch { notify.error(t("Could not save snippets in this browser. Your editor changes are still unsaved.")); return false; }
  };
  const save = () => {
    const existing = own.find((snippet) => snippet.id === current);
    const next = existing ? own.map((snippet) => snippet.id === current ? { ...snippet, code } : snippet)
      : [{ id: `own-${crypto.randomUUID()}`, title: `${t("Snippet")} ${own.length + 1}`, code, own: true }, ...own];
    if (!store(next)) return;
    if (!existing) setCurrent(next[0]!.id);
    markSaved(); notify.success(t("Snippet saved in this browser."));
  };
  const reload = () => confirmDiscard(() => {
    const next = readOwn(storageKey);
    if (next.error) { setSaved((previous) => ({ ...previous, error: next.error })); return; }
    setSaved(next); choose(next.items.find((snippet) => snippet.id === current) ?? library[0]!);
  });
  const rename = (snippet: Snippet) => {
    const title = prompt(t("Snippet name"), snippet.title)?.trim();
    if (title) store(own.map((item) => item.id === snippet.id ? { ...item, title } : item));
  };
  const remove = (snippet: Snippet) => {
    const apply = () => { if (store(own.filter((item) => item.id !== snippet.id)) && current === snippet.id) choose(library[0]!); };
    if (current === snippet.id) confirmDiscard(apply); else apply();
  };
  const copy = async (text: string, done: string) => {
    try { await navigator.clipboard.writeText(text); notify.success(done); }
    catch { notify.error(t("Clipboard unavailable. Select and copy the code below.")); }
  };
  const preview = <div className="flex h-full min-h-0 flex-col">
    <p className="px-3 py-1 text-xs text-muted">{code !== running ? t("Preview uses the last run. Run again to apply editor changes.") : t("Preview pauses while hidden and restarts when reopened.")}</p>
    <div className="flex min-h-0 flex-1 items-start justify-center overflow-auto bg-surface p-3">
      <PreviewFrame key={runRevision} document={previewDocument(running, bridge)} width={width} frame={frame} />
    </div>
  </div>;
  const toolbar = <div className="flex items-center gap-1 border-b border-border px-2 py-1 text-xs">
    {widths.map(([w, Icon, label]) => <Button key={w} size="sm" variant={width === w ? "primary" : "ghost"} aria-label={t(label)} title={t(label)} onClick={() => setWidth(w)}><Icon />{w}</Button>)}
    <span className="ml-auto" /><Button size="sm" variant="ghost" aria-label={big ? t("Back to the editor") : t("Expand preview")} title={big ? t("Back to the editor") : t("Expand preview")} onClick={() => setBig((value) => !value)}>{big ? <Minimize2 /> : <Maximize2 />}</Button>
  </div>;
  const snippetRow = (snippet: Snippet) => <StructureRow key={snippet.id} icon={<FileCode />} label={snippet.title} selected={snippet.id === current}
    onClick={() => { if (snippet.id !== current) confirmDiscard(() => choose(snippet)); }}
    actions={snippet.own ? <><Button size="sm" variant="ghost" aria-label={t("Rename")} title={t("Rename")} onClick={(event) => { event.stopPropagation(); rename(snippet); }}><Braces /></Button>
      <Button size="sm" variant="ghost" aria-label={t("Delete")} title={t("Delete")} onClick={(event) => { event.stopPropagation(); remove(snippet); }}><Trash2 /></Button></> : undefined} />;

  return <Workbench storageKey="sandbox" mainLabel={t("Editor")} crumbs={[{ label: t("Sandbox") }, { label: selected?.title ?? "" }]} saving={dirty ? "dirty" : "idle"}
    onKeyDown={(event) => { if (!(event.metaKey || event.ctrlKey)) return; if (event.key === "Enter") { event.preventDefault(); run(); } if (event.key.toLowerCase() === "s") { event.preventDefault(); save(); } }}
    actions={<>
      <Button size="sm" variant="primary" onClick={run} title="⌘↩"><Play />{t("Run")}</Button>
      <Button size="sm" variant="ghost" onClick={save} title="⌘S"><Save />{t("Save snippet")}</Button>
      <Button size="sm" variant="ghost" onClick={() => void copy(code, t("Code copied."))}><Copy />{t("Copy")}</Button>
      <Button size="sm" variant="ghost" onClick={() => void copy(asComponent(code), t("Component skeleton copied."))} title={t("Copies a React component skeleton wrapping this markup.")}><Braces />{t("Copy as component")}</Button>
      <Button size="sm" variant="ghost" disabled={!dirty} onClick={() => confirmDiscard(restore)}><RotateCcw />{t("Reset")}</Button>
    </>}
    left={{ label: t("Snippets"), content: <div className="grid content-start gap-1 p-2">
      {saved.error && <div role="alert" className="grid gap-2 p-2 text-xs text-danger"><p>{saved.error}</p><Button size="sm" variant="ghost" onClick={reload}>{t("Reload snippets")}</Button></div>}
      <PanelSection title={t("My snippets")} actions={<Button size="sm" variant="ghost" aria-label={t("New snippet")} title={t("New snippet")} onClick={() => confirmDiscard(() => {
        setCurrent(""); setCode("<!doctype html>\n<html>\n<body>\n\n</body>\n</html>\n"); setRunning(""); setLines([]); setRunRevision((revision) => revision + 1);
      })}><Plus /></Button>}>
        {own.length ? own.map(snippetRow) : <p className="px-2 py-1 text-xs text-muted">{t("Save the editor's code to keep it here. Snippets stay in this browser.")}</p>}
      </PanelSection>
      <PanelSection title={t("Presets")}>{library.map(snippetRow)}</PanelSection>
    </div> }}
    right={big ? undefined : { label: t("Preview"), min: 280, max: 900, content: <div className="flex h-full min-h-0 flex-col">{toolbar}{preview}</div> }}
    dock={{ label: t("Sandbox dock"), tabs: [{ id: "console", title: t("Console"), badge: lines.filter((line) => line.level === "error").length || undefined, content: <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between border-b border-border px-2 py-0.5 text-xs text-muted"><span>{t("Messages from the preview frame")}</span><Button size="sm" variant="ghost" onClick={() => setLines([])}><Eraser />{t("Clear")}</Button></div>
      <ol className="min-h-0 flex-1 overflow-auto font-mono text-xs">{lines.map((line, index) => <li key={index} className={`flex gap-2 border-b border-border/50 px-2 py-0.5 ${line.level === "error" ? "text-danger" : line.level === "warn" ? "text-warning" : ""}`}>
        <span className="text-muted">{line.at}</span><span className="w-10 uppercase text-muted">{line.level}</span><span className="whitespace-pre-wrap break-all">{line.text}{line.truncated && <em>{t("Console message truncated.")}</em>}</span>
      </li>)}{!lines.length && <li className="px-2 py-1 text-muted">{t("Nothing logged yet. console.log in the snippet shows up here.")}</li>}</ol>
    </div> }] }}>
    {big ? <div className="flex min-h-0 flex-1 flex-col">{toolbar}{preview}</div> : <Textarea aria-label={t("Code")} value={code} spellCheck={false} onChange={(event) => setCode(event.target.value)}
      onKeyDown={(event) => { if (event.key === "Tab") { event.preventDefault(); const element = event.currentTarget, start = element.selectionStart, end = element.selectionEnd; setCode(`${code.slice(0, start)}  ${code.slice(end)}`); requestAnimationFrame(() => element.setSelectionRange(start + 2, start + 2)); } }}
      className="min-h-0 flex-1 resize-none rounded-none border-0 font-mono text-xs leading-5 focus-visible:ring-0" />}
  </Workbench>;
}
