import { defineConfig } from '@playwright/test';
import { readFileSync } from 'node:fs';

// Backend settings come from ../.env.example, the same defaults `make dev` uses.
const env = Object.fromEntries(
	readFileSync('../.env.example', 'utf8')
		.split(/\r?\n/)
		.filter((l) => l && !l.startsWith('#') && l.includes('='))
		.map((l) => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)])
);

// E2E_BASE_URL (CI): test an app that is already running, such as the Go binary serving the built SPA on :8080.
// Unset (local): start the Go API and Vite below and test through Vite on :5173.
const external = process.env.E2E_BASE_URL;

// Needs the compose DB migrated and seeded (make migrate seed). Uses the installed Edge, no browser download.
export default defineConfig({
	testDir: 'e2e',
	// CI: one retry, so a timing-sensitive test under full-suite load (live.spec's 2 s budget, Gotenberg prints)
	// shows as "flaky" in the report instead of failing the build. Locally a failure fails at once.
	retries: process.env.CI ? 1 : 0,
	// The first page load after starting Vite compiles dependencies and can take several seconds.
	expect: { timeout: 10_000 },
	// Reduced motion: no entrance animations, so axe and screenshots see finished pages.
	use: { baseURL: external ?? 'http://localhost:5173', channel: 'msedge', contextOptions: { reducedMotion: 'reduce' } },
	projects: [{ name: 'phone', use: { viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: false } }],
	webServer: external ? [] : [
		// e2e opens about 14 tickets per run from localhost, over the guest limit of 5 per 10 minutes (NFR-3).
		// PDF export (FR-P4): the Makefile's defaults, which .env.example does not carry.
		{
			command: 'go run ./cmd/ticket-app',
			cwd: '../backend',
			url: 'http://localhost:8080/healthz',
			env: {
				...env,
				GUEST_TICKET_LIMIT: '1000',
				GOTENBERG_URL: env.GOTENBERG_URL || 'http://localhost:3000',
				PRINT_BASE_URL: env.PRINT_BASE_URL || 'http://host.docker.internal:5173'
			},
			reuseExistingServer: true,
			timeout: 120_000
		},
		{ command: 'npm run dev', url: 'http://localhost:5173', reuseExistingServer: true, timeout: 120_000 }
	]
});
