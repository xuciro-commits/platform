import { Search } from "lucide-react";
import type { InputHTMLAttributes } from "react";
import { TextInput } from "@astryxdesign/core/TextInput";
import { t } from "../i18n";

export function SearchInput({ value, onChange, scope, disabled, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "value" | "onChange" | "placeholder"> & { value: string; onChange: (value: string) => void; scope: string[] }) {
  if (!scope.length) return <p role="status">{t("Search scope is unavailable.")}</p>;
  const label = typeof props["aria-label"] === "string" ? props["aria-label"] : t("Search");
  return <TextInput label={label} isLabelHidden type="search" size="sm" width="100%" value={value} onChange={onChange} isDisabled={disabled} placeholder={t("Search records…")} startIcon={Search} hasClear description={t("Search in: {objects}", { objects: scope.join(", ") })} />;
}
