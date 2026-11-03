import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
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
		paths: {
			base: process.env.BASE_PATH ?? ''
		}
	}
};

export default config;
