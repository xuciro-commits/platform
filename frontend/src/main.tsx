import "./globals.css";
import { InternationalizationProvider } from "@astryxdesign/core/i18n";
import astryxZhCN from "@astryxdesign/core/locales/zh-CN.generated.js";
import { LayerProvider } from "@astryxdesign/core/Layer";
import { LinkProvider } from "@astryxdesign/core/Link";
import { Theme } from "@astryxdesign/core/theme";
import { ArrowUpIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { defaultThemeSlug, themes } from "./astryx-themes";
import { useLiveChanges, useSession } from "./host";
import { useLocale } from "./i18n";
import { PeekProvider } from "./pages/RecordPeek";
import { RouterLink } from "./router";
import { useAppearance } from "./shell/appearance";
import { Shell } from "./shell/Shell";

if (import.meta.env.DEV) void import("virtual:stylex:runtime");

const styles = stylex.create({
  sortHint: { opacity: { default: 0.65, [stylex.when.ancestor(":not(:hover):not(:focus-within)")]: 0 } },
});

const astryxMessages = { "zh-CN": astryxZhCN };
const base = themes[defaultThemeSlug];
const theme = { ...base, icons: { ...base.icons, arrowsUpDown: <ArrowUpIcon {...stylex.props(styles.sortHint)} /> } };

function Root() {
  const locale = useLocale();
  const tenant = useSession()?.tenant;
  useLiveChanges();
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  return (
    <InternationalizationProvider locale={locale} messages={astryxMessages}>
      <Theme theme={theme} mode={useAppearance()}>
        <LayerProvider>
          <LinkProvider component={RouterLink}>
            <PeekProvider key={`${locale} ${tenant}`}>
              <Shell />
            </PeekProvider>
          </LinkProvider>
        </LayerProvider>
      </Theme>
    </InternationalizationProvider>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
