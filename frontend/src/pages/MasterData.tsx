import { settingsItemId } from "../apps";
import { type Api, useRead } from "../host";
import { ListView } from "./ListPage";

type Entry = { id: string; title: string; set: string; about: string; href: string };

const entries = (views: Api.ApplicationView[]): Entry[] => views
  .filter((view) => view.surface === "master-data")
  .flatMap((view) => (view.menu ?? []).flatMap((group) => group.entries.filter((entry) => entry.object).map((entry) => ({
    id: `${view.name || view.app}/${entry.name}`,
    title: entry.title || entry.name,
    set: view.title,
    about: entry.description ?? "",
    href: `/${view.name || view.app}/${group.layout === "settings" ? settingsItemId(group.title) : entry.name}`,
  }))));

export function MasterData() {
  const read = useRead<Api.ApplicationView[]>("/v1/applications");
  return (
    <ListView
      read={read}
      definition={{
        title: "Master data",
        about: "The records every application shares, such as business partners, places and units of measure. Master data packages list them here instead of adding an application to the rail.",
        rows: entries,
        id: (entry) => entry.id,
        link: (entry) => entry.href,
        groups: [{ key: "set", label: "Package", of: (entry) => entry.set }],
        columns: [
          { key: "title", header: "Name", text: (entry) => entry.title },
          { key: "set", header: "Package", text: (entry) => entry.set },
          { key: "about", header: "Description", text: (entry) => entry.about },
        ],
      }}
    />
  );
}
