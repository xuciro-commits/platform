// Relation node classes (ADR-0090 D1): the class is a declaration of what a
// thing on a relationship drawing *is*; the table is the single place its glyph,
// tone and caption word live, and a node's own words always win over it.
import { describe, expect, it } from "vitest";
import { relationNodeCaption, relationNodeClassOf, relationNodeClasses } from "./model";

describe("a relation node's class is what it draws under", () => {
  it("keeps the standing vocabulary in one table — every entry has a title and an icon name", () => {
    const expected = ["connection", "source", "dataset", "pipeline", "object", "writeback",
      "app", "page", "entity", "query", "function", "linktype", "propertytype", "action", "operation",
      "record", "flow", "run", "effect", "property", "relation"];
    expect(Object.keys(relationNodeClasses).sort()).toEqual([...expected].sort());
    for (const [name, decl] of Object.entries(relationNodeClasses)) {
      expect(decl.title, name).toBeTruthy();
      expect(decl.icon, name).toMatch(/^[a-z0-9-]+$/); // an IconName, never a JSX element
      expect(decl.tone, name).toMatch(/^(neutral|info|success|warning|danger)?$/);
    }
  });

  it("reads the class a node names, and nothing when it names none", () => {
    expect(relationNodeClassOf({ class: "dataset" })).toBe("dataset");
    expect(relationNodeClassOf({})).toBeUndefined();
  });

  it("falls back from an absent caption to the class title — translated where it renders", () => {
    expect(relationNodeCaption({ class: "dataset" })).toBe("Dataset");
    expect(relationNodeCaption({ caption: "Dataset · v3", class: "dataset" })).toBe("Dataset · v3"); // the node wins
    expect(relationNodeCaption({})).toBeUndefined(); // no class, no caption: nothing to say
    expect(relationNodeCaption({ class: "never-heard-of" })).toBeUndefined();
  });

  it("the palette and the class table agree on every icon name the table offers", async () => {
    const { iconGlyphs } = await import("../../components/IconPicker");
    for (const [name, decl] of Object.entries(relationNodeClasses)) expect(iconGlyphs[decl.icon], `${name} → ${decl.icon}`).toBeTruthy();
  });
});
