import "./globals.css";
import { LinkProvider } from "@astryxdesign/core/Link";
import { Theme } from "@astryxdesign/core/theme";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { defaultThemeSlug, themes } from "./astryx-themes";
import { RouterLink } from "./router";
import { Shell } from "./shell/Shell";

if (import.meta.env.DEV) void import("virtual:stylex:runtime");

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Theme theme={themes[defaultThemeSlug]}>
      <LinkProvider component={RouterLink}>
        <Shell />
      </LinkProvider>
    </Theme>
  </StrictMode>,
);
