// The builder's UI (ADR-0034): the objects this organisation defines, and the
// objects it has published. Both are pages of the host's definition registry,
// rendered by the same component a code page uses — the builder gets the shared
// list/detail frame, generated forms and keyboard behaviour, and nothing here
// interprets a definition of its own.
import "./i18n";
import { defineApp } from "@platform/app";
import { t, type NavSection } from "@platform/ui";
import { Boxes, Hammer } from "lucide-react";

/** The page the host installed for an object, by the object's own name. */
const objectPage = (name: string) => ({ view: "page", params: { app: "build", kind: "page", name } });

export default defineApp({
  id: "build",
  title: t("Builder"),
  icon: <Hammer />,
  home: objectPage("objects"),
  views: [],
  nav: (host): NavSection[] => {
    const published = host.entities.filter((info) => info.app === "build" && info.type !== "build.object");
    return [
      { label: t("Builder"), items: [{ label: t("Objects"), icon: <Hammer />, route: objectPage("objects") }] },
      ...(published.length > 0
        ? [{
          label: t("What this organisation defined"),
          items: published.map((info) => ({ label: info.plural, icon: <Boxes />, route: objectPage(info.type.replace(/^build\./, "")) })),
        }]
        : []),
    ];
  },
});
