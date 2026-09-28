---
version: alpha
name: Cal.com-design-analysis
description: A clean, calendar-software-first interface anchored on white canvas with black primary CTAs and custom Cal Sans display typography. The system reads as friendly modern SaaS — generous whitespace, soft-rounded cards (~12px), product UI fragments shown directly inside cards, and a dark navy footer that visually closes long-scroll pages. Brand voltage comes from the Cal Sans display headline (a custom geometric face) and from product UI artifacts shown in-card rather than from accent colors.

colors:
  primary: "#111111"
  primary-active: "#242424"
  primary-disabled: "#e5e7eb"
  ink: "#111111"
  body: "#374151"
  muted: "#6b7280"
  muted-soft: "#898989"
  hairline: "#e5e7eb"
  hairline-soft: "#f3f4f6"
  canvas: "#ffffff"
  surface-soft: "#f8f9fa"
  surface-card: "#f5f5f5"
  surface-strong: "#e5e7eb"
  surface-dark: "#101010"
  surface-dark-elevated: "#1a1a1a"
  on-primary: "#ffffff"
  on-dark: "#ffffff"
  on-dark-soft: "#a1a1aa"
  brand-accent: "#3b82f6"
  success: "#10b981"
  warning: "#f59e0b"
  error: "#ef4444"
  badge-orange: "#fb923c"
  badge-pink: "#ec4899"
  badge-violet: "#8b5cf6"
  badge-emerald: "#34d399"

typography:
  display-xl:
    fontFamily: "Cal Sans, Inter, sans-serif"
    fontSize: 64px
    fontWeight: 600
    lineHeight: 1.05
    letterSpacing: -2px
  display-lg:
    fontFamily: "Cal Sans, Inter, sans-serif"
    fontSize: 48px
    fontWeight: 600
    lineHeight: 1.1
    letterSpacing: -1.5px
  display-md:
    fontFamily: "Cal Sans, Inter, sans-serif"
    fontSize: 36px
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: -1px
  display-sm:
    fontFamily: "Cal Sans, Inter, sans-serif"
    fontSize: 28px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: -0.5px
  title-lg:
    fontFamily: "Inter, sans-serif"
    fontSize: 22px
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: -0.3px
  title-md:
    fontFamily: "Inter, sans-serif"
    fontSize: 18px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: 0
  title-sm:
    fontFamily: "Inter, sans-serif"
    fontSize: 16px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: 0
  body-md:
    fontFamily: "Inter, sans-serif"
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
  body-sm:
    fontFamily: "Inter, sans-serif"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
  caption:
    fontFamily: "Inter, sans-serif"
    fontSize: 13px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0
  code:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
  button:
    fontFamily: "Inter, sans-serif"
    fontSize: 14px
    fontWeight: 600
    lineHeight: 1
    letterSpacing: 0
  nav-link:
    fontFamily: "Inter, sans-serif"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
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
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 12px 20px
    height: 40px
  button-primary-active:
    backgroundColor: "{colors.primary-active}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.md}"
  button-primary-disabled:
    backgroundColor: "{colors.primary-disabled}"
    textColor: "{colors.muted}"
    rounded: "{rounded.md}"
  button-secondary:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 12px 20px
    height: 40px
  button-icon-circular:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.full}"
    size: 36px
  button-text-link:
    backgroundColor: transparent
    textColor: "{colors.ink}"
    typography: "{typography.button}"
  text-link:
    backgroundColor: transparent
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
  top-nav:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.nav-link}"
    height: 64px
  nav-pill-group:
    backgroundColor: "{colors.surface-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.nav-link}"
    rounded: "{rounded.pill}"
    padding: 6px
  hero-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-xl}"
    padding: 96px
  hero-app-mockup-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
  feature-card:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.title-md}"
    rounded: "{rounded.lg}"
    padding: 32px
  feature-icon-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.title-sm}"
    rounded: "{rounded.lg}"
    padding: 24px
  product-mockup-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    padding: 24px
  testimonial-card:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.lg}"
    padding: 24px
  pricing-tier-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.title-lg}"
    rounded: "{rounded.lg}"
    padding: 32px
  pricing-tier-card-featured:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark}"
    typography: "{typography.title-lg}"
    rounded: "{rounded.lg}"
    padding: 32px
  text-input:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: 10px 14px
    height: 40px
  text-input-focused:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
  category-tab:
    backgroundColor: transparent
    textColor: "{colors.muted}"
    typography: "{typography.nav-link}"
    padding: 8px 14px
    rounded: "{rounded.md}"
  category-tab-active:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.nav-link}"
    rounded: "{rounded.md}"
  avatar-circle:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    rounded: "{rounded.full}"
    size: 36px
  badge-pill:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 4px 12px
  rating-stars:
    backgroundColor: transparent
    textColor: "{colors.badge-orange}"
    typography: "{typography.caption}"
  cta-band-light:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
    typography: "{typography.display-sm}"
    rounded: "{rounded.lg}"
    padding: 48px
  footer:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.on-dark-soft}"
    typography: "{typography.body-sm}"
    padding: 64px
---

## Overview

Cal.com's marketing surface is a clean, friendly modern-SaaS interface — white canvas (`{colors.canvas}` — #ffffff) with black primary CTAs (`{colors.primary}` — #111111), custom **Cal Sans** display typography, and `{colors.surface-card}` (#f5f5f5) light-gray cards holding product UI fragments. The system reads as confidently engineered without trying to impress — every band has clear hierarchy, generous whitespace, and a single primary action.

