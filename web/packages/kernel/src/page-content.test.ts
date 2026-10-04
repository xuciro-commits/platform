import {afterEach,expect,test,vi} from "vitest";
import {EdgeClient} from "./client";
afterEach(()=>vi.unstubAllGlobals());
const ref={app:"build",kind:"page",name:"child"},version="page.sha256."+"a".repeat(64),client=()=>new EdgeClient({server:"https://test.invalid",token:"caller",tenant:"tenant",principal:"member"});
test("exact page content reads preserve original identity and caller authorization without a latest fallback",async()=>{
 const answer={ref,contentVersion:version,page:{name:"child"}},fetcher=vi.fn(async(url,options)=>{expect(url).toBe(`https://test.invalid/v1/pages/build/child/${version}`);expect(options.headers).toMatchObject({Authorization:"Bearer caller","Platform-Tenant":"tenant"});return new Response(JSON.stringify(answer));});vi.stubGlobal("fetch",fetcher);expect(await client().pageContent(ref,version)).toEqual(answer);expect(fetcher).toHaveBeenCalledTimes(1);
});
test("unknown, refused and mismatched original page versions never become another page",async()=>{
 for(const answer of [{ref:{...ref,name:"other"},contentVersion:version,page:{}},{ref,contentVersion:"different",page:{}},{ref,contentVersion:version},null]){const fetcher=vi.fn(async()=>new Response(JSON.stringify(answer)));vi.stubGlobal("fetch",fetcher);await expect(client().pageContent(ref,version)).rejects.toThrow("identity");expect(fetcher).toHaveBeenCalledTimes(1);}
 const denied=vi.fn(async()=>new Response("Denied",{status:403}));vi.stubGlobal("fetch",denied);await expect(client().pageContent(ref,version)).rejects.toThrow("403");expect(denied).toHaveBeenCalledTimes(1);await expect(client().pageContent(ref,"latest")).rejects.toThrow("Invalid");expect(denied).toHaveBeenCalledTimes(1);
});
