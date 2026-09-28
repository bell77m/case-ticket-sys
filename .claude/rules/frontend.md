---
paths:
  - "frontend/**"
---

# Frontend rules

- Svelte 5 runes (`$state`, `$derived`, `$props`). adapter-static SPA, no server routes.
- Every user-facing string is a Paraglide message. Add the key to en, zh-CN, my and th together.
- Format dates and numbers with `Intl`, in the active locale.
- Burmese text input: detect Zawgyi and convert to Unicode before submit (FR-I8).
- UI checks are convenience only; the API enforces permissions.
- Never save on a select's change event: on Windows, arrow keys on a closed select fire change. Use an explicit Save button (see the ticket detail Details card).
- Windows file names are case-insensitive: never name a module like a component (`toast.svelte.ts` next to `Toast.svelte` resolves to the component). Use a distinct name such as `toast-state.svelte.ts`.
- Paraglide options live in two places: `paraglideVitePlugin` in vite.config.ts and the `check` script in package.json. Change both together.
- Links and GET forms that must leave the SPA need `data-sveltekit-reload`; otherwise the SvelteKit router handles them and shows its 404 page.
- e2e: a sign-in or save done with `fetch` has no navigation to wait for. Wait for its response (see `signIn` in e2e/helpers.ts) before `page.goto`, or the navigation cancels the request.
- e2e: `page.goto` resolves before the SPA's load function has fetched its data. When a test changes data and expects the page to pick it up later (live refresh, reload), first wait for the page's own API response (`page.waitForResponse`, set up before `goto`), or the first load may already include the change and the test proves nothing. Check such a test fails with the feature switched off.
