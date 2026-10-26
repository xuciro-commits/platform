import { t } from "../i18n";

/**
 * Humanizes kernel or authority error codes into clear, localized user-facing messages (Testing C3).
 */
export function humanizeKernelError(codeOrMessage?: string): string {
  if (!codeOrMessage) return t("refused");
  const key = codeOrMessage.replace(/^Error:\s*/, "");
  switch (key) {
    case "ERROR_CODE_CONFLICT":
      return t("The record was modified by another operation. Please refresh and try again.");
    case "ERROR_CODE_IDEMPOTENCY_CONFLICT":
      return t("A conflicting submission was already processed with this key.");
    case "ERROR_CODE_POLICY_DENIED":
      return t("You do not have permission to perform this action.");
    case "ERROR_CODE_NOT_FOUND":
      return t("The record was not found or is outside your scope.");
    case "ERROR_CODE_INVALID_ARGUMENT":
      return t("The submitted data is invalid.");
    case "ERROR_CODE_INVALID_REFERENCE":
      return t("The referenced record does not exist.");
    case "ERROR_CODE_UNKNOWN_SCHEMA":
      return t("Unknown action or schema.");
    case "ERROR_CODE_NOT_AUTHORITY":
      return t("The host is not the authority for this action.");
    default:
      return codeOrMessage;
  }
}
