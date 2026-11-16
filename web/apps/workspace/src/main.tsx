import { currentSession, type Api, type OidcConfig } from "@platform/api";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { preparedSignIn } from "./offline/signin";
import { SignIn } from "./session/SignIn";
import { AstryxTheme, Button, t } from "@platform/ui";
import { Center } from "@astryxdesign/core/Center";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";

const root = createRoot(document.getElementById("root")!);
class Boundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  render() {
    if (!this.state.error) return this.props.children;
    return <Center height="100dvh" padding={6}>
      <VStack gap={2} maxWidth={576} role="alert">
        <Text as="p">{t("This view could not be displayed.")}</Text>
        <Text type="code" wordBreak="break-word">{this.state.error.message}</Text>
        <Button variant="link" onClick={() => location.reload()}>{t("Reload")}</Button>
      </VStack>
    </Center>;
  }
}
const mount = (view: ReactNode) => root.render(<AstryxTheme><Boundary>{view}</Boundary></AstryxTheme>);
async function start(){
const previewWorkspace = new URLSearchParams(location.search).get("preview") === "pattern/workspace";
if (previewWorkspace) {
  const { CatalogPreview } = await import("@platform/catalog-app/view");
  mount(<StrictMode><CatalogPreview id="pattern/workspace" standalone /></StrictMode>);
} else {
  let offline=false;
  const how = await fetch("/v1/sign-in").then((r) => r.json() as Promise<Api.SignIn>).catch((): Api.SignIn => {offline=true;return preparedSignIn()??{};});
  const oidc: OidcConfig | undefined = how.issuer && how.client ? { issuer: how.issuer, clientId: how.client, redirectUri: `${location.origin}/` } : undefined;
  const session = oidc ? await currentSession(oidc) ?? undefined : undefined;

  const queries = new QueryClient({ defaultOptions: { queries: { refetchInterval: false, retry: 1, networkMode:"always" } } });

  if(oidc&&!session)mount(offline
    ?<Center height="100dvh" padding={6}><VStack role="alert"><Text as="p">{t("Connect to sign in. A new or expired identity cannot use prepared content.")}</Text></VStack></Center>
    :<StrictMode><SignIn config={oidc}/></StrictMode>);
  else mount(
    <StrictMode>
      <QueryClientProvider client={queries}>
        <App signedIn={oidc && session ? { config: oidc, session } : undefined} identities={how.identities ?? []} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
}
void start().catch(problem=>mount(<Center height="100dvh" padding={6}><VStack gap={2} role="alert"><Text as="p">{t("This workspace task could not be reopened. Connect and reload; saved inputs were retained.")}</Text><Text as="p" type="supporting">{t(problem instanceof Error?problem.message:String(problem))}</Text></VStack></Center>));