import { expect, test } from "vitest";
import { codeChallenge } from "./oidc";

test("S256 code challenge matches RFC 7636 appendix B", async () => {
  expect(await codeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")).toBe("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
});
