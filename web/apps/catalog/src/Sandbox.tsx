import { useEffect, useState } from "react";
import { Button, Card, Input, PageHeader, Panel, Tag, Textarea, t } from "@platform/ui";
import {
  Copy,
  Check,
  Play,
  RotateCcw,
  Sparkles,
  Code2,
  Monitor,
  Tablet,
  Smartphone,
  FileCode,
  Maximize2,
  Minimize2,
  Plus,
  Trash2,
} from "lucide-react";
import INDUSTRIAL_INSTRUMENT_SAMPLE from "./presets/industrial-hmi.html?raw";

const b = "button";
const INDUSTRIAL_HMI_SAMPLE = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <title>工业控制台</title>
  <style>
    body { margin: 0; background: #0b0f19; color: #e2e8f0; font-family: ui-monospace, monospace; padding: 20px; }
    .card { background: #131c2e; border: 1px solid #1e293b; border-radius: 8px; padding: 16px; margin-bottom: 16px; }
    .title { font-size: 14px; font-weight: 600; color: #38bdf8; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.05em; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
    .metric { background: #0f172a; border: 1px solid #334155; border-radius: 6px; padding: 12px; }
    .metric-name { font-size: 11px; color: #94a3b8; }
    .metric-val { font-size: 24px; font-weight: 700; color: #22c55e; margin-top: 4px; }
    .btn { background: linear-gradient(180deg, #1e293b 0%, #0f172a 100%); border: 1px solid #475569; color: #f8fafc; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; font-weight: 500; }
    .btn:hover { border-color: #38bdf8; color: #38bdf8; }
    .btn-danger { background: #dc2626; border-color: #ef4444; color: white; }
  </style>
</head>
<body>
  <div class="card">
    <div class="title">真空退火炉实时工况 (Annealing Furnace #3)</div>
    <div class="grid">
      <div class="metric"><div class="metric-name">炉温主控温度</div><div class="metric-val">842.6 ℃</div></div>
      <div class="metric"><div class="metric-name">腔体气压</div><div class="metric-val">1.2e-4 Pa</div></div>
      <div class="metric"><div class="metric-name">循环水流量</div><div class="metric-val">42.8 L/min</div></div>
      <div class="metric"><div class="metric-name">主轴电机负载</div><div class="metric-val">68.4 %</div></div>
    </div>
  </div>
  <div style="display: flex; gap: 10px;">
    <${b} class="btn" onclick="alert('执行加热程序')">启动升温</${b}>
    <${b} class="btn" onclick="alert('氮气吹扫中')">氮气保护吹扫</${b}>
    <${b} class="btn btn-danger" onclick="alert('触发急停联锁')">急停控制</${b}>
  </div>
</body>
</html>`;

const METAL_BUTTON_SAMPLE = `<!DOCTYPE html>
<html>
<head>
  <style>
    body { background: #0f172a; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
    .metal-btn {
      position: relative;
      display: inline-flex;
      align-items: center;
      gap: 10px;
      padding: 12px 24px;
      font-size: 14px;
      font-weight: 600;
      color: #0f172a;
      border: 1px solid rgba(255,255,255,0.4);
      border-radius: 8px;
      cursor: pointer;
      background: linear-gradient(135deg, #e2e8f0 0%, #cbd5e1 50%, #94a3b8 100%);
      box-shadow: 0 4px 6px -1px rgba(0,0,0,0.3), inset 0 1px 0 rgba(255,255,255,0.8), inset 0 -1px 0 rgba(0,0,0,0.2);
      transition: all 0.15s ease;
    }
    .metal-btn:hover {
      box-shadow: 0 6px 12px -2px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.9);
      transform: translateY(-1px);
    }
  </style>
</head>
<body>
  <${b} class="metal-btn">
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg>
    确认关键工艺放行
  </${b}>
</body>
</html>`;

const RETRO_BUTTON_SAMPLE = `<!DOCTYPE html>
<html>
<head>
  <style>
    body { background: #18181b; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
    .retro-btn {
      font-family: monospace;
      font-size: 14px;
      font-weight: 700;
      padding: 10px 20px;
      color: #18181b;
      background: #fbbf24;
      border: 2px solid #000;
      box-shadow: 4px 4px 0px #000;
      cursor: pointer;
      text-transform: uppercase;
      transition: all 0.1s ease;
    }
    .retro-btn:active {
      transform: translate(2px, 2px);
      box-shadow: 2px 2px 0px #000;
    }
  </style>
</head>
<body>
  <${b} class="retro-btn">INITIALIZE SYSTEM // 01</${b}>
</body>
</html>`;

type InspirationItem = {
  id: string;
  title: string;
  tag: string;
  tone?: "neutral" | "info" | "success" | "warning" | "danger";
  desc: string;
  code: string;
  isCustom?: boolean;
};

const DEFAULT_INSPIRATIONS: InspirationItem[] = [
  {
    id: "industrial-prototype",
    title: "工业仪表 UI · 单页原型",
    tag: "Braun / Dieter Rams",
    tone: "success",
    desc: "博朗/拉姆斯风格精工工业仪表 UI，包含实体旋钮、冲压质感按键、液晶段码屏与工况示波波形。",
    code: INDUSTRIAL_INSTRUMENT_SAMPLE,
  },
  {
    id: "industrial-furnace",
    title: "真空退火炉高精度看板 (HMI Console)",
    tag: "Industrial HMI",
    tone: "info",
    desc: "深色工业装备看板原型，包含实时炉温、高真空读数、循环水监控与急停控制。",
    code: INDUSTRIAL_HMI_SAMPLE,
  },
  {
    id: "metal-button",
    title: "拉丝金属精密按钮 (Metal Button)",
    tag: "CSS Spec",
    tone: "neutral",
    desc: "具备真实微米级金属反光拉丝质感，专用于高端制造与关键放行确认。",
    code: METAL_BUTTON_SAMPLE,
  },
  {
    id: "retro-button",
    title: "复古立体按钮 (Retro Button)",
    tag: "Retro 80s",
    tone: "warning",
    desc: "80年代赛博复古厚边框高饱和度按钮，按压具备物理位移反馈。",
    code: RETRO_BUTTON_SAMPLE,
  },
];

const STORAGE_KEY = "catalog.custom_inspirations";

export function SandboxView({ tab = "playground" }: { tab?: string }) {
  const [currentTab, setCurrentTab] = useState(tab);
  const [code, setCode] = useState(INDUSTRIAL_INSTRUMENT_SAMPLE);
  const [copied, setCopied] = useState(false);
  const [previewWidth, setPreviewWidth] = useState<"100%" | "768px" | "375px">("100%");
  const [isFullscreen, setIsFullscreen] = useState(false);

  // Custom inspirations state with localStorage persistence
  const [customInspirations, setCustomInspirations] = useState<InspirationItem[]>(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });

  // Add Inspiration Modal/Form state
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [newTag, setNewTag] = useState("HTML/CSS");
  const [newDesc, setNewDesc] = useState("");
  const [newCode, setNewCode] = useState("");

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (isFullscreen) setIsFullscreen(false);
        if (isAddOpen) setIsAddOpen(false);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isFullscreen, isAddOpen]);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const loadPreset = (presetCode: string) => {
    setCode(presetCode);
    setCurrentTab("playground");
  };

  const handleAddInspiration = () => {
    if (!newTitle.trim() || !newCode.trim()) return;
    const newItem: InspirationItem = {
      id: `custom-${Date.now()}`,
      title: newTitle.trim(),
      tag: newTag.trim() || "HTML/CSS",
      tone: "neutral",
      desc: newDesc.trim() || t("Paste custom HTML/CSS markup to save as inspiration"),
      code: newCode.trim(),
      isCustom: true,
    };
    const next = [newItem, ...customInspirations];
    setCustomInspirations(next);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      /* storage unavailable */
    }
    setNewTitle("");
    setNewDesc("");
    setNewCode("");
    setIsAddOpen(false);
  };

  const handleDeleteInspiration = (id: string) => {
    const next = customInspirations.filter((item) => item.id !== id);
    setCustomInspirations(next);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      /* storage unavailable */
    }
  };

  const allInspirations = [...customInspirations, ...DEFAULT_INSPIRATIONS];

  if (isFullscreen) return (
          <div className="flex h-full min-h-[32rem] flex-col bg-background p-4">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-sm text-foreground">{t("Live preview")}</span>
                <span className="rounded bg-surface px-1.5 py-0.5 text-xs text-muted">{t("Expand preview")}</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1 rounded bg-surface p-0.5 border border-border">
                  <Button
                    size="sm"
                    variant={previewWidth === "100%" ? "primary" : "ghost"}
                    onClick={() => setPreviewWidth("100%")}
                    aria-label="Desktop view"
                  >
                    <Monitor className="size-3.5" />
                    100%
                  </Button>
                  <Button
                    size="sm"
                    variant={previewWidth === "768px" ? "primary" : "ghost"}
                    onClick={() => setPreviewWidth("768px")}
                    aria-label="Tablet view"
                  >
                    <Tablet className="size-3.5" />
                    768px
                  </Button>
                  <Button
                    size="sm"
                    variant={previewWidth === "375px" ? "primary" : "ghost"}
                    onClick={() => setPreviewWidth("375px")}
                    aria-label="Mobile view"
                  >
                    <Smartphone className="size-3.5" />
                    375px
                  </Button>
                </div>
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => setIsFullscreen(false)}
                  aria-label={t("Exit fullscreen")}
                >
                  <Minimize2 className="size-3.5" />
                  {t("Exit fullscreen")}
                </Button>
              </div>
            </div>
            <div className="flex flex-1 items-center justify-center overflow-auto p-4 bg-surface/30">
              <iframe
                title="sandbox-preview-fullscreen"
                srcDoc={code}
                sandbox="allow-scripts"
                style={{ width: previewWidth, height: "100%", minHeight: "560px" }}
                className="rounded border border-border bg-white shadow-2xl transition-all duration-200"
              />
            </div>
          </div>
  );

  return (
    <div className="space-y-4">
      <PageHeader
        title={t("Sandbox")}
        description={t("In-a-box code playground")}
        actions={
          <div className="flex items-center gap-2">
            <Button
              variant={currentTab === "playground" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("playground")}
            >
              <Code2 className="size-3.5" />
              {t("Live playground")}
            </Button>
            <Button
              variant={currentTab === "inspirations" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("inspirations")}
            >
              <Sparkles className="size-3.5" />
              {t("Inspirations")}
            </Button>
            <Button
              variant={currentTab === "convert" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("convert")}
            >
              <FileCode className="size-3.5" />
              {t("Convert component")}
            </Button>
            {currentTab === "inspirations" && (
              <Button size="sm" variant="primary" onClick={() => setIsAddOpen(true)}>
                <Plus className="size-3.5" />
                {t("Add inspiration")}
              </Button>
            )}
          </div>
        }
      />

      {currentTab === "playground" && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {/* Left: Code Box */}
          <Panel
            title={t("Code editor")}
            description={t("Paste HTML, CSS or markup to preview in real-time.")}
            actions={
              <div className="flex items-center gap-1.5">
                <Button size="sm" onClick={handleCopy}>
                  {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
                  {copied ? t("Copied") : t("Copy developer example")}
                </Button>
                <Button size="sm" onClick={() => setCode(INDUSTRIAL_INSTRUMENT_SAMPLE)}>
                  <RotateCcw className="size-3.5" />
                  {t("Reset")}
                </Button>
              </div>
            }
          >
            <div className="relative">
              <Textarea
                value={code}
                onChange={(e) => setCode(e.target.value)}
                rows={22}
                spellCheck={false}
                className="w-full rounded-md border border-border bg-slate-950 p-3 font-mono text-xs text-slate-100 outline-none focus:border-primary"
              />
            </div>
          </Panel>

          {/* Right: Live Preview Box */}
          <Panel
            title={t("Live preview")}
            description={t("Synthetic fixtures. This preview does not call a tenant host.")}
            actions={
              <div className="flex items-center gap-1">
                <Button
                  size="sm"
                  variant={previewWidth === "100%" ? "primary" : "ghost"}
                  onClick={() => setPreviewWidth("100%")}
                  aria-label="Desktop view"
                >
                  <Monitor className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant={previewWidth === "768px" ? "primary" : "ghost"}
                  onClick={() => setPreviewWidth("768px")}
                  aria-label="Tablet view"
                >
                  <Tablet className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant={previewWidth === "375px" ? "primary" : "ghost"}
                  onClick={() => setPreviewWidth("375px")}
                  aria-label="Mobile view"
                >
                  <Smartphone className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setIsFullscreen(true)}
                  aria-label={t("Expand preview")}
                >
                  <Maximize2 className="size-3.5" />
                  {t("Expand preview")}
                </Button>
              </div>
            }
          >
            <div className="flex justify-center rounded-md border border-border bg-slate-900 p-2">
              <iframe
                title="sandbox-preview"
                srcDoc={code}
                sandbox="allow-scripts"
                style={{ width: previewWidth, height: "480px" }}
                className="rounded border border-slate-700 bg-white shadow-md transition-all duration-200"
              />
            </div>
          </Panel>
        </div>
      )}

      {currentTab === "inspirations" && (
        <div className="space-y-4">
          {/* Add Inspiration Form */}
          {isAddOpen && (
            <Card className="p-4 space-y-3 border-primary/50 shadow-md">
              <div className="flex items-center justify-between border-b border-border pb-2">
                <div className="flex items-center gap-2">
                  <Plus className="size-4 text-primary" />
                  <span className="font-semibold text-sm">{t("Add inspiration")}</span>
                </div>
                <Button size="sm" variant="ghost" onClick={() => setIsAddOpen(false)}>
                  {t("Cancel")}
                </Button>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div>
                  <span className="text-xs text-muted block mb-1">{t("Inspiration title")}</span>
                  <Input
                    placeholder="例如：精密高低温试验箱看板"
                    value={newTitle}
                    onChange={(e) => setNewTitle(e.target.value)}
                  />
                </div>
                <div>
                  <span className="text-xs text-muted block mb-1">{t("Category / Tag")}</span>
                  <Input
                    placeholder="例如：HTML/CSS 或 拟物工控"
                    value={newTag}
                    onChange={(e) => setNewTag(e.target.value)}
                  />
                </div>
              </div>
              <div>
                <span className="text-xs text-muted block mb-1">{t("Description")}</span>
                <Input
                  placeholder="简要说明设计特性与交互重点"
                  value={newDesc}
                  onChange={(e) => setNewDesc(e.target.value)}
                />
              </div>
              <div>
                <span className="text-xs text-muted block mb-1">{t("HTML / CSS markup")}</span>
                <Textarea
                  rows={6}
                  placeholder="在此粘贴完整 HTML / CSS 代码..."
                  value={newCode}
                  onChange={(e) => setNewCode(e.target.value)}
                  className="font-mono text-xs"
                />
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button size="sm" variant="ghost" onClick={() => setIsAddOpen(false)}>
                  {t("Cancel")}
                </Button>
                <Button
                  size="sm"
                  variant="primary"
                  disabled={!newTitle.trim() || !newCode.trim()}
                  onClick={handleAddInspiration}
                >
                  <Check className="size-3.5" />
                  {t("Save inspiration")}
                </Button>
              </div>
            </Card>
          )}

          {/* Inspirations Grid */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {/* Add Card */}
            <Card
              className="flex min-h-[220px] cursor-pointer flex-col items-center justify-center border-dashed border-2 border-border p-6 text-center hover:border-primary hover:bg-row-hover transition-colors"
              onClick={() => setIsAddOpen(true)}
            >
              <div className="rounded-full bg-surface p-3 shadow-sm border border-border">
                <Plus className="size-5 text-muted" />
              </div>
              <h4 className="mt-3 text-sm font-medium">{t("Add inspiration")}</h4>
              <p className="mt-1 text-xs text-muted">{t("Paste custom HTML/CSS markup to save as inspiration")}</p>
            </Card>

            {/* Inspiration Cards */}
            {allInspirations.map((item) => (
              <Card key={item.id} className="flex flex-col justify-between p-4">
                <div>
                  <div className="flex items-center justify-between">
                    <Tag label={item.tag} tone={item.tone ?? "neutral"} />
                    {item.isCustom ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-red-500 hover:text-red-600"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDeleteInspiration(item.id);
                        }}
                        aria-label={t("Delete")}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    ) : (
                      <span className="font-mono text-xs text-muted">HTML/CSS</span>
                    )}
                  </div>
                  <h3 className="mt-2 text-sm font-semibold">{item.title}</h3>
                  <p className="mt-1 text-xs text-muted line-clamp-3">{item.desc}</p>
                </div>
                <div className="mt-4 pt-2 border-t border-border flex justify-end">
                  <Button size="sm" variant="primary" onClick={() => loadPreset(item.code)}>
                    <Play className="size-3.5" />
                    {t("Load template")}
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        </div>
      )}

      {currentTab === "convert" && (
        <Card className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">{t("Convert to Platform Component")}</h3>
            <Tag label="Beta" tone="neutral" />
          </div>
          <p className="text-xs text-muted">{t("Generate component definition from HTML prototype.")}</p>
          <Card className="p-3 text-xs space-y-2 font-mono">
            <div className="text-muted">// 平台组件输出规范 (Target: @platform/ui)</div>
            <div className="text-green-600 dark:text-green-400">export interface CustomWidgetProps extends React.ButtonHTMLAttributes&lt;HTMLButtonElement&gt; &#123;</div>
            <div className="pl-4">variant?: "default" | "metal" | "retro" | "liquid";</div>
            <div className="pl-4">size?: "sm" | "md" | "lg";</div>
            <div className="text-green-600 dark:text-green-400">&#125;</div>
          </Card>
          <Button size="sm" variant="primary">
            <Sparkles className="size-3.5" />
            {t("Convert component")}
          </Button>
        </Card>
      )}
    </div>
  );
}
