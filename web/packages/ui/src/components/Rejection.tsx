import type { ReactNode } from "react";
import { Notice } from "./Notice";
import { Disclosure } from "../primitives/controls";
import { t } from "../i18n";

export function Rejection({ title, message, advice, tone, waiting = false, code, requestKey, trail, children }: {
  title?: string;
  message: string;
  advice?: string;
  tone?: "danger" | "warning" | "info";
  waiting?: boolean;
  code?: string;
  requestKey?: string;
  trail?: ReactNode;
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
