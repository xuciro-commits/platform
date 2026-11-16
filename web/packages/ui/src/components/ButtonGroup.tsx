import type { ButtonOptions } from "../presentation";
import { ArrowRight, Pencil, Plus, Trash2 } from "lucide-react";
import { ButtonGroup as AstryxButtonGroup } from "@astryxdesign/core/ButtonGroup";
import { Button, type ButtonProps } from "../primitives/button";

export function ButtonGroup({ buttons, label, onActivate, enabled = true, isBound }: { buttons: ButtonOptions[]; label: string; onActivate: (id: string) => void; enabled?: boolean; isBound: (id: string) => boolean }) {
  const icons = { arrow: ArrowRight, edit: Pencil, plus: Plus, trash: Trash2 };
  return <AstryxButtonGroup label={label} size="sm">
    {buttons.map((b) => {
      const Icon = icons[b.icon as keyof typeof icons];
      return <Button key={b.id} size="sm" variant={(b.variant || "default") as ButtonProps["variant"]} disabled={!enabled || !isBound(b.id)} onClick={() => onActivate(b.id)}>{Icon && <Icon aria-hidden />}{b.title}</Button>;
    })}
  </AstryxButtonGroup>;
}
