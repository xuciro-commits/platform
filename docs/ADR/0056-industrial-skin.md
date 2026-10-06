# ADR-0056 — Appearance = scheme × skin; the Industrial skin

Status: accepted · 2026-10-06

## 1. Why

Our default look is the flat, white, text-only SaaS page. The people who use the
applications built on this platform stand in dim halls next to presses, welding
robots, compressors and AGVs, often in gloves, at arm's length from a panel PC.
They read control pendants (Siemens, ABB, Fanuc), oscilloscope keycaps and IEC
60073 signal lamps all day. For them "minimal" is not clarity: a button has to
*look like a button*, a status has to be a *lamp*, and focus has to be visible from
a metre away.

## 2. Decision

**Appearance is two independent axes on `<html>`:**

* `data-theme` — colour scheme: `light` · `dark` · unset = follow system (unchanged).
* `data-skin` — material: unset = standard · `industrial`.

`useTheme()` owns both, persists them (`platform.appearance`), and the shell's
**Appearance** menu switches them. A switch is one attribute write: no component
re-mounts, no second component library, no runtime theming JS. The industrial skin
is a single stylesheet, `packages/ui/src/themes/industrial.css`, that (a) overrides
the design tokens (colours, radius 2px, 14px type, tabular numerals) and (b) restyles
a handful of primitives through **`data-ui` hooks** the primitives now carry:
`button` (+`data-variant`), `field`, `card`, `tag`, `chrome` (header/rails). Nothing
in the skin depends on Tailwind class names, so components can be refactored freely.

### The language

| Element | Industrial | Why |
|---|---|---|
| Buttons | keycaps: 1px dark edge, top highlight, 2px hard drop; pressed = `translateY(2px)` + inset | a key you can see and feel; ghost/link stay text |
| Primary / danger | signal-blue key / signal-red key, white text | IEC 60073: blue = act, red = stop |
| Fields | recessed wells (inset top shadow), dark glass with warm text in dark mode | a pendant's display |
| Status tags | **filled** lamps, uppercase, white text | read across the room, not by hue tint |
| Header & rails | gunmetal with a 2px safety-amber stripe | the panel frame |
| Focus | 2px amber outline everywhere | gloves, distance, dim light |
| Light vs dark | RAL-7035 panel grey · gunmetal HMI | bright hall · dim workshop |

No fog, dust, grime, textures or glossy gradients: the look comes from geometry,
weight and colour, as a real panel does.

## 3. Performance budget (the real constraint)

The skin uses only colours, borders, 1px/2px **zero-blur** shadows, and one two-stop
gradient on keycaps. Forbidden in this file: `filter`, `backdrop-filter`, blur radii,
`animation`, images, large gradients, `transition` on layout properties. The pressed
state uses `transform` only. Measured cost is a stylesheet of ~6 KB and zero extra DOM,
so a 2-core panel PC renders it like the standard skin.

## 4. Consequences

* Adding a third skin = one CSS file + one entry in `skins`; no component changes.
* Primitives must keep their `data-ui` hooks when refactored (a grep-able contract).
* The catalog's `RetroButton` remains a standalone component for pages that want a
  deliberate keycap regardless of skin; the skin does not depend on it.
* Relative colour syntax is avoided on purpose: panel browsers lag.
