import {initializeRules} from "@platform/app/rules";
import { currentSession, signIn, type Api, type OidcConfig } from "@platform/kernel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";

await initializeRules();

const root = createRoot(document.getElementById("root")!);
const how = await fetch("/v1/sign-in").then((r) => r.json() as Promise<Api.SignIn>).catch((): Api.SignIn => ({}));
const oidc: OidcConfig | undefined = how.issuer && how.client ? { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` } : undefined;
const session = oidc ? (await currentSession(oidc)) ?? (await signIn(oidc)) : undefined;
const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: false, retry: 1 } } });

root.render(
  <StrictMode>
    <QueryClientProvider client={queries}>
      <App signedIn={oidc && session ? { config: oidc, session } : undefined} identities={how.identities ?? []} />
    </QueryClientProvider>
  </StrictMode>,
);
