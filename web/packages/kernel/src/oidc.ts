export type OidcConfig = { issuer: string; clientId: string; redirectUri: string };
export type OidcSession = { issuer: string; clientId: string; accessToken: string; refreshToken?: string; idToken?: string; expiresAt: number; email: string };

type Discovery = { authorization_endpoint: string; token_endpoint: string; end_session_endpoint?: string };
type TokenResponse = { access_token: string; refresh_token?: string; id_token?: string; expires_in: number };
type Pending = Pick<OidcConfig, "issuer" | "clientId"> & { verifier: string; state: string };

const SESSION = "oidc:session";
const PENDING = "oidc:pending";
const sameProvider = (config: OidcConfig, saved: Pick<OidcConfig, "issuer" | "clientId"> | null) => saved?.issuer === config.issuer && saved?.clientId === config.clientId;
function saved<T>(key: string): T | null {
  try { return JSON.parse(sessionStorage.getItem(key) ?? "null") as T | null; }
  catch { sessionStorage.removeItem(key); return null; }
}
function clearSession() { sessionStorage.removeItem(SESSION); sessionStorage.removeItem(PENDING); }

const base64url = (bytes: Uint8Array) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const random = (n: number) => base64url(crypto.getRandomValues(new Uint8Array(n)));

export async function codeChallenge(verifier: string): Promise<string> {
  return base64url(new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier))));
}

async function discover(issuer: string): Promise<Discovery> {
  const response = await fetch(issuer.replace(/\/?$/, "/") + ".well-known/openid-configuration");
  if (!response.ok) throw new Error(`OpenID discovery: HTTP ${response.status}`);
  return response.json() as Promise<Discovery>;
}

function store(config: OidcConfig, t: TokenResponse): OidcSession {
  const claims = JSON.parse(atob(t.access_token.split(".")[1]!.replace(/-/g, "+").replace(/_/g, "/"))) as { email?: string };
  const session = { issuer: config.issuer, clientId: config.clientId, accessToken: t.access_token, refreshToken: t.refresh_token, idToken: t.id_token,
    expiresAt: Date.now() + t.expires_in * 1000, email: claims.email ?? "" };
  sessionStorage.setItem(SESSION, JSON.stringify(session));
  return session;
}

async function token(config: OidcConfig, grant: Record<string, string>): Promise<OidcSession | null> {
  const { token_endpoint } = await discover(config.issuer);
  const response = await fetch(token_endpoint, { method: "POST", body: new URLSearchParams({ client_id: config.clientId, ...grant }) });
  return response.ok ? store(config, await response.json() as TokenResponse) : null;
}

export async function signIn(config: OidcConfig): Promise<never> {
  const { authorization_endpoint } = await discover(config.issuer);
  const verifier = random(32), state = random(16);
  sessionStorage.setItem(PENDING, JSON.stringify({ issuer: config.issuer, clientId: config.clientId, verifier, state } satisfies Pending));
  const url = new URL(authorization_endpoint);
  url.search = new URLSearchParams({ response_type: "code", client_id: config.clientId, redirect_uri: config.redirectUri,
    scope: "openid email profile", state, code_challenge: await codeChallenge(verifier), code_challenge_method: "S256" }).toString();
  location.assign(url);
  return new Promise<never>(() => {});
}

export async function currentSession(config: OidcConfig): Promise<OidcSession | null> {
  const params = new URLSearchParams(location.search);
  const code = params.get("code");
  if (code) {
    const pending = saved<Pending>(PENDING);
    sessionStorage.removeItem(PENDING);
    history.replaceState(null, "", location.pathname + location.hash);
    if (!pending || !sameProvider(config, pending) || pending.state !== params.get("state")) { clearSession(); return null; }
    return token(config, { grant_type: "authorization_code", code, redirect_uri: config.redirectUri, code_verifier: pending.verifier });
  }
  const stored = saved<OidcSession>(SESSION);
  if (stored && sameProvider(config, stored) && stored.expiresAt > Date.now() + 30_000) return stored;
  clearSession(); return null;
}

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

export async function signOut(config: OidcConfig): Promise<void> {
  const stored = saved<OidcSession>(SESSION), session = sameProvider(config, stored) ? stored : null;
  if (!session) clearSession();
  const { end_session_endpoint } = await discover(config.issuer);
  clearSession();
  if (!end_session_endpoint) return location.assign(config.redirectUri);
  const url = new URL(end_session_endpoint);
  url.search = new URLSearchParams({ post_logout_redirect_uri: config.redirectUri, ...(session?.idToken ? { id_token_hint: session.idToken } : {}) }).toString();
  location.assign(url);
}
