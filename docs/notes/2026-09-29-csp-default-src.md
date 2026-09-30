# 2026-09-29-csp-default-src CSP default-src with exact exceptions

Date: 2026-09-29 · Requirements: none (security hardening, follow-up of T3.17 and T2.14) · Agent: fe (main agent; the config by svelte-frontend)

## What was done
- The app's CSP (the `<meta>` tag SvelteKit writes into the built index.html) now starts from `default-src 'self'`. Fetch, EventSource, frames, workers and everything else not listed below may only reach the app's own origin.
- Exceptions, each for a feature that needs it:
  - `img-src 'self' data: blob:`. `data:` is for the QR code on the tracking card. `blob:` is for upload previews on the guest form and for guest evidence, which is fetched with the token header and shown from an object URL.
  - `media-src 'self' blob:`: guest evidence videos (blob:) and staff evidence videos (same origin).
  - `font-src 'self' data:`: Vite inlines font files under 4 KB into the CSS as data: URIs.
- The smoke flow now also attaches a video and opens both files on the tracking page, so the blob: image and video paths run under the policy in all 4 languages.

## Files
- frontend/vite.config.ts: `default-src`, `img-src`, `media-src` and `font-src` in `csp.directives`, with a comment on why each exception exists.
- frontend/e2e/smoke.spec.ts:
  - the guest attaches a photo and a video, and opens both on the tracking page before confirming;
  - the CSP test requires `default-src 'self'`.
- .claude/rules/frontend.md: the CSP rule says a new kind of resource needs its directive.

## Decisions
- `font-src data:` rather than turning off Vite's asset inlining for fonts. A data: font cannot run code or reach the network. Reverse: `build.assetsInlineLimit` returning false for font files, then drop `data:` from font-src.
- `connect-src`, `worker-src` and `frame-src` are not set on their own: `default-src 'self'` covers them.

## Checks
- Test first: against the app built before the change, the CSP test failed on the missing `default-src 'self'`. The 4 smoke flows passed with the video evidence added.
- First build with `default-src`: 522 refusals, all of them `Loading the font 'data:font/woff2…'` or `'data:font/woff…'`. `font-src 'self' data:` was added.
- Against the built app (binary on :8081 with `GUEST_TICKET_LIMIT=1000`, `E2E_BASE_URL=http://localhost:8081`), `e2e/smoke.spec.ts` passed 5/5 with no CSP refusal.
- Full suite against the built app, 6 workers: 57 passed, 4 failed. The 4 do not come from this change:
  - live.spec:42, live.spec:77 and export.spec:38 (Thai and Burmese PDF with fonts and charts) are load flakes. All passed when run again with one worker, together with live.spec:88.
  - reports.spec:137 is another session's test. Its expected open-ticket count drifted (867 against 870) while other specs ran in parallel.
- Dev mode: the smoke spec run through Vite logged no CSP refusal on any page it reached, HMR websocket included. It stopped at the guest form, because the shared Go API on :8080 enforced the guest limit of 5 tickets per 10 minutes (NFR-3).
- `npm run check`: 0 errors, 0 warnings.
- Security review (vite.config.ts, main.go, the three exceptions): no findings. Each exception is as narrow as its use allows. data:/blob: images and media cannot run script (and object-src 'none' stays). blob: URLs are same-origin only. worker-src, frame-src and connect-src fall back to 'self'.

## Docs updated
- .claude/rules/frontend.md: CSP rule extended (see Files).

## Follow-ups
- None.
