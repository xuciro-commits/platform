import { type Browser, type Page, expect, test } from "@playwright/test";
import { signIn, submit } from "./support";

const caller = "+8613800138000";

test.use({
  permissions: ["microphone"],
  launchOptions: { args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream"] },
});

async function seat(browser: Browser, name: string) {
  const context = await browser.newContext({ permissions: ["microphone"] });
  await context.addInitScript(() => {
    const list: AudioContext[] = [];
    const Native = window.AudioContext;
    if (Native) {
      class TrackedAudioContext extends Native {
        constructor(...args: ConstructorParameters<typeof AudioContext>) {
          super(...args);
          list.push(this);
        }
      }
      Object.defineProperty(window, "AudioContext", { value: TrackedAudioContext, configurable: true, writable: true });
    }
    (window as unknown as { __audioContexts: AudioContext[] }).__audioContexts = list;
  });
  const page = await context.newPage();
  await signIn(page, `${name} · phone`);
  return page;
}

async function audioContextStates(page: Page) {
  return page.evaluate(() => ((window as unknown as { __audioContexts?: AudioContext[] }).__audioContexts ?? []).map((ctx) => ctx.state));
}

async function call(gateway: Page, id: string) {
  return gateway.evaluate(async (id) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const response = await fetch(`/v1/records/phone.call/${encodeURIComponent(id)}`, {
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant },
    });
    const view = await response.json() as { record: Record<string, unknown> };
    const plain = (value: unknown) => (value && typeof value === "object" && "value" in value ? (value as { value: unknown }).value : value);
    return Object.fromEntries(Object.entries(view.record).map(([key, value]) => [key, plain(value)])) as Record<string, unknown>;
  }, id);
}

async function answerFor(gateway: Page, offer: string) {
  return gateway.evaluate(async (offer) => {
    const pc = new RTCPeerConnection();
    await pc.setRemoteDescription({ type: "offer", sdp: offer });
    await pc.setLocalDescription(await pc.createAnswer());
    return pc.localDescription!.sdp;
  }, offer);
}

test("phone tenant: an agent answers an incoming call in the browser and hangs it up", async ({ browser }) => {
  const agent = await seat(browser, "user-1");
  const gateway = await seat(browser, "manager-1");

  const created = await submit(gateway, "phone.call.create", "phone.call", "", { number: caller });
  const id = created.record.submission.target.id as string;
  const ringing = await call(gateway, id);
  await submit(gateway, "phone.call.receive", "phone.call", id, { line: "phone1" }, ringing.revision as number);

  const banner = agent.getByText(`Incoming call from ${caller}`, { exact: true });
  await expect(banner).toBeVisible({ timeout: 15_000 });
  await agent.getByRole("button", { name: "Answer call", exact: true }).click();

  await expect.poll(async () => String((await call(gateway, id)).offer ?? ""), { timeout: 15_000 }).toContain("m=audio");
  const taken = await call(gateway, id);
  expect(taken.agent).toBeTruthy();
  const answer = await answerFor(gateway, String(taken.offer));
  await submit(gateway, "phone.call.connect", "phone.call", id, { agent: taken.agent, line: "phone1", answer }, taken.revision as number);

  await expect(agent.getByText(`On a call with ${caller}`, { exact: true })).toBeVisible({ timeout: 15_000 });
  await expect(banner).toHaveCount(0);
  await agent.getByRole("button", { name: "Hang up", exact: true }).click();
  await expect.poll(async () => (await call(gateway, id)).ended, { timeout: 15_000 }).toBe(true);

  const ended = await call(gateway, id);
  await submit(gateway, "phone.call.finish", "phone.call", id, { seconds: 3 }, ended.revision as number);
  await expect(agent.getByText(`On a call with ${caller}`, { exact: true })).toHaveCount(0, { timeout: 15_000 });
  expect((await call(gateway, id)).state).toBe("completed");
});

test("phone tenant: an outbound call plays a ringback AudioContext while ringing and closes it on connect", async ({ browser }) => {
  const agent = await seat(browser, "user-1");
  const gateway = await seat(browser, "manager-1");
  const callee = "+8613900139000";

  const created = await submit(agent, "phone.call.create", "phone.call", "", { number: callee });
  const id = created.record.submission.target.id as string;

  await expect(agent.getByText(`Calling ${callee}`, { exact: true })).toBeVisible({ timeout: 15_000 });
  await expect.poll(async () => String((await call(gateway, id)).offer ?? ""), { timeout: 15_000 }).toContain("m=audio");
  await expect.poll(async () => (await audioContextStates(agent)).filter((state) => state === "running").length, { timeout: 15_000 }).toBe(1);

  const ringing = await call(gateway, id);
  const answer = await answerFor(gateway, String(ringing.offer));
  await submit(gateway, "phone.call.connect", "phone.call", id, { agent: ringing.agent, line: "phone1", answer }, ringing.revision as number);

  await expect(agent.getByText(`On a call with ${callee}`, { exact: true })).toBeVisible({ timeout: 15_000 });
  await expect.poll(async () => {
    const states = await audioContextStates(agent);
    return states.length > 0 && states.every((state) => state === "closed");
  }, { timeout: 15_000 }).toBe(true);

  await agent.getByRole("button", { name: "Hang up", exact: true }).click();
  await expect.poll(async () => (await call(gateway, id)).ended, { timeout: 15_000 }).toBe(true);
  const ended = await call(gateway, id);
  await submit(gateway, "phone.call.finish", "phone.call", id, { seconds: 2 }, ended.revision as number);
  await expect(agent.getByText(`On a call with ${callee}`, { exact: true })).toHaveCount(0, { timeout: 15_000 });
});
