#!/usr/bin/env node
/*
 * ============================================================================
 *  SYNTHETIC DESIGN DATA, FOR LOCAL DEVELOPMENT ONLY.
 *
 *  Every number this server returns is invented. It exists so the dashboard
 *  can be designed and tested while the Go backend is not running. It is NOT
 *  imported by the SvelteKit app and is never part of the production build
 *  (it lives outside src/ and nothing in src/ references it).
 * ============================================================================
 *
 * A tiny HTTP server on :8080 that serves JSON shaped exactly like
 * docs/api.md. The Vite dev server proxies /v1 here.
 *
 *   node dev/mock-api.mjs              # mock API only
 *   node dev/mock-api.mjs --with-vite  # mock API + `vite dev` (pnpm dev:mock)
 *
 * Options (flags or env):
 *   --port=8080        MOCK_PORT
 *   --scenario=incident|ok|settling     MOCK_SCENARIO  (default: incident)
 *   --no-replay        MOCK_REPLAY=0    /v1/replay answers 404
 *   --stale            MOCK_STALE=1     last check 6 minutes ago
 *
 * Runtime switches (handy for screenshots):
 *   GET /__mock?scenario=ok&replay=0&stale=1&down=1
 *   (down=1 makes every /v1 route fail with a non-JSON 503, like a dead backend.)
 */

import http from 'node:http';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const args = new Map(
	process.argv.slice(2).map((a) => {
		const [k, v] = a.replace(/^--/, '').split('=');
		return [k, v ?? 'true'];
	})
);

const cfg = {
	port: Number(args.get('port') ?? process.env.MOCK_PORT ?? 8080),
	scenario: args.get('scenario') ?? process.env.MOCK_SCENARIO ?? 'incident',
	replay: !(args.has('no-replay') || process.env.MOCK_REPLAY === '0'),
	stale: args.has('stale') || process.env.MOCK_STALE === '1',
	down: false
};

// ---------------------------------------------------------------------------
// Deterministic helpers
// ---------------------------------------------------------------------------

