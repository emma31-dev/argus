/// <reference types="vitest/config" />
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), 'PUBLIC_');
	const apiBase = (process.env.PUBLIC_API_BASE ?? env.PUBLIC_API_BASE ?? '').replace(/\/+$/, '');
	const proxyTarget = process.env.ARGUS_API_PROXY ?? 'http://localhost:8080';
	const rawBase = process.env.BASE_PATH ?? '';
	const base = (rawBase && !rawBase.startsWith('/') ? `/${rawBase}` : rawBase) as '' | `/${string}`;

	return {
		plugins: [
			sveltekit({
				preprocess: vitePreprocess(),
				// Fully static output. Every route is prerendered as an HTML shell;
				// data is fetched client-side from the Argus API. `404.html` is the SPA
				// fallback for hosts that serve it on unknown paths (it must not be
				// `index.html`, which would overwrite the prerendered overview page).
				adapter: adapter({
					pages: 'build',
					assets: 'build',
					fallback: '404.html',
					precompress: false,
					strict: true
				}),

				prerender: {
					// Pages link to live API endpoints (/v1/..., /healthz), which only
					// exist when the Go server is running. Don't fail the static build
					// on them; fail on anything else.
					handleHttpError: ({ path, message }) => {
						if (path.startsWith('/v1/') || path === '/healthz' || path === '/readyz') return;

						throw new Error(message);
					}
				},
				// Emit /token/index.html etc. so any plain static file server
				// (including Go's http.FileServer) resolves clean URLs.
				paths: { base }
			})
		],
		define: {
			// Base URL of the Argus API. '' means same origin (the default:
			// the Go server serves both the API and this static build).
			__ARGUS_API_BASE__: JSON.stringify(apiBase)
		},
		server: {
			proxy: {
				'/v1': { target: proxyTarget, changeOrigin: true },
				'/healthz': { target: proxyTarget, changeOrigin: true }
			}
		},
		test: {
			include: ['src/**/*.test.ts'],
			environment: 'node'
		}
	};
});
