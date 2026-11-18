import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Kbd } from "@astryxdesign/core/Kbd";
import { Layout, LayoutContent } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { useEffect, useState } from "react";
import { t } from "../i18n";

export function KeyboardHelp() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const show = (event: KeyboardEvent) => {
      if (event.key !== "?" || event.defaultPrevented || event.isComposing || event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.target instanceof HTMLElement &&
        (event.target.isContentEditable || event.target.closest("input, textarea, select, [role=dialog], [role=alertdialog], [role=menu]"))) return;
      event.preventDefault();
      setOpen(true);
    };
    addEventListener("keydown", show);
    return () => removeEventListener("keydown", show);
  }, []);
  if (!open) return null;
  return (
    <Dialog isOpen onOpenChange={setOpen} purpose="info">
      <Layout header={<DialogHeader title={t("Keyboard shortcuts")} onOpenChange={setOpen} />} content={
        <LayoutContent padding={4}>
          <MetadataList>
            <MetadataListItem label={t("Move to the next field or control")}><Kbd keys="tab" /></MetadataListItem>
            <MetadataListItem label={t("Move to the previous field or control")}><Kbd keys="shift+tab" /></MetadataListItem>
            <MetadataListItem label={t("Save the field or activate the control")}><Kbd keys="enter" /></MetadataListItem>
            <MetadataListItem label={t("Close the current overlay")}><Kbd keys="escape" /></MetadataListItem>
          </MetadataList>
        </LayoutContent>
      } />
    </Dialog>
  );
}
