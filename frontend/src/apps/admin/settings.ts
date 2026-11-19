import type { Api } from "../../host";
import { settingsPage } from "../../pages/SettingsPage";

export default settingsPage<Api.AppSettings[]>({
  title: "Settings",
  about: "Each app's settings for this workspace; a change applies to every member.",
  read: "/v1/settings",
  groups: (answer) => answer.map((app) => ({ id: app.app, label: app.title || app.app, settings: app.settings })),
  action: "platform.setting.set",
  target: (app, setting) => `${app}/${setting}`,
});
