import { expect, test } from "vitest";
import { language, register, t } from ".";

test("English is the source and the fallback; placeholders take values", () => {
  expect(language()).toBe("en");
  register("en", { "Hello {name}": "Hi {name}" });
  expect(t("Hello {name}", { name: "Ana" })).toBe("Hi Ana");
  expect(t("Not translated {n}", { n: 3 })).toBe("Not translated 3");
  expect(t("Keeps {unknown}", {})).toBe("Keeps {unknown}");
});
