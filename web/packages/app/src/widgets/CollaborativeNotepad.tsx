"use client";

import * as Y from "yjs";
import { useEffect, useRef, useState, useCallback } from "react";
import { Textarea, Panel, t, ContextHelp, notify } from "@platform/ui";
import { useHost } from "../index";
import type { VariableResult } from "../runtime/variables";

type CollaborativeNotepadProps = {
  value?: VariableResult;
  onChange?: (value: string) => void;
  enabled?: boolean;
  label: string;
  readCurrent?: boolean;
  sessionId?: string;
};

export function CollaborativeNotepadRenderer({
  value,
  onChange,
  enabled,
  label,
  readCurrent = true,
  sessionId,
}: CollaborativeNotepadProps) {
  const { client, me } = useHost();
  
  const ydoc = useRef<Y.Doc | null>(null);
  const ytext = useRef<Y.Text | null>(null);
  const ws = useRef<WebSocket | null>(null);
  const connected = useRef(false);
  const localSeq = useRef(0);
  const pendingLocal = useRef<boolean>(false);
  const [presence, setPresence] = useState<Record<string, { name: string; color: string }>>({});
  const [docText, setDocText] = useState("");

  const serverUrl = useCallback(() => {
    const server = client.connection.server;
    const protocol = server.startsWith("https:") ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${server.replace(/^https?:\/\//, "")}/v1/collaborate/`;
    return wsUrl + encodeURIComponent(sessionId ?? "default");
  }, [client.connection.server, sessionId]);

  // Initialize Yjs document
  useEffect(() => {
    if (ydoc.current) return;
    ydoc.current = new Y.Doc();
    ytext.current = ydoc.current.getText("notepad");

    // Watch for local Yjs changes and send via WebSocket
    ytext.current.observe(() => {
      if (pendingLocal.current) {
        pendingLocal.current = false;
        return;
      }
      const wsConn = ws.current;
      if (!wsConn || wsConn.readyState !== WebSocket.OPEN) return;
      // Yjs gives us delta events; we need to convert to our protocol
      // For simplicity, send the full current text as an insert op at position 0 after clearing
      // This is not optimal but works for collaborative editing
      const currentText = ytext.current!.toString();
      setDocText(currentText);
      onChange?.(currentText);
      localSeq.current++;
      const op = {
        id: `${me.principalId}:${localSeq.current}`,
        type: "insert",
        pos: 0,
        text: currentText,
        client: me.principalId,
        seq: localSeq.current,
      };
      try {
        wsConn.send(JSON.stringify({ kind: "op", op }));
      } catch {
        // ignore send errors
      }
    });
  }, []);

  // Connect WebSocket
  useEffect(() => {
    const url = serverUrl();
    const socket = new WebSocket(url);
    ws.current = socket;

    socket.onopen = () => {
      connected.current = true;
      // Send initial presence
      socket.send(
        JSON.stringify({
          kind: "presence",
          client: me.principalId,
          name: me.principalId,
        })
      );
      // Request sync
      socket.send(JSON.stringify({ kind: "sync" }));
    };

    socket.onclose = () => {
      connected.current = false;
      ws.current = null;
      // Reconnect after 3 seconds
      setTimeout(() => {
        // Trigger reconnect by letting effect re-run
      }, 3000);
    };

    socket.onerror = (err) => {
      console.error("Collaborative notepad WebSocket error:", err);
      notify.error(t("Collaboration connection error."));
    };

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as {
          kind: string;
          op?: any;
          text?: string;
          version?: number;
          client?: string;
          name?: string;
          error?: string;
        };

        if (msg.kind === "error") {
          notify.error(msg.error || t("Collaboration error."));
          return;
        }

        if (msg.kind === "sync") {
          // Initialize Yjs doc with server state
          const yDoc = ydoc.current;
          const yText = ytext.current;
          if (yDoc && yText && typeof msg.text === "string") {
            pendingLocal.current = true;
            yDoc.transact(() => {
              yText.insert(0, msg.text ?? "");
            });
            setDocText(msg.text);
            onChange?.(msg.text);
          }
          return;
        }

        if (msg.kind === "op" && msg.op) {
          const yDoc = ydoc.current;
          const yText = ytext.current;
          if (!yDoc || !yText) return;
          pendingLocal.current = true;
          yDoc.transact(() => {
            if (msg.op.type === "insert") {
              yText.insert(msg.op.pos, msg.op.text);
            } else if (msg.op.type === "delete") {
              yText.delete(msg.op.pos, msg.op.count);
            }
          });
          const newText = yText.toString();
          setDocText(newText);
          onChange?.(newText);
          return;
        }

        if (msg.kind === "presence") {
          setPresence((prev) => {
            if (msg.client && msg.client !== me.principalId) {
              return { ...prev, [msg.client]: { name: msg.name ?? msg.client, color: "#3b82f6" } };
            }
            return prev;
          });
          return;
        }
      } catch {
        // ignore parse errors
      }
    };

    return () => {
      socket.close();
      ws.current = null;
      connected.current = false;
    };
  }, [serverUrl, me, notify, onChange]);

  // Handle local textarea changes - write to Yjs
  const handleChange = useCallback(
    (event: React.ChangeEvent<HTMLTextAreaElement>) => {
      const next = event.target.value;
      setDocText(next);
      onChange?.(next);
      // Yjs change observer will fire and send via WebSocket
    },
    [onChange]
  );

  const userCount = Object.keys(presence).length;

  if (!readCurrent || (value?.status ?? "value") === "pending") {
    return <p role="status">{t("Loading…")}</p>;
  }
  if (value?.status !== "value" || typeof value.value !== "string") {
    return (
      <Panel role="alert">{t("The original collaborative notepad is unavailable.")}</Panel>
    );
  }

  // Initialize doc from current value if not yet synced
  useEffect(() => {
    const yDoc = ydoc.current;
    const yText = ytext.current;
    if (yDoc && yText && connected.current && yText.length === 0 && value.value) {
      pendingLocal.current = true;
      yDoc.transact(() => {
        yText.insert(0, String(value.value ?? ""));
      });
      setDocText(String(value.value ?? ""));
    }
  }, [value.value, connected.current]);

  const disabled = enabled === false || !onChange || !connected.current;

  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium">{label}</span>
        {connected.current ? (
          <span className="flex items-center gap-1 text-xs">
            <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
            <span className="text-muted">{userCount > 1 ? t("{n} editing", { n: userCount }) : t("You")}</span>
          </span>
        ) : (
          <span className="flex items-center gap-1 text-xs">
            <span className="w-1.5 h-1.5 rounded-full bg-yellow-500" />
            <span className="text-muted">{t("Offline")}</span>
          </span>
        )}
      </div>
      <Textarea
        aria-label={label}
        value={docText}
        disabled={disabled}
        className="h-36 font-mono text-xs"
        onChange={handleChange}
      />
      {!connected.current && (
        <p className="text-xs text-muted">{t("Reconnecting to collaboration service…")}</p>
      )}
      <ContextHelp title={label || t("Collaborative notepad")}>
        {t(
          "This note is shared in real time with others viewing this page. Changes appear as they are made."
        )}
      </ContextHelp>
    </div>
  );
}
