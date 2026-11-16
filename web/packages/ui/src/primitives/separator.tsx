import { Divider } from "@astryxdesign/core/Divider";

export function Separator({ label, name }: { label?: string; name: string }) {
  const text = label?.trim() ? label : undefined;
  return <Divider label={text} aria-label={text ?? name} />;
}
