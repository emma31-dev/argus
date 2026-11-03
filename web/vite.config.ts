/// <reference types="vitest/config" />
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), 'PUBLIC_');
	const apiBase = (process.env.PUBLIC_API_BASE ?? env.PUBLIC_API_BASE ?? '').replace(/\/+$/, '');
	const proxyTarget = process.env.ARGUS_API_PROXY ?? 'http://localhost:8080';

	return {
		plugins: [sveltekit()],
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
