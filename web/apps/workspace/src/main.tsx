import { currentSession, signIn, type OidcConfig } from "@platform/kernel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, type Identity } from "./App";
import "./styles.css";

// The host says how to sign in (ADR-0018): at its identity provider, once for
// every app; or, on development tokens, as one of its development identities.
const how = await fetch("/v1/sign-in").then((r) => r.json() as Promise<{ issuer?: string; client?: string; identities?: Identity[] }>)
  .catch(() => ({} as { issuer?: string; client?: string; identities?: Identity[] }));
const oidc: OidcConfig | undefined = how.issuer && how.client ? { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` } : undefined;
const session = oidc ? (await currentSession(oidc)) ?? (await signIn(oidc)) : undefined;

const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: 3000, retry: 1 } } });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queries}>
      <App signedIn={oidc && session ? { config: oidc, session } : undefined} identities={how.identities ?? []} />
    </QueryClientProvider>
  </StrictMode>,
);