function hash(str) {
	let h = 2166136261;
	for (let i = 0; i < str.length; i++) {
		h ^= str.charCodeAt(i);
		h = Math.imul(h, 16777619);
	}
	return h >>> 0;
}
function rand(seed) {
	// mulberry32
	let t = (seed + 0x6d2b79f5) >>> 0;
	t = Math.imul(t ^ (t >>> 15), t | 1);
	t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
	return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const pow10 = (n) => 10n ** BigInt(n);

/** Whole tokens (float) -> smallest units (bigint), with deterministic dust. */
function units(x, decimals, seed) {
	const sig = Math.min(decimals, 6);
	const base = BigInt(Math.round(x * 10 ** sig)) * pow10(decimals - sig);
	if (decimals <= 6) return base;
	const dust = BigInt(Math.floor(rand(seed) * 1e9)) % pow10(decimals - sig);
	return base + dust;
}

// ---------------------------------------------------------------------------
// Synthetic token universe
// ---------------------------------------------------------------------------

const T = (id, symbol, decimals, supply, chain) => {
	const [standard, ...rest] = id.split(':');
	const contract = rest[0];
	const inner_id = standard === 'nep245' ? rest.slice(1).join(':') : '';
	return { id, standard, contract, inner_id, symbol, decimals, supply, chain };
};

// Token ids, symbols and decimals mirror config/tokens.yaml (real ids).
// Supplies are invented round-ish numbers: SYNTHETIC.
const TOKENS = [
	T('nep141:wrap.near', 'wNEAR', 24, 41_234_567.12, 'NEAR'),
	T('nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1', 'USDC', 6, 58_912_004.5, 'NEAR'),
	T('nep141:usdt.tether-token.near', 'USDT', 6, 23_401_877.31, 'NEAR'),
	T('nep141:eth-0xdac17f958d2ee523a2206206994597c13d831ec7.omft.near', 'USDT', 6, 12_771_003.9, 'Ethereum'),
	T('nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near', 'USDC', 6, 18_204_551.2, 'Ethereum'),
	T('nep141:base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near', 'USDC', 6, 3_220_118.4, 'Base'),
	T('nep141:arb-0xaf88d065e77c8cc2239327c5edb3a432268e5831.omft.near', 'USDC', 6, 2_904_551.0, 'Arbitrum'),
	T('nep141:sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near', 'USDC', 6, 7_402_993.6, 'Solana'),
	T('nep141:sol-c800a4bd850783ccb82c2b2c7e84175443606352.omft.near', 'USDT', 6, 4_118_220.2, 'Solana'),
	T('nep141:tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near', 'USDT', 6, 9_880_412.7, 'Tron'),
	T('nep141:eth.omft.near', 'ETH', 18, 9_812.4471, 'Ethereum'),
	T('nep141:base.omft.near', 'ETH', 18, 2_019.004, 'Base'),
	T('nep141:arb.omft.near', 'ETH', 18, 1_311.82, 'Arbitrum'),
	T('nep141:eth.bridge.near', 'ETH', 18, 702.551, 'Ethereum (Rainbow)'),
	T('nep141:nbtc.bridge.near', 'BTC', 8, 141.2207, 'Bitcoin (Omni)'),
	T('nep141:btc.omft.near', 'BTC', 8, 612.2381, 'Bitcoin'),
	T('nep141:eth-0x2260fac5e5542a773aa44fbcfedf7c193bc2c599.omft.near', 'WBTC', 8, 88.0213, 'Ethereum'),
	T('nep141:sol.omft.near', 'SOL', 9, 148_220.91, 'Solana'),
	T('nep141:zec.omft.near', 'ZEC', 8, 71_420.55, 'Zcash'),
	T('nep141:doge.omft.near', 'DOGE', 8, 9_120_334.1, 'Dogecoin'),
	T('nep141:xrp.omft.near', 'XRP', 6, 4_402_118.0, 'XRP Ledger'),
	T('nep141:tron.omft.near', 'TRX', 6, 12_804_221.7, 'Tron'),
	T('nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g', 'USDT', 18, 5_310_442.8, 'BNB Chain'),
	T('nep245:v2_1.omni.hot.tg:56_2w93GqMcEmQFDru84j3HZZWt557r', 'USDC', 18, 1_820_119.3, 'BNB Chain'),
	T('nep245:v2_1.omni.hot.tg:56_11111111111111111111', 'BNB', 18, 3_120.441, 'BNB Chain'),
	T('nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr', 'USDT', 6, 902_118.3, 'Polygon')
];

const RED_ID = 'nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near';
const AMBER_ID = 'nep141:zec.omft.near';
const UNKNOWN_ID = 'nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr';
const STALE_ID = 'nep141:doge.omft.near';
const BLIP_ID = 'nep141:wrap.near';

const INTERVAL_S = 30;
const K = 4;
const BLOCK_MS = 600;
const T0 = Date.UTC(2026, 10, 3, 12, 0, 0);
const H0 = 171_234_567;

const blockAt = (ms) => H0 + Math.floor((ms - T0) / BLOCK_MS);
const timeAt = (h) => T0 + (h - H0) * BLOCK_MS;
const iso = (ms) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z');
const snapFloor = (ms) => Math.floor(ms / (INTERVAL_S * 1000)) * INTERVAL_S * 1000;

function now() {
	return cfg.stale ? Date.now() - 6 * 60_000 : Date.now();
}

/** Negative-gap windows per token: [startMs, endMs, peakDeficitTokens, shape]. */
function negativeWindows(tok, nowMs) {
	const w = [];
	const latest = snapFloor(nowMs);
	if (tok.id === RED_ID && cfg.scenario === 'incident') {
		w.push([latest - 41 * 60_000, latest + 60_000, 184_220.42, 'grow']);
	}
	if (tok.id === AMBER_ID && cfg.scenario !== 'ok') {
		w.push([latest - 30_000, latest + 60_000, 0.8125, 'flat']);
	}
	if (tok.id === BLIP_ID) {
		// A single in-flight deposit snapshot a few hours ago (settled, never red).
		w.push([latest - 3 * 3600_000 - 17 * 60_000, latest - 3 * 3600_000 - 16 * 60_000, 12.5, 'flat']);
		w.push([latest - 26 * 3600_000, latest - 26 * 3600_000 + 2 * 60_000, 3.1, 'flat']);
	}
	if (tok.symbol === 'SOL') {
		// The closed incident #6.
		const s = latest - 5 * 86400_000 - 3 * 3600_000;
		w.push([s, s + 3.5 * 60_000, 41.2, 'bump']);
	}
	return w;
}

function pointAt(tok, ms, nowMs) {
	const seed = hash(tok.id);
	const ph = rand(seed) * Math.PI * 2;
	const day = 86400_000;
	const x =
		tok.supply *
		(1 +
			0.045 * Math.sin((ms / (7 * day)) * Math.PI * 2 + ph) +
			0.012 * Math.sin((ms / (9 * 3600_000)) * Math.PI * 2 + ph * 2) +
			0.0007 * Math.sin((ms / (47 * 60_000)) * Math.PI * 2 + ph * 3));
	const owed = units(x, tok.decimals, seed + Math.floor(ms / 1000));
	// Normal surplus: fees and dust, a few millionths of supply.
	const surplusTok = tok.supply * (2.5e-6 + 1.5e-6 * Math.sin((ms / (5 * 3600_000)) * Math.PI * 2 + ph));
	let gap = units(Math.max(surplusTok, 0), tok.decimals, seed ^ Math.floor(ms / 7000));
	for (const [s, e, peak, shape] of negativeWindows(tok, nowMs)) {
		if (ms >= s && ms < e) {
			const p = (ms - s) / Math.max(1, Math.min(e, nowMs) - s);
			let d = peak;
			if (shape === 'grow') d = peak * Math.min(1, 0.08 + 0.92 * Math.pow(p, 0.7));
			if (shape === 'bump') d = peak * Math.sin(Math.PI * Math.min(1, p + 0.1));
			gap = -units(d, tok.decimals, seed + 7);
		}
	}
	return {
		block_height: blockAt(ms),
		block_time: iso(ms),
		owed: owed.toString(),
		held: (owed + gap).toString(),
		gap: gap.toString()
	};
}

function tokenStatus(tok, nowMs) {
	if (tok.id === UNKNOWN_ID) return { status: 'unknown', streak: 0 };
	const latest = snapFloor(nowMs);
	let streak = 0;
	for (let i = 0; i < 400; i++) {
		const p = pointAt(tok, latest - i * INTERVAL_S * 1000, nowMs);
		if (BigInt(p.gap) < 0n) streak++;
		else break;
	}
	return { status: streak === 0 ? 'green' : streak >= K ? 'red' : 'amber', streak };
}

function tokenObject(tok, nowMs) {
	const latestMs = snapFloor(nowMs);
	const { status, streak } = tokenStatus(tok, nowMs);
	const latestAt = tok.id === STALE_ID ? latestMs - 4 * INTERVAL_S * 1000 : latestMs;
	const latest = tok.id === UNKNOWN_ID ? null : { ...pointAt(tok, latestAt, nowMs), provider: 'fastnear' };
	return {
		id: tok.id,
		standard: tok.standard,
		contract: tok.contract,
		inner_id: tok.inner_id,
		symbol: tok.symbol,
		decimals: tok.decimals,
		chain: tok.chain,
		status,
		negative_streak: streak,
		latest,
		...(tok.id === STALE_ID ? { stale: true, last_error: 'rpc fastnear: context deadline exceeded after 10s' } : {})
	};
}

const RANK = { red: 0, amber: 1, unknown: 2, green: 3 };
function ratio(t) {
	if (!t.latest || BigInt(t.latest.owed) === 0n) return Infinity;
	return Number((BigInt(t.latest.gap) * 10n ** 12n) / BigInt(t.latest.owed));
}
function allTokens(nowMs) {
	return TOKENS.map((t) => tokenObject(t, nowMs)).sort(
		(a, b) => RANK[a.status] - RANK[b.status] || ratio(a) - ratio(b)
	);
}

// ---------------------------------------------------------------------------
// Incidents
// ---------------------------------------------------------------------------

function txHash(seed) {
	const alphabet = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
	let s = '';
	for (let i = 0; i < 44; i++) s += alphabet[Math.floor(rand(seed + i * 31) * alphabet.length)];
	return s;
}

function incidents(nowMs) {
	const latest = snapFloor(nowMs);
	const list = [];
	const red = TOKENS.find((t) => t.id === RED_ID);
	if (cfg.scenario === 'incident') {
		const [s] = negativeWindows(red, nowMs)[0];
		const startMs = s;
		const redMs = s + (K - 1) * INTERVAL_S * 1000;
		const p = pointAt(red, latest, nowMs);
		list.push({
			id: 7,
			token_id: red.id,
			symbol: red.symbol,
			chain: red.chain,
			decimals: red.decimals,
			start_block: blockAt(startMs),
			red_block: blockAt(redMs),
			end_block: null,
			opened_at: iso(redMs + 3000),
			closed_at: null,
			duration_seconds: Math.round((nowMs - redMs) / 1000),
			max_deficit: (-BigInt(p.gap)).toString(),
			max_deficit_block: p.block_height,
			summary: ''
		});
	}
	const sol = TOKENS.find((t) => t.symbol === 'SOL');
	const [ss, se] = negativeWindows(sol, nowMs)[0];
	list.push({
		id: 6,
		token_id: sol.id,
		symbol: sol.symbol,
		chain: sol.chain,
		decimals: sol.decimals,
		start_block: blockAt(ss),
		red_block: blockAt(ss + (K - 1) * INTERVAL_S * 1000),
		end_block: blockAt(se),
		opened_at: iso(ss + (K - 1) * INTERVAL_S * 1000 + 3000),
		closed_at: iso(se + 2000),
		duration_seconds: Math.round((se - ss - (K - 1) * INTERVAL_S * 1000) / 1000),
		max_deficit: units(41.2, 9, 1).toString(),
		max_deficit_block: blockAt(ss + 1.6 * 60_000),
		summary: ''
	});
	const eth = TOKENS.find((t) => t.id === 'nep141:base.omft.near');
	const es = latest - 12 * 86400_000 - 7 * 3600_000;
	list.push({
		id: 5,
		token_id: eth.id,
		symbol: eth.symbol,
		chain: eth.chain,
		decimals: eth.decimals,
		start_block: blockAt(es),
		red_block: blockAt(es + 90_000),
		end_block: blockAt(es + 7 * 60_000),
		opened_at: iso(es + 93_000),
		closed_at: iso(es + 7 * 60_000 + 2000),
		duration_seconds: Math.round((7 * 60_000 - 90_000) / 1000),
		max_deficit: units(0.4212, 18, 3).toString(),
		max_deficit_block: blockAt(es + 4 * 60_000),
		summary: ''
	});
	return list;
}

function incidentDetail(inc, nowMs) {
	const tok = TOKENS.find((t) => t.id === inc.token_id);
	const startMs = timeAt(inc.start_block);
	const endMs = inc.end_block ? timeAt(inc.end_block) : snapFloor(nowMs);
	const pad = 6 * INTERVAL_S * 1000;
	const snapshots = [];
	for (let ms = snapFloor(startMs - pad); ms <= Math.min(snapFloor(nowMs), endMs + pad); ms += INTERVAL_S * 1000) {
		const p = pointAt(tok, ms, nowMs);
		if (inc.id === 5 && ms >= startMs && ms < endMs) {
			const q = (ms - startMs) / (endMs - startMs);
			const d = units(0.4212 * Math.sin(Math.PI * Math.min(1, q + 0.05)), 18, 3);
			p.gap = (-d).toString();
			p.held = (BigInt(p.owed) - d).toString();
		}
		snapshots.push(p);
	}
	const events = [];
	const n = inc.id === 7 ? 9 : 3;
	for (let i = 0; i < n; i++) {
		const h = inc.start_block - 3 + Math.floor((i / n) * ((inc.end_block ?? blockAt(endMs)) - inc.start_block));
		const mint = i % 3 !== 2;
		const amount = units(mint ? 12_000 + rand(i + inc.id) * 30_000 : 4_000 + rand(i) * 2_000, tok.decimals, i);
		const ev = mint ? 'mt_mint' : 'ft_withdraw';
		events.push({
			incident_id: inc.id,
			block_height: h,
			receipt_id: txHash(hash(`r${inc.id}-${i}`)),
			tx_hash: txHash(hash(`t${inc.id}-${i}`)),
			standard: mint ? 'nep245' : 'dip4',
			event: ev,
			token_id: tok.id,
			amount: amount.toString(),
			raw: JSON.stringify({
				standard: mint ? 'nep245' : 'dip4',
				version: '1.0.0',
				event: ev,
				data: [
					mint
						? { owner_id: `${txHash(i).slice(0, 8).toLowerCase()}.near`, token_ids: [tok.id], amounts: [amount.toString()] }
						: { account_id: `${txHash(i + 9).slice(0, 8).toLowerCase()}.near`, token: tok.contract, amount: amount.toString() }
				]
			})
		});
	}
	return { ...inc, events, snapshots };
}

// ---------------------------------------------------------------------------
// Replay (synthetic shape only)
// ---------------------------------------------------------------------------

function replay() {
	const dec = 6;
	const h0 = 168_402_100;
	const labels = [];
	const points = [];
	const owedBase = 6_204_118.42;
	let deficit = 0;
	for (let i = 0; i < 34; i++) {
		const h = h0 + i * 50;
		let label = '';
		if (i < 6) label = 'before exploit';
		else if (i < 27) {
			if (i % 1 === 0 && i >= 7 && i <= 25) {
				deficit += 165_000 + rand(i) * 40_000;
				label = `failed deposit ${i - 6}`;
			} else label = 'exploit window';
		} else label = 'after pause';
		if (i === 30) label = 'funds returned';
		if (i >= 30) deficit = 0;
		const owed = units(owedBase + deficit + i * 140.3, dec, i);
		const held = owed - units(deficit, dec, i + 100) + units(212.4, dec, 9);
		const gap = held - owed;
		labels.push(label);
		const negStreak = (() => {
			let s = 0;
			for (let j = points.length - 1; j >= 0 && BigInt(points[j].gap) < 0n; j--) s++;
			return gap < 0n ? s + 1 : 0;
		})();
		points.push({
			block_height: h,
			block_time: iso(Date.UTC(2026, 9, 1, 2, 40, 0) + i * 30_000),
			owed: owed.toString(),
			held: held.toString(),
			gap: gap.toString(),
			label,
			status: gap >= 0n ? 'green' : negStreak >= K ? 'red' : 'amber',
			owed_query: `query call_function intents.near.mt_batch_supply({"token_ids":["nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near"]}) at block_id ${h}`,
			held_query: `query call_function eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near.ft_balance_of({"account_id":"intents.near"}) at block_id ${h}`
		});
	}
	const firstNeg = points.find((p) => BigInt(p.gap) < 0n);
	const firstRed = points.find((p) => p.status === 'red');
	const max = points.reduce((m, p) => (BigInt(p.gap) < -m ? -BigInt(p.gap) : m), 0n);
	return {
		title: 'NEAR Intents, 2026-10-01',
		generated_at: iso(Date.UTC(2026, 10, 2, 18, 0, 0)),
		rpc_provider: 'archival-rpc.mainnet.near.org',
		notes: 'SYNTHETIC DESIGN DATA from dev/mock-api.mjs. Not the real replay.',
		debounce_k: K,
		cadence_seconds: INTERVAL_S,
		tokens: [
			{
				id: 'nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near',
				symbol: 'USDC',
				chain: 'Ethereum',
				decimals: dec,
				points,
				first_negative_block: firstNeg.block_height,
				detected_red_block: firstRed.block_height,
				max_deficit: max.toString(),
				time_to_detect_seconds: (K - 1) * INTERVAL_S,
				detection_note: 'Synthetic: the debounce fires on the fourth consecutive negative sample.'
			}
		]
	};
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

function parseRangeBound(v, fallback) {
	if (v === null || v === undefined || v === '') return fallback;
	if (/^\d+$/.test(v)) return timeAt(Number(v));
	const t = Date.parse(v);
	return Number.isNaN(t) ? fallback : t;
}

function history(tok, from, to, limit, nowMs) {
	const step = INTERVAL_S * 1000;
	const start = snapFloor(from) + step;
	const end = Math.min(snapFloor(to), snapFloor(nowMs));
	const n = Math.max(1, Math.floor((end - start) / step) + 1);
	const stride = Math.max(1, Math.ceil(n / limit));
	const times = new Set();
	for (let ms = start; ms <= end; ms += step * stride) times.add(ms);
	times.add(end);
	// Always keep every snapshot with gap < 0 (as the real API does).
	for (const [s, e] of negativeWindows(tok, nowMs)) {
		for (let ms = snapFloor(s); ms < e && ms <= end; ms += step) if (ms >= start) times.add(ms);
	}
	return [...times].sort((a, b) => a - b).map((ms) => pointAt(tok, ms, nowMs));
}

function send(res, status, body, type = 'application/json') {
	res.writeHead(status, {
		'content-type': type,
		'access-control-allow-origin': '*',
		'cache-control': 'no-store',
		'x-argus-mock': 'synthetic'
	});
	res.end(type === 'application/json' ? JSON.stringify(body, null, 2) : body);
}

const server = http.createServer((req, res) => {
	const url = new URL(req.url, 'http://localhost');
	const p = url.pathname;
	const nowMs = now();

	if (p === '/__mock') {
		for (const [k, v] of url.searchParams) {
			if (k === 'scenario') cfg.scenario = v;
			if (k === 'replay') cfg.replay = v !== '0';
			if (k === 'stale') cfg.stale = v === '1';
			if (k === 'down') cfg.down = v === '1';
		}
		return send(res, 200, cfg);
	}
	if (cfg.down && p.startsWith('/v1')) return send(res, 503, 'Service Unavailable', 'text/plain');
	if (p === '/healthz') return send(res, 200, 'ok', 'text/plain');

	if (p === '/v1/status') {
		const tokens = allTokens(nowMs);
		const by = { green: 0, amber: 0, red: 0, unknown: 0 };
		for (const t of tokens) by[t.status]++;
		const worst = by.red ? 'red' : by.amber ? 'amber' : by.green ? 'green' : 'unknown';
		const latest = snapFloor(nowMs);
		return send(res, 200, {
			status: cfg.stale ? 'unknown' : worst,
			monitor_stale: cfg.stale,
			last_block: blockAt(latest),
			last_block_time: iso(latest),
			last_checked_at: iso(latest + 3400),
			interval_seconds: INTERVAL_S,
			debounce_snapshots: K,
			tokens_total: tokens.length,
			tokens_by_status: by,
			open_incidents: incidents(nowMs).filter((i) => !i.closed_at).length,
			providers: [
				{ name: 'fastnear', url: 'https://free.rpc.fastnear.com', healthy: true, last_error: '' },
				{ name: 'near', url: 'https://rpc.mainnet.near.org', healthy: true, last_error: '' }
			],
			version: '0.1.0-mock'
		});
	}
	if (p === '/v1/tokens') {
		return send(res, 200, { block_height: blockAt(snapFloor(nowMs)), tokens: allTokens(nowMs) });
	}
	let m = p.match(/^\/v1\/tokens\/([^/]+)(\/history)?$/);
	if (m) {
		const id = decodeURIComponent(m[1]);
		const tok = TOKENS.find((t) => t.id === id);
		if (!tok) return send(res, 404, { error: 'token not found' });
		if (m[2]) {
			const to = parseRangeBound(url.searchParams.get('to'), nowMs);
			const from = parseRangeBound(url.searchParams.get('from'), to - 86400_000);
			const limit = Math.min(10000, Number(url.searchParams.get('limit') ?? 2000) || 2000);
			const points = tok.id === UNKNOWN_ID ? [] : history(tok, from, to, limit, nowMs);
			return send(res, 200, { token: { id: tok.id, symbol: tok.symbol, chain: tok.chain, decimals: tok.decimals }, points });
		}
		const open = incidents(nowMs).find((i) => i.token_id === id && !i.closed_at) ?? null;
		return send(res, 200, { ...tokenObject(tok, nowMs), open_incident: open });
	}
	if (p === '/v1/incidents') {
		const status = url.searchParams.get('status') ?? 'all';
		let list = incidents(nowMs);
		if (status === 'open') list = list.filter((i) => !i.closed_at);
		if (status === 'closed') list = list.filter((i) => i.closed_at);
		return send(res, 200, { incidents: list });
	}
	m = p.match(/^\/v1\/incidents\/(\d+)$/);
	if (m) {
		const inc = incidents(nowMs).find((i) => i.id === Number(m[1]));
		if (!inc) return send(res, 404, { error: 'incident not found' });
		return send(res, 200, incidentDetail(inc, nowMs));
	}
	if (p === '/v1/replay') {
		if (!cfg.replay) return send(res, 404, { error: 'replay not available' });
		return send(res, 200, replay());
	}
	if (p === '/v1/export.csv') {
		const id = url.searchParams.get('token');
		const toks = id ? TOKENS.filter((t) => t.id === id) : TOKENS;
		const to = parseRangeBound(url.searchParams.get('to'), nowMs);
		const from = parseRangeBound(url.searchParams.get('from'), to - 86400_000);
		const lines = ['# SYNTHETIC DESIGN DATA (dev/mock-api.mjs)', 'token_id,symbol,decimals,block_height,block_time,owed,held,gap,provider'];
		for (const t of toks) {
			for (const pt of history(t, from, to, 100000, nowMs))
				lines.push([t.id, t.symbol, t.decimals, pt.block_height, pt.block_time, pt.owed, pt.held, pt.gap, 'fastnear'].join(','));
		}
		res.setHeader('content-disposition', `attachment; filename="argus-${id ? id.replace(/[^a-z0-9.-]+/gi, '_') : 'all'}.csv"`);
		return send(res, 200, lines.join('\n') + '\n', 'text/csv');
	}
	return send(res, 404, { error: 'not found' });
});

server.listen(cfg.port, () => {
	console.log(`\n  [mock-api] SYNTHETIC DESIGN DATA on http://localhost:${cfg.port}  (scenario=${cfg.scenario}, replay=${cfg.replay})\n`);
});

if (args.has('with-vite')) {
	const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
	// Run Vite through Node so the same command works on Windows and POSIX.
	const vite = spawn(process.execPath, [path.join(root, 'node_modules', 'vite', 'bin', 'vite.js'), 'dev'], { cwd: root, stdio: 'inherit' });
	const stop = () => {
		vite.kill('SIGTERM');
		server.close();
		process.exit(0);
	};
	vite.on('exit', (code) => {
		server.close();
		process.exit(code ?? 0);
	});
	process.on('SIGINT', stop);
	process.on('SIGTERM', stop);
}
