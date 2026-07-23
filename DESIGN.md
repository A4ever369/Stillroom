---
version: alpha
name: Stillroom-design-system
description: "Stillroom's visual identity: a near-black product canvas built on #010102, light gray text (#f7f8f8), and a single lavender-blue accent (#5e6ad2) used only on the brand mark, focus rings and primary actions. Hierarchy is carried by a four-step surface ladder plus hairline borders — never by shadow, never by a second chromatic colour. Display type sets tight and negative-tracked; the reading surfaces stay dense and technical. The system is adapted from Linear's published design language (see Provenance) and extended with a light theme derived from its own inverse ladder, because a knowledge search tool is used in daylight all day."

colors:
  primary: "#5e6ad2"
  on-primary: "#ffffff"
  primary-hover: "#828fff"
  primary-focus: "#5e69d1"
  ink: "#f7f8f8"
  ink-muted: "#d0d6e0"
  ink-subtle: "#8a8f98"
  ink-tertiary: "#62666d"
  canvas: "#010102"
  surface-1: "#0f1011"
  surface-2: "#141516"
  surface-3: "#18191a"
  surface-4: "#191a1b"
  hairline: "#23252a"
  hairline-strong: "#34343a"
  hairline-tertiary: "#3e3e44"
  inverse-canvas: "#ffffff"
  inverse-surface-1: "#f5f6f6"
  inverse-surface-2: "#f6f7f7"
  inverse-ink: "#08090a"
  inverse-ink-subtle: "#6b6f76"
  inverse-hairline: "#e3e4e6"
  inverse-hairline-strong: "#d0d2d6"
  semantic-success: "#27a644"
  semantic-attention: "#d4a13a"
  semantic-attention-inverse: "#8a6300"
  semantic-overlay: "#000000"

typography:
  display-xl:
    fontFamily: Stillroom Display
    fontSize: 80px
    fontWeight: 600
    lineHeight: 1.05
    letterSpacing: -3.0px
  display-lg:
    fontFamily: Stillroom Display
    fontSize: 56px
    fontWeight: 600
    lineHeight: 1.10
    letterSpacing: -1.8px
  display-md:
    fontFamily: Stillroom Display
    fontSize: 40px
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: -1.0px
  headline:
    fontFamily: Stillroom Display
    fontSize: 28px
    fontWeight: 600
    lineHeight: 1.20
    letterSpacing: -0.6px
  card-title:
    fontFamily: Stillroom Display
    fontSize: 22px
    fontWeight: 500
    lineHeight: 1.25
    letterSpacing: -0.4px
  subhead:
    fontFamily: Stillroom Display
    fontSize: 20px
    fontWeight: 400
    lineHeight: 1.40
    letterSpacing: -0.2px
  body-lg:
    fontFamily: Stillroom Text
    fontSize: 18px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: -0.1px
  body:
    fontFamily: Stillroom Text
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: -0.05px
  body-sm:
    fontFamily: Stillroom Text
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0
  caption:
    fontFamily: Stillroom Text
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.40
    letterSpacing: 0
  button:
    fontFamily: Stillroom Text
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.20
    letterSpacing: 0
  eyebrow:
    fontFamily: Stillroom Text
    fontSize: 13px
    fontWeight: 500
    lineHeight: 1.30
    letterSpacing: 0.4px
  mono:
    fontFamily: Stillroom Mono
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  xxl: 24px
  pill: 9999px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px
  section: 96px

components:
  top-nav:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    height: 56px
  search-input:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.md}"
    padding: 8px 12px
  search-input-focused:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.md}"
    padding: 8px 12px
  facet-item:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.md}"
    padding: 5px 8px
  facet-item-selected:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.md}"
    padding: 5px 8px
  result-row:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.xs}"
    padding: 16px 0
  result-title:
    textColor: "{colors.ink}"
    typography: "{typography.body-lg}"
  chip:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 2px 8px
  chip-link:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 2px 8px
  knowledge-panel:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 24px
  side-panel:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.xs}"
    padding: 0
  note-block:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.xs}"
    padding: 12px 16px
  empty-state:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.body}"
    rounded: "{rounded.xs}"
    padding: 48px 0
  footer:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-tertiary}"
    typography: "{typography.caption}"
    padding: 24px 16px
