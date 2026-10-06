import { currentSession, signIn, type Api, type OidcConfig } from "@platform/kernel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";

const root = createRoot(document.getElementById("root")!);
const previewWorkspace = new URLSearchParams(location.search).get("preview") === "pattern/workspace";
if (previewWorkspace) {
  // Only the fixture Workspace shell needs its own window. Ordinary examples
  // stay inside Catalog's app shell and the normal host sign-in path.
  const { CatalogPreview } = await import("@platform/catalog-app/view");
  root.render(<StrictMode><CatalogPreview id="pattern/workspace" standalone /></StrictMode>);
} else {
  // The host says how to sign in (ADR-0018): at its identity provider, once for
  // every app; or, on development tokens, as one of its development identities.
  const how = await fetch("/v1/sign-in").then((r) => r.json() as Promise<Api.SignIn>).catch((): Api.SignIn => ({}));
  const oidc: OidcConfig | undefined = how.issuer && how.client ? { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` } : undefined;
  const session = oidc ? (await currentSession(oidc)) ?? (await signIn(oidc)) : undefined;

  // Tenant changes already invalidate reads through /v1/changes. Only views
  // of live operational state opt into their own polling interval.
  const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: false, retry: 1 } } });

  root.render(
    <StrictMode>
      <QueryClientProvider client={queries}>
        <App signedIn={oidc && session ? { config: oidc, session } : undefined} identities={how.identities ?? []} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
