import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { useEffect, useMemo, useRef, useState } from "react";
import { type Api, useRead, useReads } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { plain } from "./values";

type CallMapping = {
  number: string;
  contact?: string;
  agent: string;
  offer: string;
  answer: string;
  ended?: string;
  take: string;
  hangup: string;
  ringing: string;
  connected: string;
};

type CallType = { type: string; state: string; mapping: CallMapping };

type Call = {
  type: string;
  id: string;
  revision: number;
  state: "ringing" | "connected";
  number: string;
  contact: string;
  agent: string;
  offer: string;
  answer: string;
  ended: boolean;
  canTake: boolean;
  canHangUp: boolean;
  take?: Api.Action;
  hangup?: Api.Action;
};

type Media = { pc: RTCPeerConnection; mic: MediaStream; speaker: HTMLAudioElement; answered: boolean };
const sessions = new Map<string, Media>();

function callTypes(entities: Api.EntityInfo[]): CallType[] {
  return entities.flatMap((entity) => {
    const view = entity.view as { call?: Partial<CallMapping> } | undefined;
    const call = view?.call;
    const state = entity.lifecycle?.field;
    if (!call || !state || !call.number || !call.agent || !call.offer || !call.answer || !call.take || !call.hangup || !call.ringing || !call.connected) return [];
    return [{ type: entity.type, state, mapping: call as CallMapping }];
  });
}

async function openSession(key: string): Promise<string> {
  const mic = await navigator.mediaDevices.getUserMedia({ audio: true });
  const pc = new RTCPeerConnection();
  const speaker = new Audio();
  speaker.autoplay = true;
  pc.ontrack = (event) => { speaker.srcObject = event.streams[0] ?? new MediaStream([event.track]); };
  for (const track of mic.getTracks()) pc.addTrack(track, mic);
  const gathered = new Promise<void>((resolve) => {
    if (pc.iceGatheringState === "complete") resolve();
    pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === "complete") resolve(); };
    setTimeout(resolve, 3000);
  });
  await pc.setLocalDescription(await pc.createOffer());
  await gathered;
  sessions.set(key, { pc, mic, speaker, answered: false });
  return pc.localDescription?.sdp ?? "";
}

function closeSession(key: string) {
  const media = sessions.get(key);
  if (!media) return;
  sessions.delete(key);
  media.mic.getTracks().forEach((track) => track.stop());
  media.pc.close();
  media.speaker.srcObject = null;
}

