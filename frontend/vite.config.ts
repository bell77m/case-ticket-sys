import { paraglideVitePlugin } from '@inlang/paraglide-js';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { spawn } from 'node:child_process';
import { connect } from 'node:net';

const api = 'http://localhost:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) => filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// SPA: the Go server embeds build/ and serves index.html for unknown paths.
			// precompress writes .br and .gz copies, which the Go server sends instead of compressing per request.
			adapter: adapter({ fallback: 'index.html', precompress: true }),

			// CSP (T3.17): the SPA's inline bootstrap script is allowed by its hash, sent as a <meta> tag in the built
			// index.html. The Go server's header (securityHeaders in main.go) keeps frame-ancestors, object-src, base-uri
			// and form-action. Vite adds 'unsafe-inline' to style-src in dev only. The one inline style allowed is SvelteKit's
			// route announcer (visually hidden; blocked, it shows on screen): its exact value, by hash. A SvelteKit update
			// that changes it fails e2e/smoke.spec.ts against the built app, which prints the new hash. default-src 'self'
			// covers fetch/EventSource and frames. img-src needs data: for the tracking QR code and blob: for upload
			// previews and guest evidence (fetched with the token header); media-src needs blob: for guest evidence videos;
			// font-src needs data: because Vite inlines font files under 4 KB into the CSS.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['self'],
					'img-src': ['self', 'data:', 'blob:'],
					'media-src': ['self', 'blob:'],
					'font-src': ['self', 'data:'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-hashes', 'sha256-S8qMpvofolR8Mpjy4kQvEm7m1q8clzU4dfDH0AmvZjo=']
				}
			}
		}),

		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			emitTsDeclarations: true,
			// SPA, no locale in URLs: saved choice, then browser language, then English (FR-I2).
			// Keep in sync with the "check" script in package.json.
			strategy: ['cookie', 'preferredLanguage', 'baseLocale'],
			disableAsyncLocalStorage: true
		}),

		// Fontsource CSS lists woff2 first and woff as a fallback that only pre-2016 browsers fetch.
		// Dropping the woff files keeps the embedded build (and the image, T1.20) about 11 MB smaller.
		{
			name: 'woff2-only',
			apply: 'build',
			generateBundle(_, bundle) {
				for (const name of Object.keys(bundle)) if (name.endsWith('.woff')) delete bundle[name];
			}
		},

		// `npm run dev` alone left /api with nothing behind it (ECONNREFUSED): start the Go API with `make dev-backend`
		// (the same settings as `make dev`) when nothing listens on :8080. Under make (MAKELEVEL set), `make dev` starts
		// it itself; Playwright starts its API before Vite. Ctrl+C stops both, as they share the terminal.
		{
			name: 'start-api',
			apply: 'serve',
			configureServer() {
				if (process.env.MAKELEVEL) return;
				const probe = connect(8080, '127.0.0.1');
				probe.on('connect', () => probe.destroy());
				probe.on('error', () => {
					console.log('Go API not running on :8080, starting it: make dev-backend');
					spawn('make', ['dev-backend'], { cwd: '..', stdio: 'inherit' }).on('error', (err) =>
						console.error(`Could not start the Go API (${err.message}). Run make dev-backend in the repo root.`)
					);
				});
			}
		}
	],
	// Loaded only on the first Burmese submit (FR-I8); found that late, the dev server would reload the page mid-submit.
	optimizeDeps: { include: ['myanmar-tools'] },
	server: {
		// PDF export (FR-P4): Gotenberg's Chromium, in Docker, opens /print/report as host.docker.internal:5173
		// (PRINT_BASE_URL). Docker Desktop forwards that to the host's 127.0.0.1, while "localhost" binds only ::1 on
		// Windows; 127.0.0.1 keeps the dev server off the network.
		host: '127.0.0.1',
		allowedHosts: ['host.docker.internal'],
		proxy: {
			'/api': {
				target: api,
				// Events stream as they come, but Node holds response headers until the first body byte, and an SSE stream
				// (FR-P3) sends none until the first event or ping: send them once the proxy has copied them, so EventSource opens.
				configure: (proxy) =>
					proxy.on('proxyRes', (proxyRes, _req, res) => {
						if (proxyRes.headers['content-type']?.startsWith('text/event-stream')) process.nextTick(() => res.flushHeaders());
					})
			},
			'/healthz': api
		}
	}
});
