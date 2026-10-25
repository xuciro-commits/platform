import type { ReactNode } from "react";
import { Notice } from "./Notice";
import { Disclosure } from "../primitives/controls";
import { t } from "../i18n";

/** The one face every refusal shows: a plain-language title, what happened,
 * the next action to take, and — behind a disclosure — the refusal code,
 * request key and recorded facts. Field-level problems stay next to their
 * fields; the caller renders them as children inside this block. */
export function Rejection({ title, message, advice, tone, waiting = false, code, requestKey, trail, children }: {
  /** Plain-language headline; defaults to the waiting or refused phrase. */
  title?: string;
  message: string;
  /** The next action, in words, not a stack trace. */
  advice?: string;
  tone?: "danger" | "warning" | "info";
  /** The outcome is not known yet: the request may still confirm. */
  waiting?: boolean;
  /** The authority's refusal code, kept out of the headline. */
  code?: string;
  /** The saved request (receipt) key, so the outcome can be queried later. */
  requestKey?: string;
  /** The recorded trail: facts, steps, prior attempts. */
  trail?: ReactNode;
  /** Field-level problems rendered beside their fields. */
  children?: ReactNode;
}) {
  const heading = title ?? (waiting ? t("This request is still awaiting confirmation.") : t("The request was refused."));
  return <div role={waiting ? "status" : "alert"} aria-label={heading} className="grid min-w-0 gap-2">
    <Notice title={heading} message={message} tone={waiting ? "warning" : tone ?? "danger"} role="note" />
    {advice && <p className="break-words text-xs text-muted">{advice}</p>}
    {children}
    {(code || requestKey || trail) && <Disclosure summary={t("Technical details")}>
      <div className="grid min-w-0 gap-1 text-xs">
        {code && <p className="break-all font-mono">{code}</p>}
        {requestKey && <p className="break-all font-mono text-muted">{t("Request")}: {requestKey}</p>}
        {trail}
      </div>
    </Disclosure>}
  </div>;
}
