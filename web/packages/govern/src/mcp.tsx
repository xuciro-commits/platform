import { useEffect, useState } from "react";
import { useHost } from "@platform/app";
import { Button, Card, Form, Input, PageHeader, Panel, Select, Textarea, Tag, t } from "@platform/ui";
import { Plug } from "lucide-react";

export function MCPClient() {
  const { client } = useHost();
  const { server, token, tenant } = client.connection;
  const [tools, setTools] = useState<{ name: string; description: string; inputSchema: any }[]>([]);
  const [selected, setSelected] = useState<string>();
  const [args, setArgs] = useState("{}");
  const [result, setResult] = useState<any>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [initialized, setInitialized] = useState(false);

  const mcp = async (method: string, params: any) => {
    const res = await fetch(`${server}/mcp`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${token}`,
        "Platform-Tenant": tenant,
        "MCP-Protocol-Version": "2025-06-18",
      },
      body: JSON.stringify({ jsonrpc: "2.0", id: crypto.randomUUID(), method, params }),
    });
    const json = await res.json();
    if (json.error) throw new Error(json.error.message);
    return json.result;
  };

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await mcp("initialize", { protocolVersion: "2025-06-18" });
        const listed = await mcp("tools/list", {});
        if (!cancelled) {
          setTools(listed.tools ?? []);
          setInitialized(true);
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => { cancelled = true; };
  }, [server, token, tenant]);

  const call = async () => {
    try {
      setBusy(true);
      setError("");
      setResult(undefined);
      const parsed = args.trim() ? JSON.parse(args) : {};
      const out = await mcp("tools/call", { name: selected!, arguments: parsed });
      setResult(out);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="grid gap-3">
      <PageHeader title={t("MCP client")} icon={<Plug />} help={t("Model Context Protocol client for the host catalog. Lists tools the caller may read or act through, and lets you invoke one to inspect its result. Writes create proposals that a person confirms in the workspace.")} />
      {!initialized && !error ? (
        <p className="text-xs text-muted">{t("Connecting to MCP host…")}</p>
      ) : (
        <>
          {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
          <Panel title={t("Tools")} className="grid content-start gap-2">
            <p className="text-xs text-muted">{t("{n} tools available", { n: tools.length })}</p>
            <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
              {tools.map((tool) => (
                <Button
                  key={tool.name}
                  variant={selected === tool.name ? "primary" : "ghost"}
                  className="justify-start text-left"
                  onClick={() => setSelected(tool.name)}
                >
                  <span className="font-mono text-xs text-muted block">{tool.name}</span>
                  {tool.description}
                </Button>
              ))}
              {tools.length === 0 && <p className="text-xs text-muted">{t("No tools visible to you.")}</p>}
            </div>
          </Panel>
          {selected && (
            <Form className="grid gap-2" onSubmit={call}>
              <div className="grid gap-1">
                <label className="text-xs font-semibold">{t("Tool")}</label>
                <Input value={selected} readOnly />
              </div>
              <div className="grid gap-1">
                <label className="text-xs font-semibold">{t("Arguments (JSON)")}</label>
                <Textarea rows={6} value={args} onChange={(e) => setArgs(e.target.value)} />
                <p className="text-xs text-muted">{t("Pass an object matching the tool's input schema.")}</p>
              </div>
              <Button type="submit" variant="primary" disabled={busy}>{t("Call tool")}</Button>
              {busy && <span className="text-xs text-muted">{t("Calling…")}</span>}
            </Form>
          )}
          {result && (
            <Panel title={t("Result")} className="grid min-w-0 content-start gap-2">
              <pre className="overflow-auto whitespace-pre-wrap font-mono text-xs text-muted p-2 rounded border border-border">{JSON.stringify(result, null, 2)}</pre>
            </Panel>
          )}
        </>
      )}
    </div>
  );
}
