// The workspace's entry: mount the platform UI. When the host names an OIDC
// provider, the page finishes the provider's redirect (or starts it) before the
// first render; the token joins the host's session and the tenant is learned
// from GET /v1/me on the way in. Without a provider, the UI asks for a token.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { currentSession, signIn } from "@platform/kernel";
import { PlatformApp } from "@pkg/platform";
import "@pkg/platform/styles.css";
import "./styles.css";

const root = createRoot(document.getElementById("root")!);
const store = "workspace:session";

async function fromProvider(): Promise<void> {
  const how = await fetch("/v1/sign-in").then((response) => response.json()).catch(() => undefined) as { issuer?: string; client?: string } | undefined;
  if (!how?.issuer || !how.client) return;
  if (sessionStorage.getItem(store)) return; // an established host session outranks the provider
  const config = { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` };
  const session = (await currentSession(config)) ?? (await signIn(config));
  if (!session) return;
  sessionStorage.setItem(store, JSON.stringify({ token: session.accessToken, tenant: "" }));
}

void fromProvider().catch(() => undefined).finally(() => {
  root.render(
    <StrictMode>
      <PlatformApp />
    </StrictMode>,
  );
});