function duration(since: number, now: number) {
  const seconds = Math.max(0, Math.floor((now - since) / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

export function CallDock() {
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const me = useRead<Api.MeView>("/v1/me");
  const types = useMemo(() => (entities.status === "ready" ? callTypes(entities.data) : []), [entities]);
  const reads = useReads<Api.RecordPage>(types.map(({ type, state, mapping }) =>
    `/v1/records/${encodeURIComponent(type)}?${new URLSearchParams({ limit: "20", actions: "1", domain: JSON.stringify([[state, "in", [mapping.ringing, mapping.connected]]]) })}`));
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState("");
  const since = useRef(new Map<string, number>()).current;
  const [now, setNow] = useState(() => Date.now());
  const self = me.status === "ready" ? me.data.principalId : "";

  const calls = useMemo<Call[]>(() => {
    if (reads.status !== "ready" || catalog.status !== "ready") return [];
    return types.flatMap(({ type, state, mapping }, index) => {
      const page = reads.data[index];
      if (!page?.records) return [];
      return (page.records as { id: string; revision: number; [field: string]: unknown }[]).flatMap((row): Call[] => {
        const value = (field?: string) => (field ? plain(row[field]) : undefined);
        const current = String(value(state) ?? "");
        const allowed = (name: string) => page.actions?.[row.id]?.some((action) => action.schema === `${type}.${name}` && action.allowed) ?? false;
        const status = current === mapping.ringing ? "ringing" : current === mapping.connected ? "connected" : undefined;
        if (!status) return [];
        return [{
          type, id: row.id, revision: row.revision, state: status,
          number: String(value(mapping.number) ?? ""),
          contact: mapping.contact ? String(page.refs?.[mapping.contact]?.[String(value(mapping.contact) ?? "")] ?? "") : "",
          agent: String(value(mapping.agent) ?? ""),
          offer: String(value(mapping.offer) ?? ""),
          answer: String(value(mapping.answer) ?? ""),
          ended: value(mapping.ended) === true,
          canTake: allowed(mapping.take),
          canHangUp: allowed(mapping.hangup),
          take: catalog.data.find((action) => action.schema === `${type}.${mapping.take}`),
          hangup: catalog.data.find((action) => action.schema === `${type}.${mapping.hangup}`),
        }];
      });
    });
  }, [reads, catalog, types]);

  const key = (call: Call) => `${call.type}/${call.id}`;
  const mine = calls.filter((call) => self && call.agent === self && !call.ended);
  const ringing = calls.filter((call) => call.state === "ringing" && !call.agent && !call.offer && call.canTake);

  const take = async (call: Call) => {
    if (!call.take) return;
    setBusy(key(call));
    setFailure("");
    try {
      const offer = await openSession(key(call));
      await send(call.take, call.id, { offer }, call.revision);
    } catch (problem) {
      closeSession(key(call));
      setFailure(reasonOf(problem));
    } finally {
      setBusy("");
    }
  };

  const hangUp = async (call: Call) => {
    if (!call.hangup) return;
    try {
      await send(call.hangup, call.id, {}, call.revision);
    } catch (problem) {
      setFailure(reasonOf(problem));
    }
  };

  useEffect(() => {
    for (const call of mine) {
      const media = sessions.get(key(call));
      if (call.state === "ringing" && !call.offer && !media && call.canTake && busy !== key(call)) void take(call);
      if (media && call.answer && !media.answered) {
        media.answered = true;
        media.pc.setRemoteDescription({ type: "answer", sdp: call.answer }).catch((problem) => setFailure(reasonOf(problem)));
      }
      if (call.state === "connected" && !since.has(key(call))) since.set(key(call), Date.now());
    }
    const open = new Set(mine.map(key));
    for (const held of [...sessions.keys()]) if (!open.has(held) && busy !== held) closeSession(held);
  });

  const talking = mine.some((call) => call.state === "connected");
  useEffect(() => {
    if (!talking) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [talking]);

  if (!ringing.length && !mine.length && !failure) return null;
  const who = (call: Call) => call.contact || call.number;
  return (
    <VStack gap={2}>
      {failure && <Banner status="error" title={t("The call could not be connected")} description={failure} isDismissable onDismiss={() => setFailure("")} />}
      {mine.map((call) => (
        <Banner
          key={key(call)}
          status={call.state === "connected" ? "success" : "info"}
          title={call.state === "connected" ? t("On a call with {name}").replace("{name}", who(call)) : t("Calling {name}").replace("{name}", who(call))}
          description={call.state === "connected" ? duration(since.get(key(call)) ?? now, now) : call.contact ? call.number : t("Ringing")}
          endContent={call.canHangUp ? <Button label={t("Hang up")} variant="destructive" size="sm" clickAction={() => hangUp(call)} /> : undefined}
        />
      ))}
      {ringing.filter((call) => !sessions.has(key(call))).map((call) => (
        <Banner
          key={key(call)}
          status="info"
          title={t("Incoming call from {name}").replace("{name}", who(call))}
          description={call.contact ? call.number : undefined}
          endContent={(
            <HStack gap={2}>
              <Button label={t("Answer call")} variant="primary" size="sm" isLoading={busy === key(call)} clickAction={() => take(call)} />
            </HStack>
          )}
        />
      ))}
    </VStack>
  );
}
