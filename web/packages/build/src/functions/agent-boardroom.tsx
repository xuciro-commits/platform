import { useState } from "react";
import { useRead } from "@platform/app";
import {
  Button,
  Card,
  Checkbox,
  Form,
  PageHeader,
  Panel,
  Textarea,
  Tag,
  t,
} from "@platform/ui";
import { useHost, newId, RunView, useOpenRecord, type AgentInfo } from "@platform/app";
import { Bot } from "lucide-react";

export function AgentBoardroom() {
  const { decide, can, me } = useHost();
  const openRecord = useOpenRecord();
  const agents = (useRead<AgentInfo[]>("/v1/agents") ?? []).filter((a) =>
    me.apps.some((x) => x.id === a.app)
  );
  const [selected, setSelected] = useState<string[]>([]);
  const [goal, setGoal] = useState("");
  const [busy, setBusy] = useState(false);
  const [runs, setRuns] = useState<{ agent: string; id: string }[]>([]);
  const toggle = (id: string) =>
    setSelected((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));
  const launch = async () => {
    if (!selected.length || !goal.trim() || busy || !can("agent.run.start")) return;
    setBusy(true);
    const next = [...runs];
    for (const agent of selected) {
      const id = newId("RUN");
      if (
        await decide("agent.run.start", { type: "agent.run", id }, { agent, goal })
      ) {
        next.push({ agent, id });
      }
    }
    setRuns(next);
    setBusy(false);
  };
  const openRun = (id: string) =>
    void openRecord({ type: "agent.run", id }, { window: "beside" });
  return (
    <div className="grid gap-3">
      <PageHeader
        title={t("Agent boardroom")}
        icon={<Bot />}
        help={t("Run several published agents on the same goal and compare their traces in one place. Every agent starts its own run; results stay in each run.")}
      />
      <Panel title={t("Agents")} className="grid content-start gap-2">
        <p className="text-xs text-muted">{t("Pick the agents to run side by side.")}</p>
        <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
          {agents.map((a) => (
            <label key={a.id} className="flex items-center gap-2 px-2 py-1 border rounded hover:bg-muted cursor-pointer select-none">
              <Checkbox
                checked={selected.includes(a.id)}
                onChange={() => toggle(a.id)}
                aria-label={a.title}
              />
              <div className="flex-1 min-w-0">
                <div className="text-sm font-medium truncate">{a.title}</div>
                <div className="text-xs text-muted truncate">{a.app}</div>
                <Tag label={t("tools") + ": " + a.tools.length} tone="neutral" />
              </div>
            </label>
          ))}
        </div>
      </Panel>
      <Form className="grid gap-2" onSubmit={launch}>
        <div className="grid gap-1">
          <label className="text-xs font-semibold">{t("Shared goal")}</label>
          <Textarea
            rows={3}
            placeholder={t("What should the agents do together?")}
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="submit"
            variant="primary"
            disabled={
              !selected.length ||
              !goal.trim() ||
              busy ||
              !can("agent.run.start")
            }
          >
            {t("Start boardroom")}
          </Button>
          {busy && <span className="text-xs text-muted">{t("Starting…")}</span>}
        </div>
      </Form>
      {runs.length > 0 && (
        <Panel title={t("Running agents")} className="grid content-start gap-3">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {runs.map((r) => {
              const agent = agents.find((a) => a.id === r.agent);
              return (
                <Card key={r.id} className="flex flex-col gap-2 border rounded p-3 min-h-[320px]">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium truncate">
                        {agent?.title ?? r.agent}
                      </div>
                      <div className="text-xs text-muted truncate">{r.id}</div>
                    </div>
                    <Button size="sm" variant="ghost" onClick={() => openRun(r.id)}>
                      {t("Open run")}
                    </Button>
                  </div>
                  <RunView id={r.id} compact />
                </Card>
              );
            })}
          </div>
        </Panel>
      )}
    </div>
  );
}
