// A reader's arrangement of a drawing (ADR-0092): kept per drawing, bounded,
// and never fatal when storage is unavailable.
import { expect, test } from "vitest";
import { readCanvasPositions, writeCanvasPositions } from "./store";

test("an arrangement round-trips for its own drawing only", () => {
  localStorage.clear();
  writeCanvasPositions("one", { a: { x: 12, y: 34 } });
  expect(readCanvasPositions("one")).toEqual({ a: { x: 12, y: 34 } });
  expect(readCanvasPositions("two")).toBeUndefined();
});

test("junk in storage reads as no arrangement", () => {
  localStorage.setItem("canvas:junk", "\"a string\"");
  expect(readCanvasPositions("junk")).toBeUndefined();
  localStorage.setItem("canvas:junk", "[1,2,3]");
  expect(readCanvasPositions("junk")).toBeUndefined();
  localStorage.removeItem("canvas:junk");
});

test("the oldest drawings expire so storage never grows without limit", () => {
  localStorage.clear();
  for (let i = 0; i < 70; i++) writeCanvasPositions(`key-${i}`, { n: { x: i, y: 0 } });
  expect(readCanvasPositions("key-0")).toBeUndefined(); // evicted
  expect(readCanvasPositions("key-69")).toEqual({ n: { x: 69, y: 0 } }); // newest kept
  // Rewriting a key moves it to the front of the line.
  writeCanvasPositions("key-20", { n: { x: 20, y: 5 } });
  expect(readCanvasPositions("key-20")).toEqual({ n: { x: 20, y: 5 } });
});
