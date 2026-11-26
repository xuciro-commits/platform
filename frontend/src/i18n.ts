import admin from "./i18n/admin.json";
import shell from "./i18n/shell.json";
import values from "./i18n/values.json";
import views from "./i18n/views.json";
export { dateText, dayText, setDateFormat, setLanguage, setTimeZone, t, useLocale } from "./language";

export const zhCN: Record<string, string> = {
  ...values,
  ...shell,
  ...views,
  ...admin,
};
