import { paraglideVitePlugin } from '@inlang/paraglide-js';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const api = 'http://localhost:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) => filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// SPA: the Go server embeds build/ and serves index.html for unknown paths.
			adapter: adapter({ fallback: 'index.html' })
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
		}
	],
	server: {
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
