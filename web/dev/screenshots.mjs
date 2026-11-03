#!/usr/bin/env node
/*
 * Screenshot the dashboard against the mock API (dev only).
 *
 *   pnpm dev:mock                       # in one terminal
 *   node dev/screenshots.mjs [outDir] [--only=overview,token] [--full]
 *
 * Writes <name>-<width>-<theme>.png. Uses the preinstalled Chromium via
 * Playwright; nothing here is part of the production build.
 */
import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';

// Resolve the globally installed Playwright (not a project dependency).
const { chromium } = createRequire(import.meta.url)('playwright');

const argv = process.argv.slice(2);
const outDir = argv.find((a) => !a.startsWith('--')) ?? 'dev/.screens';
const only = argv.find((a) => a.startsWith('--only='))?.slice(7).split(',');
const full = argv.includes('--full');
const widths = (argv.find((a) => a.startsWith('--widths='))?.slice(9) ?? '1440,390').split(',').map(Number);
const themes = (argv.find((a) => a.startsWith('--themes='))?.slice(9) ?? 'light,dark').split(',');
const BASE = process.env.BASE_URL ?? 'http://localhost:5173';
const MOCK = process.env.MOCK_URL ?? 'http://localhost:8080';

const pages = [
	{ name: 'overview', path: '/' },
	{ name: 'token', path: '/token/?id=' + encodeURIComponent('nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near') },
	{ name: 'token-green', path: '/token/?id=' + encodeURIComponent('nep141:wrap.near') },
	{ name: 'incidents', path: '/incidents/' },
	{ name: 'incident', path: '/incident/?id=7' },
	{ name: 'how', path: '/how-it-works/' },
	{ name: 'replay', path: '/replay/' },
	{ name: 'replay-empty', path: '/replay/', mock: 'replay=0' },
	{ name: 'api', path: '/api/' },
	{ name: 'offline', path: '/', mock: 'down=1' },
	{ name: 'stale', path: '/', mock: 'stale=1' }
].filter((p) => !only || only.includes(p.name));

fs.mkdirSync(outDir, { recursive: true });
const browser = await chromium.launch();
// Mock switches are best-effort, so the script also works against the real backend.
const reset = () => fetch(`${MOCK}/__mock?scenario=incident&replay=1&stale=0&down=0`).catch(() => {});
for (const pg of pages) {
	for (const width of widths) {
		for (const theme of themes) {
			await reset();
			if (pg.mock) await fetch(`${MOCK}/__mock?${pg.mock}`).catch(() => {});
			const ctx = await browser.newContext({
				viewport: { width, height: width < 600 ? 844 : 900 },
				deviceScaleFactor: 1,
				colorScheme: theme
			});
			await ctx.addInitScript((t) => {
				try {
					localStorage.setItem('argus-theme', t);
				} catch {}
			}, theme);
			const page = await ctx.newPage();
			page.on('pageerror', (e) => console.error(`[${pg.name}] pageerror`, e.message));
			page.on('console', (m) => m.type() === 'error' && console.error(`[${pg.name}] console`, m.text()));
			await page.goto(BASE + pg.path, { waitUntil: 'load' });
			await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
			await page.waitForTimeout(700);
			const file = path.join(outDir, `${pg.name}-${width}-${theme}.png`);
			await page.screenshot({ path: file, fullPage: full });
			console.log('wrote', file);
			await ctx.close();
		}
	}
}
await reset();
await browser.close();
