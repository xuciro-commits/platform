import { currentSession, signIn, type Api, type OidcConfig } from "@platform/kernel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";
import {preparedSignIn} from "./offline/signin";
import {t} from "@platform/ui";

const root = createRoot(document.getElementById("root")!);
async function start(){
const previewWorkspace = new URLSearchParams(location.search).get("preview") === "pattern/workspace";
if (previewWorkspace) {
  const { CatalogPreview } = await import("@platform/catalog-app/view");
  root.render(<StrictMode><CatalogPreview id="pattern/workspace" standalone /></StrictMode>);
} else {
  let offline=false;
  const how = await fetch("/v1/sign-in").then((r) => r.json() as Promise<Api.SignIn>).catch((): Api.SignIn => {offline=true;return preparedSignIn()??{};});
  const oidc: OidcConfig | undefined = how.issuer && how.client ? { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` } : undefined;
  const session = oidc ? (await currentSession(oidc)) ?? (!offline?await signIn(oidc):undefined) : undefined;

  const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: false, retry: 1, networkMode:"always" } } });

  if(oidc&&!session)root.render(<main role="alert" className="grid h-dvh place-items-center p-6 text-sm">{t("Connect to sign in. A new or expired identity cannot use prepared content.")}</main>);
  else root.render(
    <StrictMode>
      <QueryClientProvider client={queries}>
        <App signedIn={oidc && session ? { config: oidc, session } : undefined} identities={how.identities ?? []} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
}
void start().catch(problem=>root.render(<main role="alert" className="grid h-dvh place-items-center p-6 text-sm"><div><p>{t("This workspace task could not be reopened. Connect and reload; saved inputs were retained.")}</p><p className="mt-2 text-muted">{t(problem instanceof Error?problem.message:String(problem))}</p></div></main>));