---

## Overview

Stillroom's surface is near-black. `{colors.canvas}` (#010102) is the anchor — essentially
pure black with a faint blue tint, never `#000000`. Above it sits a four-step ladder
(`{colors.surface-1}` → `{colors.surface-4}`) that carries every level of hierarchy in the
product, with hairline borders from `{colors.hairline}` upward. Text is light gray
`{colors.ink}` (#f7f8f8), stepping down through muted, subtle and tertiary.

The single chromatic accent is **lavender-blue** `{colors.primary}` (#5e6ad2). It appears on
the brand mark, focus rings, links and the primary action — **never as a fill, never
decoratively**. There is no second brand colour.

Depth comes from surface lift plus hairlines, not from shadow. This matters for us
specifically: the product is a dense reading surface — facts, diffs, search results — and
shadow on a dark ground reads as noise where a one-step lift reads as structure.

**Key characteristics**

- Dark canvas at `{colors.canvas}` #010102; a light theme exists but is the counterpart, not the origin.
- Lavender used scarcely: brand mark, focus, link, primary action.
- Four-step surface ladder carries hierarchy without shadow.
- Display tracking pulls aggressively negative (-3.0px at 80px); body holds at -0.05px.
- Cards at `{rounded.lg}` 12px with 1px hairline borders. Buttons and inputs at `{rounded.md}` 8px.
- Mono is reserved for the things that are literally code: fact IDs, file paths, commands, JSON.

## Provenance and how to use this file

This system is adapted from **Linear's published design language**. The token values are
Linear's; the component set, the light theme and the semantic-colour rules below are ours.
It is an internal style guide for Stillroom's own surfaces — do not present Stillroom UI as
Linear's, and do not copy Linear's wordmark, product screenshots or marketing copy.

Applies to: the `stillroomd` web UI (`cmd/stillroomd/web/`), any published design or product
artifact, and any future landing page. It does **not** apply to terminal output — the CLI
inherits the user's own terminal palette and must stay readable in any of them.

Two deliberate departures from the source system are documented inline below, marked
**[extension]**, so nobody has to guess whether they were mistakes.

## Colors

### Brand & accent

- **Lavender** (`{colors.primary}`) — brand mark, links, focus ring, primary action. Nothing else.
- **Lavender hover** (`{colors.primary-hover}`) — hovered primary action.
- **Lavender focus** (`{colors.primary-focus}`) — 2px focus ring at 50% opacity.

### Surface ladder

| Token | Use in Stillroom |
| --- | --- |
| `{colors.canvas}` | page background, top nav, footer, result rows |
| `{colors.surface-1}` | the knowledge panel (a fact or playbook body), inputs |
| `{colors.surface-2}` | selected facet, hovered row, chips |
| `{colors.surface-3}` | nested surfaces inside a lifted panel |
| `{colors.surface-4}` | deepest lift; rare |
| `{colors.hairline}` | 1px borders and dividers |
| `{colors.hairline-strong}` | input borders, emphasised dividers |

Do not skip levels. A card on canvas is surface-1; a chip inside that card is surface-2.

### Text

`{colors.ink}` headlines and emphasised body · `{colors.ink-muted}` secondary ·
`{colors.ink-subtle}` tertiary (facet labels, meta) · `{colors.ink-tertiary}` quaternary
(footnotes, disabled).

### Semantic

- `{colors.semantic-success}` — confirmed / high-confidence states.
- `{colors.semantic-attention}` — **[extension]** unverified or stale knowledge. The source
  system admits only success green, because it documents a marketing page. Stillroom is an
  operational surface where *"this fact has not been re-observed in 180 days"* must read at a
  glance, and encoding that in the accent hue would violate the scarcity rule instead. It is a
  state colour, not a brand colour: it may tint text and a 1px rule, never fill a surface.
- No other chromatic colour. Ever.

### Light theme **[extension]**

The source documents no light mode because its marketing site ships none. Stillroom's search
UI is read all day, so a light counterpart is required — but it is derived from the system's
own **inverse ladder**, not invented:

| Dark | Light |
| --- | --- |
| `{colors.canvas}` | `{colors.inverse-canvas}` #ffffff |
| `{colors.surface-1}` | `{colors.inverse-surface-1}` #f5f6f6 |
| `{colors.surface-2}` | `{colors.inverse-surface-2}` #f6f7f7 |
| `{colors.ink}` | `{colors.inverse-ink}` #08090a |
| `{colors.ink-subtle}` | `{colors.inverse-ink-subtle}` #6b6f76 |
| `{colors.hairline}` | `{colors.inverse-hairline}` #e3e4e6 |
| `{colors.semantic-attention}` | `{colors.semantic-attention-inverse}` #8a6300 |

Lavender does **not** change between themes — it is legible on both grounds, and a brand
colour that shifts per theme stops being a brand colour.

**Dark is the base declaration, not a forced choice.** The palette is defined dark on
`:root`; the light counterpart applies when the viewer's OS asks for it. A tool people read
all day follows the reader's system preference — the identity is carried by the accent, the
surface ladder and the type, all of which hold in both grounds.

Implement themes at token level: define the palette on `:root`, redefine only the tokens under
`@media (prefers-color-scheme: light)` and again under `:root[data-theme="light"]` /
`:root[data-theme="dark"]` so an explicit toggle wins in both directions, and style every
component through the tokens — never inside the media query.

## Typography

### Families

Custom faces are not distributed and we ship no webfonts — the server must render with zero
network egress, and published artifacts are behind a CSP that blocks font CDNs. Use the
documented substitute stacks:

- **Display / Text** — `-apple-system, BlinkMacSystemFont, "SF Pro Display", system-ui, "Segoe UI", Roboto, "PingFang SC", sans-serif`
- **Mono** — `ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace`

Display and Text are one continuous voice; the family change is silent.

### Hierarchy

| Token | Size | Weight | Tracking | Use in Stillroom |
| --- | --- | --- | --- | --- |
| `{typography.display-xl}` | 80px | 600 | -3.0px | landing hero only |
| `{typography.display-lg}` | 56px | 600 | -1.8px | section openers |
| `{typography.display-md}` | 40px | 600 | -1.0px | sub-sections |
| `{typography.headline}` | 28px | 600 | -0.6px | document title (a fact or playbook) |
| `{typography.card-title}` | 22px | 500 | -0.4px | panel titles |
| `{typography.subhead}` | 20px | 400 | -0.2px | lead paragraphs |
| `{typography.body-lg}` | 18px | 400 | -0.1px | **search result title**, fact body |
| `{typography.body}` | 16px | 400 | -0.05px | document body |
| `{typography.body-sm}` | 14px | 400 | 0 | **default UI size** — nav, facets, snippets |
| `{typography.caption}` | 12px | 400 | 0 | chips, meta, age |
| `{typography.button}` | 14px | 500 | 0 | all button labels |
| `{typography.eyebrow}` | 13px | 500 | +0.4px | section eyebrows and facet headings |
| `{typography.mono}` | 13px | 400 | 0 | fact IDs, paths, commands, JSON |

### Principles

- **Negative tracking on display, positive on eyebrow.** The contrast is what marks an eyebrow
  as taxonomy rather than as small text.
- **Display 600, body 400.** Resist 700+.
- **`{typography.body-sm}` is the default UI size**, not `{typography.body}` — this is a dense
  data surface. `{typography.body}` is for reading a fact, not for chrome.
- **Mono only where the content is literally machine text.** A fact ID is mono; the sentence
  around it is not.

## Layout

- Base unit 4px; tokens `{spacing.xxs}` 4 · `{spacing.xs}` 8 · `{spacing.sm}` 12 ·
  `{spacing.md}` 16 · `{spacing.lg}` 24 · `{spacing.xl}` 32 · `{spacing.xxl}` 48 ·
  `{spacing.section}` 96.
- Max content width ~1280px.
- Search: 260px facet rail + fluid results. Document view: fluid document + 300px side panel.
- Panel interior padding `{spacing.lg}` 24px.
- Reading measure stays near 65–70 characters. Wide content (tables, code, JSON) scrolls
  inside its own `overflow-x: auto` container — the page body never scrolls sideways.
- The dark canvas is the whitespace. Sections separate by lift onto surface-1, not by gaps.

## Elevation

| Level | Treatment | Use |
| --- | --- | --- |
| 0 | no border, no shadow | body type, result rows, nav, footer |
| 1 | `{colors.surface-1}` + 1px `{colors.hairline}` | knowledge panel, inputs |
| 2 | `{colors.surface-2}` + 1px `{colors.hairline-strong}` | selected facet, hovered row, chips |
| 3 | `{colors.surface-3}` | nested surfaces |
| 4 | 2px `{colors.primary-focus}` outline at 50% | focus ring |

No drop shadows on dark. No atmospheric gradients. No spotlight cards.

## Components

Each entry maps to a class in `cmd/stillroomd/web/static/style.css`.

- **`top-nav`** (`.topbar`) — 56px, canvas, 1px hairline bottom. Brand mark left (the only
  place the lavender mark appears), search centre, index meta right.
- **`search-input`** (`.search input`) — surface-1, 1px `{colors.hairline-strong}`,
  `{rounded.md}`, padding 8px 12px. Focus: 2px `{colors.primary-focus}` at 50%.
- **`facet-item`** / **`facet-item-selected`** (`.facet-item`, `.facet-item.on`) — selection is
  a **surface lift to surface-2 with ink text**, not a lavender fill. This is the rule that
  most often gets broken; lavender is not a background.
- **`result-row`** (`.hit`) — canvas, 1px hairline bottom rule, padding 16px 0. Title at
  `{typography.body-lg}` weight 500, snippet at `{typography.body-sm}` in ink-subtle.
- **`chip`** (`.chip`) — surface-2, ink-muted, `{rounded.pill}`, 2px 8px, `{typography.caption}`.
  Confidence and kind chips tint the **text** only; the fill stays surface-2.
- **`knowledge-panel`** (`.doc-body`) — surface-1, 1px hairline, `{rounded.lg}`, padding 24px.
  The fact or playbook body itself. This is the protagonist of the document view, the way a
  product screenshot is the protagonist of the source system's marketing page.
- **`note-block`** (`.doc-note`) — the privacy/provenance note. Canvas, 3px left rule in
  `{colors.hairline-strong}`, ink-subtle. Never boxed, never coloured — it is a statement of
  fact, not an alert.
- **`empty-state`** (`.empty`) — canvas, centred, ink-subtle. Says what the empty answer means.
- **`footer`** (`.footer`) — canvas, 1px hairline top, ink-tertiary caption.

## Do's and Don'ts

### Do

- Anchor on `{colors.canvas}` #010102 — the faint blue tint is intentional.
- Use lavender only for: brand mark, link, focus ring, primary action.
- Carry hierarchy with the surface ladder; don't skip levels.
- Track display type aggressively negative; give eyebrows positive tracking.
- Keep `{rounded.md}` 8px on buttons and inputs, `{rounded.lg}` 12px on panels.
- Let the fact body be the largest, most-lifted thing on a document page.

### Don't

- Don't use `#000000` as the canvas.
- Don't fill a surface with lavender, or use it to indicate selection.
- Don't introduce a second chromatic accent. `{colors.semantic-attention}` is a state colour
  and may tint text and rules only.
- Don't add drop shadows on the dark canvas, atmospheric gradients or spotlight cards.
- Don't pill-round buttons (pills are for chips and status only).
- Don't set chrome in mono, or set fact IDs in the body face.
- Don't link a webfont — there is no network in the deployment target.

## Responsive

| Name | Width | Key change |
| --- | --- | --- |
| Desktop | 1280px | facet rail + results |
| Tablet | 1024px | rail narrows |
| Mobile-Lg | 900px | rail moves below results; nav meta hidden |
| Mobile | 480px | single column; display-xl scales toward display-md |

Tap targets ≥40px on touch. Wide blocks scroll inside their own container.

## Known gaps

- Error and validation styling is not specified — the current surfaces are read-only.
- The landing page (display-xl / display-lg) has no implementation yet; the tokens are
  reserved for it.
- Mono weight variation is unspecified; use 400 everywhere for now.
