import { currentSession, signIn } from "@platform/kernel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";

// With VITE_OIDC_ISSUER the plant runs on its production path: users sign in at
// the identity provider (deploy/local); without it, demo identities are offered.
const issuer = import.meta.env.VITE_OIDC_ISSUER as string | undefined;
const oidc = issuer ? { issuer, clientId: "mes-web", redirectUri: `${location.origin}/` } : undefined;
const session = oidc ? (await currentSession(oidc)) ?? (await signIn(oidc)) : undefined;

const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: 3000, retry: 1 } } });

createRoot(document.getElementById("root")!).render(
  <StrictMode><QueryClientProvider client={queries}><App signedIn={oidc && session ? { config: oidc, session } : undefined} /></QueryClientProvider></StrictMode>,
);