Type voice splits cleanly into two roles: **Cal Sans** (the brand's custom geometric display face — used for h1, h2, h3, and hero headlines) and **Inter** (used for everything else — body, buttons, nav, captions). Cal Sans uses weight 600 with negative letter-spacing (-0.5px to -2px depending on size) — it feels modern, slightly condensed, distinctly Cal.com.

Component voltage comes from **product UI fragments shown directly inside cards** — calendar widgets, scheduling forms, automation diagrams, integration tiles. Cal.com doesn't paint marketing illustrations of the product; it shows the actual product chrome at small scale embedded in the marketing flow.

The footer flips to `{colors.surface-dark}` (#101010) — a deep near-black that visually closes every long-scroll page. The footer is the only dark surface in the system; everything above stays white-with-light-gray-cards.

**Key Characteristics:**
- White canvas with black primary CTA (`{colors.primary}` — #111111). Buttons are `{rounded.md}` (8px) with confident weight-600 labels. Standard friendly-SaaS button.
- Custom `Cal Sans` display typeface for headlines (substituted with Inter weight 600 here). Negative letter-spacing on display sizes — geometric, precise, slightly condensed.
- Light-gray card surfaces (`{colors.surface-card}` — #f5f5f5) for feature cards, testimonials, and pricing tiers (non-featured). The featured pricing tier flips to `{colors.surface-dark}` (the only dark card on light pages).
- Product UI fragments embedded directly in cards — Cal.com shows real schedule pickers, calendar widgets, integration grids inside its marketing cards. Brand voltage from real product chrome at small scale.
- Nav-pill-group (`{component.nav-pill-group}`) — a small pill-radius wrapper around grouped nav segments (e.g., the sub-nav switcher between product views). The pill wrapper is one of the system's signature interactive components.
- Avatars are circular (`{rounded.full}`), 36px diameter, used in testimonial rows and team-listing surfaces.
- Footer is dark navy (`{colors.surface-dark}` — #101010) with light text (`{colors.on-dark-soft}` — #a1a1aa). The dark footer closes every page even though the body above is white.
- Spacing rhythm is `{spacing.section}` (96px) between major bands — tight enough to feel modern-SaaS but generous enough to breathe.
- Border radius is hierarchical: `{rounded.md}` (8px) for buttons + inputs, `{rounded.lg}` (12px) for content cards, `{rounded.xl}` (16px) for the hero app-mockup container, `{rounded.pill}` for nav-pill-group + badges, `{rounded.full}` for avatars + icon buttons.

## Colors

### Brand & Accent
- **Primary** (`{colors.primary}` — #111111): The dominant action color. All primary CTAs, h1/h2 display type. Press state shifts to `{colors.primary-active}` (#242424).
- **Brand Accent** (`{colors.brand-accent}` — #3b82f6): Used sparely on inline links and on a small badge / "Customer story" highlight. Cal.com is a near-monochrome brand — the blue appears rarely.
- **Badge Pastels** — A small pastel set for category badges and avatar fills: `{colors.badge-orange}` (#fb923c), `{colors.badge-pink}` (#ec4899), `{colors.badge-violet}` (#8b5cf6), `{colors.badge-emerald}` (#34d399). These appear on tag pills and small accent moments inside product UI fragments — never on hero CTAs.

### Surface
- **Canvas** (`{colors.canvas}` — #ffffff): The default page floor.
- **Surface Soft** (`{colors.surface-soft}` — #f8f9fa): Nav-pill-group background, very-soft section dividers.
- **Surface Card** (`{colors.surface-card}` — #f5f5f5): Feature cards, testimonial cards, badge pills, default avatar fills.
- **Surface Strong** (`{colors.surface-strong}` — #e5e7eb): Hairline border alternative; disabled button background.
- **Surface Dark** (`{colors.surface-dark}` — #101010): The footer background — the only dark surface on every page. Also used for the featured pricing tier card.
- **Surface Dark Elevated** (`{colors.surface-dark-elevated}` — #1a1a1a): Used for nested cards inside the dark footer or featured pricing card.
- **Hairline** (`{colors.hairline}` — #e5e7eb): The 1px border tone on light surfaces. Used on input borders, table dividers, content card outlines (sometimes).
- **Hairline Soft** (`{colors.hairline-soft}` — #f3f4f6): A barely-visible divider used between sections that share the white canvas.

### Text
- **Ink** (`{colors.ink}` — #111111): All headlines and primary text.
- **Body** (`{colors.body}` — #374151): Default running-text color.
- **Muted** (`{colors.muted}` — #6b7280): Secondary text — sub-headings, breadcrumbs, footer body.
- **Muted Soft** (`{colors.muted-soft}` — #898989): Tertiary text — captions, fine-print, copyright lines.
- **On Primary / On Dark** (`{colors.on-primary}` / `{colors.on-dark}` — #ffffff): Text on primary buttons and dark footer.
- **On Dark Soft** (`{colors.on-dark-soft}` — #a1a1aa): Footer body text — slightly muted white for the link rows.

### Semantic
- **Success** (`{colors.success}` — #10b981): Confirmation states, success badges in product UI.
- **Warning** (`{colors.warning}` — #f59e0b): Warning callouts.
- **Error** (`{colors.error}` — #ef4444): Validation errors.

## Typography

### Font Family
The system runs **Cal Sans** for display + brand wordmark and **Inter** for everything else. Cal Sans is Cal.com's custom geometric display typeface — slightly condensed, weight 600, negative letter-spacing. Inter handles body, buttons, navigation, captions, and tabular code blocks. The fallback stack walks `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif` for both families.

The split is functional:
- Cal Sans (display, 600 weight, -0.5 to -2px tracking) — h1, h2, h3
- Inter (body + UI, 400-600 weight, 0 letter-spacing) — paragraphs, labels, buttons, nav

### Hierarchy

| Token | Size | Weight | Line Height | Letter Spacing | Use |
|---|---|---|---|---|---|
| `{typography.display-xl}` | 64px | 600 | 1.05 | -2px | Homepage h1 ("The better way to schedule your meetings") — Cal Sans |
| `{typography.display-lg}` | 48px | 600 | 1.1 | -1.5px | Section heads ("Your all-purpose scheduling app") — Cal Sans |
| `{typography.display-md}` | 36px | 600 | 1.15 | -1px | Sub-section heads, card titles — Cal Sans |
| `{typography.display-sm}` | 28px | 600 | 1.2 | -0.5px | CTA-band heads, pricing tier prices — Cal Sans |
| `{typography.title-lg}` | 22px | 600 | 1.3 | -0.3px | Pricing plan names — Inter |
| `{typography.title-md}` | 18px | 600 | 1.4 | 0 | Feature card titles, intro paragraphs |
| `{typography.title-sm}` | 16px | 600 | 1.4 | 0 | Small card titles, list labels |
| `{typography.body-md}` | 16px | 400 | 1.5 | 0 | Default running-text |
| `{typography.body-sm}` | 14px | 400 | 1.5 | 0 | Footer body, fine-print |
| `{typography.caption}` | 13px | 500 | 1.4 | 0 | Badge labels, captions |
| `{typography.code}` | 14px | 400 | 1.5 | 0 | Code snippets, API examples — JetBrains Mono |
| `{typography.button}` | 14px | 600 | 1.0 | 0 | Standard button labels |
| `{typography.nav-link}` | 14px | 500 | 1.4 | 0 | Top-nav menu items |

### Principles
Cal Sans is the brand voice — every display headline uses it. Inter handles the supporting type. The boundary is strict: never put body copy in Cal Sans, never put a display headline in Inter. Cal Sans without negative letter-spacing reads as off-brand — the -0.5 to -2px tracking is part of the voice.

Display weight stays at 600 across all sizes — never 700, never 500. The middle weight is what makes Cal Sans feel modern and confident without becoming bombastic.

### Note on Font Substitutes
If Cal Sans is unavailable, **Inter** at weight 600 with -0.04em letter-spacing is a usable approximation. The geometric character of Cal Sans differs from Inter's humanist forms, but the substitution preserves the weight + tracking signature. **Manrope** at weight 700 is another close alternative.

## Layout

### Spacing System
- **Base unit:** 4px.
- **Tokens:** `{spacing.xxs}` 4px · `{spacing.xs}` 8px · `{spacing.sm}` 12px · `{spacing.md}` 16px · `{spacing.lg}` 24px · `{spacing.xl}` 32px · `{spacing.xxl}` 48px · `{spacing.section}` 96px.
- **Section padding:** `{spacing.section}` (96px) — the universal vertical rhythm between editorial bands.
- **Card internal padding:** `{spacing.xl}` (32px) for feature cards and pricing tier cards; `{spacing.lg}` (24px) for testimonial and product-mockup cards.
- **Gutters:** `{spacing.lg}` (24px) between cards in 3-up grids; `{spacing.md}` (16px) inside footer columns.

### Grid & Container
- **Max content width:** ~1200px centered on marketing pages.
- **Editorial body:** Single 12-column grid; hero band often uses 7/5 split (h1 left, app mockup card right).
- **Feature card grids:** 3-up at desktop, 2-up at tablet, 1-up at mobile.
- **Pricing grid:** 4-up at desktop, 2-up at tablet, 1-up at mobile.
- **Footer:** 4-column link list at desktop, wrapping to 2-up at tablet, 1-up at mobile.

### Whitespace Philosophy
Cal.com uses generous but not excessive whitespace — section padding sits at 96px (modern-SaaS standard), and card internal padding stays at 32px. The rhythm is calibrated for fast scanning: every band has a single h1 + h2 + supporting cards, never densely packed lists. The result reads as confident-not-shouting.

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| Flat | No shadow, no border | Body sections, top nav, hero bands |
| Soft hairline | 1px `{colors.hairline}` border | Inputs, table dividers, occasionally on cards |
| Card surface | `{colors.surface-card}` background — no shadow | Feature cards, testimonials |
| Subtle drop shadow | Faint shadow at low alpha | Pricing tier cards, hover-elevated states (the system uses `0 1px 2px rgba(0,0,0,0.05)` and `0 4px 12px rgba(0,0,0,0.08)`) |
| Featured tier | `{colors.surface-dark}` background, no shadow needed | The featured pricing tier inverts to dark surface — color contrast does the elevation work |

The elevation philosophy is **soft and modern** — small drop shadows on elevated cards, color-block contrast for emphasis. No heavy shadows, no neumorphism, no glassmorphism.

### Decorative Depth
- Calendar widgets and product UI fragments embedded inside marketing cards carry their own internal shadows from the product UI itself — these are not system tokens, they're product chrome shown as content.
- Avatar circles in testimonial sections sometimes carry pastel fill colors (`{colors.badge-orange}`, `{colors.badge-pink}`, etc.) — adds a small chromatic flourish without breaking the monochrome brand voice.

## Shapes

### Border Radius Scale

| Token | Value | Use |
|---|---|---|
| `{rounded.xs}` | 4px | Almost no use — reserved for badge accents |
| `{rounded.sm}` | 6px | Small inline buttons, dropdown items |
| `{rounded.md}` | 8px | Standard CTA buttons, text inputs, category tabs |
| `{rounded.lg}` | 12px | Content cards (feature cards, testimonial cards, pricing tier cards) |
| `{rounded.xl}` | 16px | Hero app-mockup card (a slightly larger radius for the marquee component) |
| `{rounded.pill}` | 9999px | Nav-pill-group, badge pills |
| `{rounded.full}` | 9999px / 50% | Avatars, icon buttons |

### Photography Geometry
Avatar photos use `{rounded.full}` (perfect circles) at 36px or 40px. Product UI fragments inside marketing cards retain their native chrome (which often has its own internal radii — e.g., calendar grid cells, button rows). Hero illustration zones use 16:9 or 4:3 ratios with `{rounded.xl}` corners.

## Components

### Top Navigation

**`top-nav`** — White nav bar pinned to the top of every page. 64px tall, `{colors.canvas}` background. Carries the Cal.com wordmark + logo at left (the lowercase "Cal.com" with the brand circle), primary horizontal menu (Product, Solutions, Resources, Pricing, Enterprise) center, right-side cluster with "Sign in" text-link, "Sign up free" `{component.button-primary}`, and a sometimes-visible language selector. Menu items in `{typography.nav-link}` (Inter 14px / 500).

**`nav-pill-group`** — A small pill-radius wrapper around 2-3 sub-nav segments (e.g., the product-mode switcher between "Personal" / "Teams" / "Enterprise"). Background `{colors.surface-soft}` with internal padding 6px, rounded `{rounded.pill}`. Active segment renders as a white-canvas pill with a subtle drop shadow inside the wrapper. The pill-in-pill treatment is one of Cal.com's signature interactive components.

### Buttons

**`button-primary`** — The signature primary CTA. Background `{colors.primary}` (#111111), text `{colors.on-primary}`, type `{typography.button}` (Inter 14px / 600), padding 12px × 20px, height 40px, rounded `{rounded.md}` (8px). Active state `button-primary-active` shifts to `{colors.primary-active}` (#242424).

**`button-secondary`** — White button with hairline outline. Background `{colors.canvas}`, text `{colors.ink}`, 1px hairline border, same padding + height + radius as primary.

**`button-icon-circular`** — 36 × 36px circular icon button. Background `{colors.canvas}`, hairline border, ink-color icon. Used for share, "view more", carousel arrows.

**`button-text-link`** — Inline text button, no background. Used for "Sign in" in the top nav and inline CTA links inside cards.

**`text-link`** — Inline body links in `{colors.ink}` (the brand keeps inline links monochrome). Underlined on hover (not documented per the no-hover policy, but mentioned for context).

### Cards & Containers

**`hero-band`** — White-canvas hero with a 7-5 grid: h1 + sub-headline + button row on the left, `{component.hero-app-mockup-card}` on the right. Vertical padding `{spacing.section}` (96px).

**`hero-app-mockup-card`** — A larger product-UI mockup card showing the actual Cal.com booking widget with calendar grid, time slots, and a primary "Confirm" button inside. Background `{colors.canvas}`, 1px hairline border, rounded `{rounded.xl}` (16px), subtle drop shadow. Used as the hero's right-side artifact.

**`feature-card`** — Used in 3-up feature grids ("With us, appointment scheduling is easy"). Background `{colors.surface-card}` (#f5f5f5), rounded `{rounded.lg}` (12px), internal padding `{spacing.xl}` (32px). Carries a small icon at top, an `{typography.title-md}` headline, and a body description in `{typography.body-md}`.

**`feature-icon-card`** — A simpler card variant used in 4-up feature grids on lower-density bands. Background `{colors.canvas}` with hairline border, rounded `{rounded.lg}`, padding `{spacing.lg}` (24px). Carries a small icon, `{typography.title-sm}` title, short description.

**`product-mockup-card`** — A card showing actual Cal.com product UI fragments (workflow editor, calendar grid, integration grid, automation flow). Background `{colors.canvas}`, rounded `{rounded.lg}`, padding `{spacing.lg}` (24px). The product UI inside has its own internal chrome — these cards display the product, they don't decorate around it.

**`testimonial-card`** — Used in customer-quote grids. Background `{colors.surface-card}`, rounded `{rounded.lg}`, padding `{spacing.lg}` (24px). Top row carries a `{component.avatar-circle}` + name + role; below sits the testimonial quote in `{typography.body-md}`.

**`pricing-tier-card`** — Standard tier card. Background `{colors.canvas}`, rounded `{rounded.lg}`, padding `{spacing.xl}` (32px). Carries the plan name in `{typography.title-lg}`, price in `{typography.display-sm}`, feature checklist in `{typography.body-md}`, and a `{component.button-primary}` at the bottom.

**`pricing-tier-card-featured`** — The featured tier (typically "Teams"). Background flips to `{colors.surface-dark}` (#101010), text inverts to `{colors.on-dark}`. The dark surface IS the featured-tier signal — no accent border, no badge, no scale shift.

### Inputs & Forms

**`text-input`** — Standard text input. Background `{colors.canvas}`, text `{colors.ink}`, type `{typography.body-md}`, rounded `{rounded.md}` (8px), padding 10px × 14px, height 40px. 1px hairline border in `{colors.hairline}`.

**`text-input-focused`** — Focus state. Border thickens or shifts to `{colors.ink}` for emphasis.

### Tags / Badges

**`badge-pill`** — Small pill label used for category tags ("Product", "Article", "New") and pastel-fill avatar substitutes. Background `{colors.surface-card}` or one of the badge pastels (`{colors.badge-orange}`, `{colors.badge-pink}`, etc.), text `{colors.ink}`, type `{typography.caption}` (13px / 500), rounded `{rounded.pill}`, padding 4px × 12px.

**`avatar-circle`** — 36px diameter, rounded `{rounded.full}`. Either holds a photo or a pastel fill with initials in `{typography.caption}`.

**`rating-stars`** — Inline star rating in `{colors.badge-orange}` (#fb923c). Used near testimonial avatars to display a 5-star satisfaction score.

### Tab / Filter

**`category-tab`** + **`category-tab-active`** — Used inside the nav-pill-group. Inactive: transparent background, `{colors.muted}` text. Active: `{colors.canvas}` background, `{colors.ink}` text, subtle drop shadow inside the pill-group wrapper. Padding 8px × 14px, rounded `{rounded.md}`.

### CTA / Footer

**`cta-band-light`** — A pre-footer "Smarter, simpler scheduling" CTA card. Background `{colors.surface-card}`, rounded `{rounded.lg}`, padding `{spacing.xxl}` (48px). Carries an h2 in `{typography.display-sm}`, a sub-line, and a `{component.button-primary}` centered.

**`footer`** — Dark navy footer that closes every page. Background `{colors.surface-dark}` (#101010), text `{colors.on-dark-soft}`. 4-column link list at desktop covering Product / Solutions / Company / Resources. Vertical padding 64px. The Cal.com wordmark sits at the top-left in `{colors.on-dark}`. The footer is the only dark surface on every page — the deliberate inversion visually closes the page.

## Do's and Don'ts

### Do
- Reserve `{colors.primary}` (#111111) for primary CTAs and h1/h2 type. Cal.com's button is near-black, not blue.
- Use Cal Sans for every display headline. Pair with Inter body. Never blur the boundary.
- Apply negative letter-spacing on display sizes (-0.5 to -2px). Cal Sans without it reads as off-brand.
- Use `{component.feature-card}` (light gray) and `{component.product-mockup-card}` (white with chrome) deliberately — the gray cards signal "abstract feature claim", white cards signal "look at the actual product".
- Embed real product UI fragments inside marketing cards. Don't paint marketing illustrations of the product when you can show the product itself.
- Keep avatar circles at 36px, perfect circles, sometimes with pastel fills. Avatars are the only place where badge pastels appear.
- Use `{component.nav-pill-group}` for grouped sub-nav segments. The pill-in-pill treatment is signature.
- End every page with the dark footer. The light-to-dark transition is part of the editorial rhythm.

### Don't
- Don't use accent colors (`{colors.brand-accent}`, badge pastels) on primary CTAs. The system is monochrome at the action layer.
- Don't bold display weight beyond 600. Cal Sans at 700 reads as bombastic.
- Don't use rounded radius beyond `{rounded.xl}` (16px) on cards. Larger radii read as consumer-app, not professional booking software.
- Don't put dark surface cards anywhere except the footer and the featured pricing tier. The dark surface is a deliberate, scarce signal.
- Don't repeat the same surface mode in two consecutive bands. Cal.com's pacing alternates white → light-gray → white → product-mockup-card → white → dark-footer.
- Don't add hover state styling beyond what the system already encodes — primary darkens on press; nothing else changes.

## Responsive Behavior

### Breakpoints

| Name | Width | Key Changes |
|---|---|---|
| Mobile | < 768px | Hamburger nav; hero h1 64→32px; hero-app-mockup-card stacks below content; feature grids 1-up; pricing 1-up; footer 4 cols → 1 |
| Tablet | 768–1024px | Top nav stays horizontal but tightens; nav-pill-group wraps; feature cards 2-up; pricing 2-up |
| Desktop | 1024–1440px | Full top-nav with all menu items; 3-up feature cards; 4-up pricing tiers |
| Wide | > 1440px | Same as desktop with more outer breathing room; max content width caps at 1200px |

### Touch Targets
- `{component.button-primary}` at minimum 40 × 40px.
- `{component.button-icon-circular}` at exactly 36 × 36 — slightly under WCAG's 44 × 44 but the centered icon and full-circle silhouette compensate.
- `{component.text-input}` height is 40px.
- `{component.category-tab}` rendered inside nav-pill-group has 8 × 14 padding; effective tap area meets 44px+ with the surrounding pill.

### Collapsing Strategy
- Top nav collapses to hamburger at < 768px; menu opens as a full-screen sheet.
- Hero band's 7-5 grid collapses to single-column on mobile — h1 + sub-head + buttons first, then the app-mockup card below.
- Feature grids reduce columns rather than scaling cards down.
- Pricing tier cards collapse 4 → 2 → 1; featured-tier dark surface stays visually distinct at every breakpoint.
- Nav-pill-group wraps to multi-row on tablet if the segments don't fit horizontally.
- Avatar + testimonial card layouts stay grid-aligned at every breakpoint.

### Image Behavior
- Product UI fragments inside cards retain native aspect ratios; the cards themselves resize.
- Avatar photos crop to circles at every breakpoint.
- Hero app-mockup card scales proportionally on mobile — the calendar grid stays legible.

## Iteration Guide

1. Focus on ONE component at a time. Reference its YAML key directly (`{component.feature-card}`, `{component.pricing-tier-card-featured}`).
2. Variants of an existing component (`-active`, `-disabled`, `-focused`) live as separate entries in `components:`.
3. Use `{token.refs}` everywhere — never inline hex.
4. Never document hover. Default and Active/Pressed states only.
5. Display headlines stay Cal Sans 600 with negative letter-spacing. Body stays Inter 400. The trinity does not blur.
6. The dark footer is the only dark surface on most pages. Don't add other dark cards casually.
7. When in doubt about emphasis: bigger Cal Sans before bolder Cal Sans.

## Known Gaps

- The dembrandt frequency analyzer captured `Buttons: 0 variants` — Cal.com renders most CTAs as styled `<a>` link elements rather than `<button>` tags, which dembrandt's button selector doesn't capture. Button styles are documented from screenshot ground-truth + standard Cal Sans / Inter baselines.
- Cal Sans is licensed to Cal.com and not available as a public web font; substitutes are documented in the typography section.
- The badge pastel set (orange / pink / violet / emerald) is documented from observed avatar fill colors; exact hex values may shift seasonally.
- Animation and transition timings (calendar slot picker, schedule confirmation, integration grid hover-reveal) are not in scope.
- Form validation states beyond `{component.text-input-focused}` are not extracted — error / success states would need a sign-up or booking flow to confirm.
- The actual booking widget surface (cal.com/{username}) is the product, not a marketing surface; its spec is out of scope.
- Avatar photos in testimonial sections sometimes carry pastel circular fills with initials instead of photographs; both treatments coexist on the same page.

## App adaptation

Everything above is the reference system. This section says how the IT Support app applies it. Where the two disagree, this section wins, because the app must pass WCAG 2.2 AA and render four languages on phones.

### Tokens
- `frontend/src/lib/styles/tokens.css` mirrors the YAML keys as CSS variables: `--color-<key>`, `--font-<typography key>`, `--radius-<rounded key>`, `--space-<spacing key>`. Components use only these variables.
- **Font:** Cal Sans and Inter are not used. The self-hosted Noto Sans family (Latin, Thai, Myanmar, SC) sets all text, so the four languages share one look. Display headlines use Noto Sans 600 with `--tracking-display` (-0.5px). Never use Cal.com's font, logo or name.
- App additions:

| Token | Value | Use |
|---|---|---|
| `--color-error-strong` | #dc2626 | Error text, danger button, urgent badge. `{colors.error}` is 3.8:1 on white, below AA for text; keep it for borders and icons. |
| `--color-{accent,success,warning,error,violet}-tint` | base color mixed with canvas (10–15%) | Alert, badge and guest-reply backgrounds |
| `--color-focus`, `--color-focus-ring` | `{colors.brand-accent}`, 25% alpha | Focus outline; input focus border plus 4px ring |
| `--font-label` | 14px / 600 | Form labels, definition terms, reply author |
| `--size-control` | 44px | Minimum height of buttons, inputs and selects (reference says 40px; guests tap on phones) |

### Per-language rules
- Body line height: Burmese (`my`) 1.8, Thai and Chinese 1.6. Headings: Burmese 1.6, Thai and Chinese 1.4.
- No negative letter-spacing in Burmese, Thai or Chinese (`--tracking-display` is 0).
- The root layout sets `<html lang>` to the active locale, so these `:lang()` rules apply.

### Surfaces
- Page floor is `{colors.canvas}`.

### Page tools (no top nav)
- No top nav bar and no brand link; this overrides `top-nav` above. The first row of `main` on every page is a right-aligned tools row, `{spacing.md}` above the content. It is not sticky and scrolls away with the page.
- The language picker is always there (FR-I2): a pill button styled like the account button (`{rounded.pill}`, canvas, hairline border, 44px high, nav-link type) with a globe glyph, the current language as an endonym, and a chevron. Its "Language" label is visually hidden. It opens a native popover styled like the account menu: one 44px row per language, each endonym with its own `lang` attribute, and a check before the current one (`aria-current`). Picking a row reloads the page in that language.
- Staff pages add an account button next to it: a hairline pill with a 32px round avatar (surface-card, person glyph), the name in `{typography.nav-link}` and a chevron. On phones only the avatar shows; the name stays in the accessible name, so the row fits at 320px (WCAG 1.4.10).
- The account button opens a native popover anchored under it, aligned to its right edge: canvas, hairline, `{rounded.lg}`, `--shadow-md`, padding `{spacing.md}`. It holds the name (`{typography.title-sm}`), username (muted body-sm), the role as a surface-card pill, a "Change password" link (same row style as the admin links) and a full-width secondary "Sign out" button. Esc or a click outside closes it.
- With report.view the menu adds a "Reports" link between the role pill and "Sign out", a 44px row in `{typography.nav-link}` ink, text in line with the name. With staff.manage two more links follow, "Staff accounts" and "Roles"; with category.manage a third, "Categories and locations", all in the same style. Picking one closes the menu. This is the only way in to the admin pages: the queue gets no admin link row, because the app has no nav bar and the queue is the same working page for every role.
- Content cards (`Card`): canvas, 1px hairline, `{rounded.lg}`, `--shadow-sm`, padding `{spacing.lg}`.
- Marquee card (tracking link after submit): canvas, 1px hairline, `{rounded.xl}`, `--shadow-md`.
- Text colors: ink for headings and labels, body for running text, muted for helper text. Muted text only on canvas or surface-soft; on surface-card or a tint it drops below 4.5:1, so use body there. `{colors.muted-soft}` is for non-text marks only (3.5:1).
- No hover styles, as in the Iteration Guide. Primary darkens on press.

### Welcome page
- `/` for guests. `{component.hero-band}` adapted: h1, one-line intro, a primary "Report a problem" link-button to `/report`, and a muted hint about the saved tracking link. On the right, a marquee card "How it works" with three numbered steps (32px accent-tint circles).
- From 1024px: 7/5 grid, h1 in `{typography.display-md}`. Below: one column, card under the text, full-width button on phones.

### Report form
- `/report`. The layout's tools row is replaced by one row over the 640px form column: the "Back to home" link on the left and the language picker on the right. After submit only the picker stays; the success screen has its own "Back to home" button.
- The back link It has a 36px round arrow (`{component.button-icon-circular}`) and a `{typography.nav-link}` label in `{colors.muted}`. On hover the label turns ink and the arrow moves 4px left. The link is 44px high.

### Success after submit
- The form is replaced in place (no new URL, so the tracking token never enters browser history). A 64px success-tint circle with a success tick sits above the tracking marquee card; then file counts and upload warnings; then "Report another problem" (primary, clears the form) and "Back to home" (secondary link).
- Focus moves to the "Ticket #id created" heading, so screen readers announce it and phones scroll to it.

### Motion
- `--motion-enter` (400ms, ease-out curve) for entrances. The global `.rise` class fades content up by `{spacing.md}`; stagger with a `--delay` custom property. The success tick pops in, then draws itself.
- `prefers-reduced-motion: reduce` turns entrance animations off (`animation: none`) and sets the other motion tokens to 0ms. Playwright runs with reduced motion, so axe and screenshots see finished pages.

### Forms
- Single column, max 640px wide. From 768px up the guest form sits in a marquee card with padding `{spacing.xl}`. On phones the form is flat and the submit bar sticks to the bottom with a hairline top border and a full-width button.
- Inputs follow `{component.text-input}` with a 44px minimum height and `--shadow-sm`. Invalid inputs get a `{colors.error}` border and an error line in `--color-error-strong`.
- File drop zone: surface-soft, 1px dashed muted-soft border, `{rounded.lg}`, round "+" icon button.
- Names fields (`NamesFields`), for admin-entered names (FR-I4): a borderless fieldset whose legend (`{typography.title-sm}`, ink) names the thing, then four text inputs in an auto-fit grid (min 200px: 2×2 on desktop, one column on phones). Each input is labelled with its language endonym, as in the language picker, and carries that `lang`, so the per-language fonts and line heights apply while typing. API field codes `<prefix>.<lang>` show under the matching input.

### Staff queue
- Filters (status, priority, assignee) as selects in an auto-fit grid, search on its own row with the helper text below. Filters live in the URL.
- From 1024px: one table in a hairline card (`{rounded.lg}`, `--shadow-sm`), header row on surface-soft, summary as the row link in `{typography.title-sm}`. Seven columns do not fit narrower: the summary shrinks to one word per line. Below 1024px: the same tickets as stacked cards (number and badges on top, summary link, location, date, employee ID, assignee).
- Pager below: secondary buttons with "Page x of y" between them.

### Ticket detail
- Top: "Back to tickets" link, then h1 "Ticket #id" with status and priority badges.
- From 1024px: two columns. Left (2fr): Problem card, Photos and videos card, Conversation card. Right (1fr, min 280px): Details card, sticky while scrolling; when taller than the window it scrolls inside itself. Below 1024px: one column with Details first.
- Details card: a two-column `dl` (label in `--font-label`, value in body-sm): status, priority, category, assignee, location, guest name, employee ID, guest language, created, first response, last update. Empty values read "Not set" in muted.
- Evidence: images as 120px square thumbnails that open a modal `<dialog>` lightbox (Esc or backdrop closes); videos full width with native controls.
- Conversation: guest messages on accent tint, staff replies on surface-card, internal notes on warning tint with a dashed warning border and an "Internal note" tag.
- Action controls: at the bottom of the Details card, under a hairline and `{spacing.lg}` of space, one column of full-width selects and a primary "Save changes" button. Each control shows only with its permission (display only; the API checks again). Status offers the current status plus the moves staff may make and is hidden when there is none (closed). Priority and category start at "Not set" when empty. Assignee lists active staff plus "Unassigned"; an Agent gets a secondary "Assign to me" button instead, hidden once assigned to them. Selects never save on change: one Save sends only what changed. Success shows a toast; errors show as an error Alert under the controls, and the ticket reloads either way.
- Reply box: at the bottom of the Conversation card, under a hairline. A fieldset "Send as" with two radios (Public reply, default, and Internal note) in 44px rows, then the message text area with a helper line saying who will see it, then a full-width primary button whose label names the action ("Send reply" or "Add internal note"). Internal mode gives the text area the internal-note look (warning tint, dashed warning border); labels, helper and errors stay on canvas for contrast. Hidden without ticket.comment and on closed tickets.
- Timeline card: in the left column after Conversation (also in the single column). An ordered list, oldest first, in body-sm with a 2px hairline rail on the left. Each row is one translated sentence that places the actor ("Dev Agent changed the status from New to In Progress"), then " · " and the muted time. The actor is the staff name, "Guest" or "System"; unknown actions show their code.

### Staff accounts
- `/staff/admin/staff`, staff.manage only; without it the page shows the h1 and an error Alert "no permission". Top: "Back to tickets" link, then h1 with the active account count in muted.
- Deactivated accounts pile up over the years, so the list shows active ones only, above it a checkbox "Show deactivated accounts (n)" in a 44px row (hidden when n is 0). An account deactivated during the visit stays in view, so focus does not drop when its row would vanish.
- Root Admin (staff.create) sees an "Add a staff member" card first: name, username (with a helper naming the allowed characters), role and temporary password in an auto-fit grid, then a primary button. The temporary password is a plain text field on purpose: the Root Admin passes it on. Errors show under each field; the rest in an error Alert.
- The accounts follow the queue pattern: from 1024px a hairline table (name, username, role, status, created date); below, stacked cards with the username in muted. Status is a badge-pill: "Active" on success tint, "Deactivated" on surface-card, each with a dot.
- Per row, with staff.manage: the role as a select with a secondary "Save" button next to it (never saves on change), and a secondary "Deactivate" or "Reactivate" button that opens a modal `<dialog>` asking to confirm (Cancel, then a danger "Deactivate" or a primary "Reactivate"). In the table the select label is visually hidden ("Role for {name}"); the column header labels it. Errors show as an error Alert under that row.
- Without role.manage the Root Admin role is not offered, and Root Admin rows show their role as text with no controls (FR-A4).
- With staff.create (Root Admin), every row also gets a secondary "Reset password" button next to Deactivate. It opens a modal `<dialog>`: title "Reset password for {name}?", a line saying the person is signed out and must choose a new password, a full-width temporary password field, then Cancel and a primary "Reset password" (FR-A9).

### Sign-in and change password
- `/login`: a centered card (max 440px, hairline, `{rounded.xl}`, `--shadow-md`, padding `{spacing.xl}`) with h1, a muted intro line, an error Alert when sign-in fails (one message for any wrong username or password, another for too many attempts), Username and Password fields and a full-width primary "Sign in" button.
- `/staff/password`: the same card with h1 "Change password", Current password, New password (helper: 12 to 128 characters) and Repeat new password, then a primary "Change password" button. After a temporary password an info Alert explains why the page appears and there is no back link; staff pages redirect here until the change is done (FR-A8). Otherwise a "Back to tickets" link sits above the card.

### Roles
- `/staff/admin/roles`, staff.manage only (same "no permission" Alert otherwise). A permission grid in the order of the permission table in docs/REQUIREMENTS.md, each code shown by its translated label.
- From 1024px: a hairline table, permissions as rows and roles as columns; each column header has the role name and its staff count in muted. Below 1024px: one card per role (h2 name, staff count, then the permissions as 44px rows).
- Read-only (no role.manage): an ink check or a muted dash per cell, with "Yes"/"No" for screen readers, and a muted line "Only a Root Admin can change roles." under the h1.
- With role.manage: checkboxes in 44px targets and a secondary "Save {role}" button under each column (or card). The Root Admin column stays read-only with "Cannot be changed". staff.create and role.manage are disabled on every other role, and their row label carries a muted "Root Admin only" hint (FR-A3). A "New role" card follows: role name, a fieldset of the same permission checkboxes, and a primary "Create role" button.

### Categories and locations
- `/staff/admin/lookups`, category.manage only (same "no permission" Alert otherwise). Top: "Back to tickets" link, then h1. Two cards in an auto-fit grid (min 480px): side by side from about 1024px, stacked below.
- Each card starts with its add form, then a hairline and the list. Categories: a "New category" names fieldset and a primary "Add category" button. Locations: an h3 "Add a line", a Building select (existing buildings by English name, then "New building"), a Floor select for that building (then "New floor"; hidden when the building is new), a names fieldset for each new building or floor, then "New line" names and a primary "Add line". Selects only pick; nothing saves on change. Adding under an existing building or floor sends a copy of its loaded names.
- Lists show every row, active or not. Each row: the English name in `{typography.title-sm}` (with `lang="en"`) and the Active/Deactivated pill from "Staff accounts"; under it the other three names in muted body-sm, each with its own `lang`, separated by a middle dot; then secondary "Edit" and "Deactivate"/"Reactivate" buttons. Deactivated rows show the name in muted as well as the pill.
- Locations are grouped as the guest form groups them (by English name): building as h3 with its other names, floors as h4 indented behind a 2px hairline rail, then the line rows.
- "Edit" (`aria-expanded`) opens the names inline under the row with a primary "Save" and a secondary "Cancel"; one row at a time, and focus returns to "Edit" when it closes. A line edits its building, floor and line names, with a muted note that building and floor names change for that line only. Deactivate and reactivate confirm in the same modal `<dialog>` as staff accounts. Field errors show under each input, and every failed save also shows an error Alert in its form or row.

### Reports dashboard
- `/staff/reports`, report.view only (same "no permission" Alert otherwise). Top: "Back to tickets" link, then h1 "Reports". Order: filter row, summary cards, written summary, graphs.
- **Filter row** (FR-P2), one row above everything it scopes: From and To (native date inputs, default the API's last 30 days), Building and Category selects (first option "All …"), then a secondary "Apply" button. Controls sit in a flex row that wraps; each control grows from 130px, so on phones they wrap two per row like pills. Filters live in the URL. A changed date applies at once (once it is a full date from 2000 on and From is not after To); selects never apply on change, only through "Apply". An invalid range shows an error Alert under the row. While new data loads, the previous render stays at 50% opacity (`aria-busy`); no skeleton, no layout jump.
- **Summary cards**, in the order of docs/REQUIREMENTS.md §5, in a grid: 2 columns below 768px, auto-fit from 160px above (all 7 in one row from about 1280px). Each is a stat tile: hairline card, `{rounded.lg}`, padding `{spacing.md}`; the label in `--font-label` body, the value in `{typography.display-sm}` ink (`{typography.title-lg}` below 768px) with proportional figures, then the change against the previous period in muted body-sm: an up or down triangle (none when 0), the signed number (`Intl`, `signDisplay`) and "vs previous period". Color never carries the direction. Medians use the written summary's duration format; a null median reads "No data", and a missing previous median reads "No data for the previous period".
- Cards that match a queue filter are links (the whole tile, 44px+): Open → `/staff?status=open`; Unassigned → open plus `assignee=none`; Urgent open → open plus `priority=urgent`; New → `status=new`; Resolved → `status=resolved`. The medians are not links. Limit: the queue shows tickets as they are now and has no date, building or category filter, so the linked list is the closest honest match, not the card's exact set (the card counts open tickets as of the period end, and New and Resolved within the period).
- **WrittenSummary** below the cards (see "Reports").
- **Graphs** (FR-P1), each in a content card: title (`{typography.title-md}`) with a secondary "Show as table" toggle (`aria-pressed`) on the right, wrapping under it on narrow cards. One column below 1024px, two from 1024px. The plot box has a fixed height that includes the axis band (line, doughnut and column charts 288px; horizontal bars 32px per row plus 96px), so a card never scrolls inside itself; the canvas has `role="img"` and the chart title as its name. The toggle swaps the canvas for a `<table>` with the same values (caption = title, `th scope`), styled like the queue table. An empty dataset shows "No data for this period" in muted body-sm instead of empty axes, and no toggle.
  - Opened vs resolved per day and weekly medians: 2 lines. Weekly medians share one y-axis in hours; never two y-axes.
  - Open by status: doughnut in the badge dot colors (new brand-accent, in progress warning, waiting badge-violet).
  - Open by priority: columns; by category, by location: horizontal bars; staff workload: horizontal bars stacked by priority.
  - By location drills down: select a building bar (or its button in the table) for its floors, a floor for its lines; a "Back to …" ghost button and a muted path line sit above the plot.
- **Marks** (Chart.js 4, the dataviz method): colors only from tokens (`--chart-1`, `--chart-2`, badge dots, `--chart-priority-*`), read with `getComputedStyle`. Single-series bars all `--chart-1`. Workload: an ordinal one-hue ramp, low light to urgent dark, not triaged gray. Lines 2px, no resting markers on dense lines, 8px markers with a 2px canvas ring on hover. Bars at most 24px thick with a 4px rounded data end, square at the baseline; stacked segments and doughnut arcs split by a 2px canvas gap. Grid 1px solid `{colors.hairline}`, never dashed; axis text muted. A legend only for 2 or more series (and the doughnut), text in ink, never the series color. Tooltips: surface-dark, every series at the hovered x, numbers and durations formatted with `Intl` in the viewer's language. No animation under reduced motion.

### Reports
- Written summary (`WrittenSummary`, FR-P1): a content card titled "Summary" with one to six sentences as a bulleted list, one sentence per item, body type in `{colors.body}`, `{spacing.xs}` apart. A list rather than a paragraph, so each sentence scans on its own and no language needs a sentence separator. Every sentence is one whole message (`summary_*` keys); numbers, dates and durations are formatted with `Intl` in the viewer's language.

### Feedback
- Badges follow `{component.badge-pill}`: tint background, colored dot, ink `{typography.caption}` text. Urgent priority is solid `--color-error-strong` with white text and no dot.

| Code | Background | Dot |
|---|---|---|
| status `new`, priority `medium` | accent tint | brand-accent |
| status `in_progress`, priority `high` | warning tint | warning |
| status `waiting` | violet tint | badge-violet |
| status `resolved` | success tint | success |
| status `closed` | surface-card | muted-soft |
| priority `low` | surface-card | muted |

- Alerts: tint background, `{rounded.lg}`, a glyph in a white circle, then the text. Color is never the only signal.
- Toast: surface-dark pill, bottom center, `--shadow-md`.

### Accessibility
- WCAG 2.2 AA. The e2e suite runs axe on the welcome page and the guest form in English and Burmese; `/dev/components?axe` checks the component gallery.
