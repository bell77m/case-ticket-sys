---
paths:
  - "frontend/**"
---

# Frontend rules

- Svelte 5 runes (`$state`, `$derived`, `$props`). adapter-static SPA, no server routes.
- Every user-facing string is a Paraglide message. Add the key to en, zh-CN, my and th together.
- Format dates and numbers with `Intl`, in the active locale. Every `Intl.DateTimeFormat` passes `calendar: 'gregory'`: Thai defaults to the Buddhist era (2569 for 2026), and the app shows Gregorian years in every language.
- To look at a PDF export: the Read tool cannot render PDFs here (no pdftoppm) and headless Edge shows a blank viewer. Screenshot the print page (`/print/report`, mocked with `page.route`) with Playwright instead; it is the page Gotenberg prints.
- Burmese text input: detect Zawgyi and convert to Unicode before submit (FR-I8). `call()` in src/lib/api.ts already does it for every JSON write; send new writes through `call()`.
- A dependency imported only with `import()` goes in `optimizeDeps.include` (vite.config.ts); otherwise the dev server finds it on first use and reloads the page mid-action, which fails e2e.
- Unicode ranges in source: tool inputs turn `\u` escapes into raw characters. Use a script property (`/\p{Script=Myanmar}/u`) or build the characters from char codes.
- UI checks are convenience only; the API enforces permissions.
- Never save on a select's change event: on Windows, arrow keys on a closed select fire change. Use an explicit Save button (see the ticket detail Details card).
- Windows file names are case-insensitive: never name a module like a component (`toast.svelte.ts` next to `Toast.svelte` resolves to the component). Use a distinct name such as `toast-state.svelte.ts`.
- Paraglide options live in two places: `paraglideVitePlugin` in vite.config.ts and the `check` script in package.json. Change both together.
- Links and GET forms that must leave the SPA need `data-sveltekit-reload`; otherwise the SvelteKit router handles them and shows its 404 page.
- e2e: a sign-in or save done with `fetch` has no navigation to wait for. Wait for its response (see `signIn` in e2e/helpers.ts) before `page.goto`, or the navigation cancels the request.
- e2e: `page.goto` resolves before the SPA's load function has fetched its data. When a test changes data and expects the page to pick it up later (live refresh, reload), first wait for the page's own API response (`page.waitForResponse`, set up before `goto`), or the first load may already include the change and the test proves nothing. Check such a test fails with the feature switched off.
- CSP (T3.17): the built app refuses inline styles and scripts. Never write a static `style="..."` attribute in a component or app.html; use a `style:` directive (Svelte sets it through CSSOM, which the CSP allows) or a class. Vite allows inline styles in dev, so only e2e against the built app (`make serve-built`) catches a violation. The policy is `default-src 'self'` plus exact exceptions (data:/blob: images, blob: media, data: fonts): a new kind of resource (another origin, a worker, a frame) needs its directive in `csp.directives` in vite.config.ts.
- e2e: inject a script with `page.evaluate(source)`, never `page.addScriptTag`: the CSP refuses the inline script tag (see `axeViolations` in e2e/helpers.ts).
