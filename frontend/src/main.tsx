import "./globals.css";
import { LayerProvider } from "@astryxdesign/core/Layer";
import { LinkProvider } from "@astryxdesign/core/Link";
import { Theme } from "@astryxdesign/core/theme";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { defaultThemeSlug, themes } from "./astryx-themes";
import { useSession } from "./host";
import { useLocale } from "./i18n";
import { RouterLink } from "./router";
import { useAppearance } from "./shell/appearance";
import { Shell } from "./shell/Shell";

if (import.meta.env.DEV) void import("virtual:stylex:runtime");

function Root() {
  const locale = useLocale();
  const tenant = useSession()?.tenant;
  return (
    <Theme theme={themes[defaultThemeSlug]} mode={useAppearance()}>
      <LayerProvider>
        <LinkProvider component={RouterLink}>
          <Shell key={`${locale} ${tenant}`} />
        </LinkProvider>
      </LayerProvider>
    </Theme>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
