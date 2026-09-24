// Browser sign-in with an OpenID provider (docs/ADR/0007): the authorization
// code flow with PKCE (RFC 7636) that public clients use. The provider proves who
// the user is; the platform server verifies the access token and looks the user
// up in the tenant's directory. Tokens live in sessionStorage (one tab, one user).
export type OidcConfig = { issuer: string; clientId: string; redirectUri: string };
export type OidcSession = { accessToken: string; refreshToken?: string; idToken?: string; expiresAt: number; email: string };

type Discovery = { authorization_endpoint: string; token_endpoint: string; end_session_endpoint?: string };
type TokenResponse = { access_token: string; refresh_token?: string; id_token?: string; expires_in: number };

const SESSION = "oidc:session";
const PENDING = "oidc:pending";

const base64url = (bytes: Uint8Array) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const random = (n: number) => base64url(crypto.getRandomValues(new Uint8Array(n)));

/** The S256 code challenge of a verifier. */
export async function codeChallenge(verifier: string): Promise<string> {
  return base64url(new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier))));
}

async function discover(issuer: string): Promise<Discovery> {
  const response = await fetch(issuer.replace(/\/?$/, "/") + ".well-known/openid-configuration");
  if (!response.ok) throw new Error(`OpenID discovery: HTTP ${response.status}`);
  return response.json() as Promise<Discovery>;
}

function store(t: TokenResponse): OidcSession {
  const claims = JSON.parse(atob(t.access_token.split(".")[1]!.replace(/-/g, "+").replace(/_/g, "/"))) as { email?: string };
  const session = { accessToken: t.access_token, refreshToken: t.refresh_token, idToken: t.id_token,
    expiresAt: Date.now() + t.expires_in * 1000, email: claims.email ?? "" };
  sessionStorage.setItem(SESSION, JSON.stringify(session));
  return session;
}

async function token(config: OidcConfig, grant: Record<string, string>): Promise<OidcSession | null> {
  const { token_endpoint } = await discover(config.issuer);
  const response = await fetch(token_endpoint, { method: "POST", body: new URLSearchParams({ client_id: config.clientId, ...grant }) });
  return response.ok ? store(await response.json() as TokenResponse) : null;
}

/** Leaves for the provider's sign-in page; the page comes back with a code. */
export async function signIn(config: OidcConfig): Promise<never> {
  const { authorization_endpoint } = await discover(config.issuer);
  const verifier = random(32), state = random(16);
  sessionStorage.setItem(PENDING, JSON.stringify({ verifier, state }));
  const url = new URL(authorization_endpoint);
  url.search = new URLSearchParams({ response_type: "code", client_id: config.clientId, redirect_uri: config.redirectUri,
    scope: "openid email profile", state, code_challenge: await codeChallenge(verifier), code_challenge_method: "S256" }).toString();
  location.assign(url);
  return new Promise<never>(() => {});
}

/** Completes a sign-in when the page is the provider's redirect, else returns the stored, unexpired session. */
export async function currentSession(config: OidcConfig): Promise<OidcSession | null> {
  const params = new URLSearchParams(location.search);
  const code = params.get("code");
  if (code) {
    const pending = JSON.parse(sessionStorage.getItem(PENDING) ?? "null") as { verifier: string; state: string } | null;
    sessionStorage.removeItem(PENDING);
    history.replaceState(null, "", location.pathname + location.hash);
    if (!pending || pending.state !== params.get("state")) return null;
    return token(config, { grant_type: "authorization_code", code, redirect_uri: config.redirectUri, code_verifier: pending.verifier });
  }
  const stored = JSON.parse(sessionStorage.getItem(SESSION) ?? "null") as OidcSession | null;
  return stored && stored.expiresAt > Date.now() + 30_000 ? stored : null;
}

/** Renews the access token shortly before it expires; signs in again when renewal is refused. */
export function keepFresh(config: OidcConfig, session: OidcSession, renewed: (s: OidcSession) => void): () => void {
  let timer: ReturnType<typeof setTimeout>;
  const schedule = (s: OidcSession) => {
    timer = setTimeout(async () => {
      const next = s.refreshToken ? await token(config, { grant_type: "refresh_token", refresh_token: s.refreshToken }).catch(() => null) : null;
      if (!next) return signIn(config);
      renewed(next);
      schedule(next);
    }, Math.max(0, s.expiresAt - Date.now() - 60_000));
  };
  schedule(session);
  return () => clearTimeout(timer);
}

/** Ends the session here and at the provider. */
export async function signOut(config: OidcConfig): Promise<void> {
  const session = JSON.parse(sessionStorage.getItem(SESSION) ?? "null") as OidcSession | null;
  sessionStorage.removeItem(SESSION);
  const { end_session_endpoint } = await discover(config.issuer);
  if (!end_session_endpoint) return location.assign(config.redirectUri);
  const url = new URL(end_session_endpoint);
  url.search = new URLSearchParams({ post_logout_redirect_uri: config.redirectUri, ...(session?.idToken ? { id_token_hint: session.idToken } : {}) }).toString();
  location.assign(url);
}
