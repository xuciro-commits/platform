import type { StyleXStyles } from "@stylexjs/stylex";
import { Icon } from "@astryxdesign/core/Icon";
import { Link } from "@astryxdesign/core/Link";
import { Typeahead } from "@astryxdesign/core/Typeahead";
import { useMemo, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { elementHref, findElements } from "./enterprise";
import type { InputStatus } from "./ParameterInput";

type ElementInputProps = {
  control?: StyleXStyles;
  id: string;
  name?: string;
  stereotypes?: string[];
  label: string;
  description?: string;
  required?: boolean;
  focused?: boolean;
  labelHidden?: boolean;
  status?: InputStatus;
  onChange: (value: string) => void;
};

export function ElementInput({ id, name, stereotypes, label, description, required, focused, labelHidden, status, onChange, control }: ElementInputProps) {
  const [picked, setPicked] = useState<{ id: string; label: string }>();
  const resolved = useRead<Api.EnterpriseQueryResult>(id && !name && picked?.id !== id ? `/v1/enterprise-resolve?ids=${encodeURIComponent(id)}` : null);
  const known = resolved.status === "ready" ? resolved.data.elements.find((element) => element.id === id)?.name : undefined;
  const kinds = (stereotypes ?? []).join(",");
  const source = useMemo(() => ({ bootstrap: () => findElements(stereotypes, ""), search: (query: string) => findElements(stereotypes, query) }), [kinds]);
  const selection = id ? { id, label: name ?? (picked?.id === id ? picked.label : known ?? id) } : null;
  return (
    <Typeahead
      label={label}
      isLabelHidden={labelHidden}
      description={description}
      isRequired={required}
      status={status}
      controlXstyle={control}
      hasAutoFocus={focused}
      hasEntriesOnFocus
      maxMenuItems={9}
      placeholder={t("Find in the organization")}
      emptySearchText={t("Nothing in the organization matches")}
      searchSource={source}
      value={selection}
      onChange={(next) => {
        setPicked(next ?? undefined);
        onChange(next?.id ?? "");
      }}
    />
  );
}

export function ElementLink({ id, name, compact }: { id: string; name: string; compact?: boolean }) {
  return (
    <Link href={elementHref(id)} label={compact ? name : undefined}>
      {compact ? <Icon icon="externalLink" size="sm" /> : name}
    </Link>
  );
}
