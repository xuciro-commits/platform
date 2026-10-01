import { useState } from "react";
import { Button, Card, PageHeader, Panel, Tag, t } from "@platform/ui";
import { LayoutGrid, Workflow, FileUp, Copy, Check, Sparkles } from "lucide-react";

export function PageBuilderView({ tab = "blocks" }: { tab?: string }) {
  const [currentTab, setCurrentTab] = useState(tab);
  const [copied, setCopied] = useState(false);

  const handleCopy = (snippet: string) => {
    navigator.clipboard.writeText(snippet);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="space-y-4">
      <PageHeader
        title={t("Page builder")}
        description={t("Reusable layout blocks for rapid page assembly.")}
        actions={
          <div className="flex gap-2">
            <Button
              variant={currentTab === "blocks" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("blocks")}
            >
              <LayoutGrid className="size-3.5" />
              {t("Block library")}
            </Button>
            <Button
              variant={currentTab === "composer" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("composer")}
            >
              <Workflow className="size-3.5" />
              {t("Visual composer")}
            </Button>
            <Button
              variant={currentTab === "export" ? "primary" : "default"}
              size="sm"
              onClick={() => setCurrentTab("export")}
            >
              <FileUp className="size-3.5" />
              {t("Export page")}
            </Button>
          </div>
        }
      />

      {currentTab === "blocks" && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Card className="p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-sm">指标看板区块 (Metric Banner Block)</span>
              <Tag label="Composite" tone="neutral" />
            </div>
            <p className="text-xs text-muted">适用于制造车间或业务大盘顶部的 4 列统计卡片与动态状态趋势。</p>
            <div className="rounded border border-border bg-surface p-3 text-xs font-mono text-muted">
              &lt;MetricGrid columns=&#123;4&#125; items=&#123;metrics&#125; trend="up" /&gt;
            </div>
            <Button size="sm" onClick={() => handleCopy('<MetricGrid columns={4} items={metrics} trend="up" />')}>
              {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
              {t("Copy developer example")}
            </Button>
          </Card>

          <Card className="p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-sm">主从详情区块 (Master-Detail Block)</span>
              <Tag label="Pattern" tone="neutral" />
            </div>
            <p className="text-xs text-muted">左侧响应式工单/设备列表，右侧Tab详情与执行动作栏的标准布局。</p>
            <div className="rounded border border-border bg-surface p-3 text-xs font-mono text-muted">
              &lt;MasterDetail list=&#123;items&#125; active=&#123;selectedId&#125; renderDetail=&#123;renderDetail&#125; /&gt;
            </div>
            <Button size="sm" onClick={() => handleCopy('<MasterDetail list={items} active={selectedId} renderDetail={renderDetail} />')}>
              {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
              {t("Copy developer example")}
            </Button>
          </Card>

          <Card className="p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-sm">记录批处理区块 (Batch Record Block)</span>
              <Tag label="Scenario" tone="neutral" />
            </div>
            <p className="text-xs text-muted">支持批量多选、工单流转、审批操作弹框与变更历史审计。</p>
            <div className="rounded border border-border bg-surface p-3 text-xs font-mono text-muted">
              &lt;BatchActionToolbar actions=&#123;['approve', 'reject', 'export']&#125; selected=&#123;selected&#125; /&gt;
            </div>
            <Button size="sm" onClick={() => handleCopy('<BatchActionToolbar actions={["approve", "reject", "export"]} selected={selected} />')}>
              {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
              {t("Copy developer example")}
            </Button>
          </Card>

          <Card className="p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-sm">时间线审计区块 (Audit Timeline Block)</span>
              <Tag label="Semantic UI" tone="neutral" />
            </div>
            <p className="text-xs text-muted">带状态指示符与操作人签名的事件时间流，严格遵循平台不可篡改审计规范。</p>
            <div className="rounded border border-border bg-surface p-3 text-xs font-mono text-muted">
              &lt;Timeline events=&#123;auditLogs&#125; showOperator=&#123;true&#125; /&gt;
            </div>
            <Button size="sm" onClick={() => handleCopy('<Timeline events={auditLogs} showOperator={true} />')}>
              {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
              {t("Copy developer example")}
            </Button>
          </Card>
        </div>
      )}

      {currentTab === "composer" && (
        <Panel title={t("Visual Composer")} description={t("Arrange blocks and configure props.")}>
          <div className="flex min-h-[360px] items-center justify-center rounded-lg border-2 border-dashed border-border bg-surface p-8 text-center">
            <div className="max-w-md space-y-2">
              <Workflow className="mx-auto size-8 text-muted" />
              <h3 className="text-sm font-medium">{t("Visual Composer")}</h3>
              <p className="text-xs text-muted">从常用区块库选择组件，拖拽到画布中进行可视化排版与数据绑定配置。</p>
              <Button size="sm" variant="primary" className="mt-3">
                <Sparkles className="size-3.5" />
                初始化空白画布
              </Button>
            </div>
          </div>
        </Panel>
      )}

      {currentTab === "export" && (
        <Panel title={t("Export Page")} description={t("Export clean TSX code or JSON schema.")}>
          <div className="space-y-3">
            <pre className="overflow-auto rounded-md bg-slate-950 p-4 font-mono text-xs text-slate-100">
              <code>{`import { defineApp } from "@platform/app";
import { PageHeader, Card, Button } from "@platform/ui";

export default defineApp({
  id: "custom-page",
  title: "自定义业务页面",
  views: [{
    id: "main",
    title: () => "工作台",
    render: () => (
      <div className="space-y-4 p-4">
        <PageHeader title="业务工况" description="实时产线与工单处理" />
        <Card className="p-4">
          <p className="text-sm">页面主体内容已就绪。</p>
        </Card>
      </div>
    ),
  }],
});`}</code>
            </pre>
            <Button size="sm" onClick={() => handleCopy('// TSX Page Template')}>
              <Copy className="size-3.5" />
              {t("Copy developer example")}
            </Button>
          </div>
        </Panel>
      )}
    </div>
  );
}
