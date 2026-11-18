import type { Api } from "./contract";
import { HostRefusal, request } from "./request";
import type { Session } from "./session";

const pending = "platform:sign-in";
const scope = "openid email profile";

const endpoint = (issuer: string, name: string) => `${new URL(issuer).pathname.replace(/\/?$/, "/")}oidc/${name}`;
const encoded = (bytes: ArrayBuffer | Uint8Array) =>
  btoa(String.fromCharCode(...new Uint8Array(bytes))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const random = () => encoded(crypto.getRandomValues(new Uint8Array(32)));
const returnTo = () => `${location.origin}/`;

async function exchange(issuer: string, form: Record<string, string>): Promise<Session> {
  const response = await fetch(endpoint(issuer, "token"), {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(form),
  });
  const answer = await response.json().catch(() => ({})) as { access_token?: string; error?: string; error_description?: string };
  if (!response.ok || !answer.access_token) throw new HostRefusal(response.status, answer.error ?? "", answer.error_description ?? "");
  const token = answer.access_token;
  const me = await request<Api.MeView>("/v1/me", { headers: { authorization: `Bearer ${token}` } });
  return { token, tenant: me.tenantId };
}

export function signInWithPassword(sign: Api.SignIn, username: string, password: string) {
  return exchange(sign.issuer ?? "", { grant_type: "password", client_id: sign.client ?? "", username, password, scope });
}

export async function startSingleSignOn(sign: Api.SignIn) {
  const verifier = random();
  const state = random();
  sessionStorage.setItem(pending, JSON.stringify({ verifier, state, back: location.pathname + location.search }));
  const challenge = encoded(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier)));
  const query = new URLSearchParams({
    response_type: "code", client_id: sign.client ?? "", redirect_uri: returnTo(), scope, state,
    code_challenge: challenge, code_challenge_method: "S256",
  });
  location.assign(`${endpoint(sign.issuer ?? "", "authorize")}?${query}`);
}

export function returningFromSingleSignOn() {
  const params = new URLSearchParams(location.search);
  return params.has("code") && params.has("state") && sessionStorage.getItem(pending) !== null;
}

export async function finishSingleSignOn(sign: Api.SignIn): Promise<{ session: Session; back: string }> {
  const params = new URLSearchParams(location.search);
  const saved = JSON.parse(sessionStorage.getItem(pending) ?? "{}") as { verifier?: string; state?: string; back?: string };
  sessionStorage.removeItem(pending);
  if (!saved.verifier || params.get("state") !== saved.state) throw new HostRefusal(400, "state", "");
  const session = await exchange(sign.issuer ?? "", {
    grant_type: "authorization_code", client_id: sign.client ?? "", code: params.get("code") ?? "",
    redirect_uri: returnTo(), code_verifier: saved.verifier,
  });
  return { session, back: saved.back || "/" };
}
