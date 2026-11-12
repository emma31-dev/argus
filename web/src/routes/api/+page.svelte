<script lang="ts">
	import { onMount } from 'svelte';
	import { API_BASE } from '#lib/api.ts';
	import { API_DOC_URL } from '#lib/links.ts';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Code from '#lib/components/Code.svelte';
	import Icon from '#lib/components/Icon.svelte';

	let origin = $state('https://<argus-host>');
	onMount(() => {
		origin = API_BASE || window.location.origin;
	});

	interface Param {
		name: string;
		desc: string;
	}
	interface Endpoint {
		id: string;
		path: string;
		title: string;
		desc: string;
		params?: Param[];
		example?: string;
		response?: string;
		note?: string;
		type?: string;
		tryPath?: string;
	}

	const endpoints: Endpoint[] = [
		{
			id: 'status',
			path: '/v1/status',
			title: 'Monitor status',
			desc: 'Overall health plus a summary. `status` is the worst token status: green | amber | red | unknown.',
			tryPath: '/v1/status',
			response: `{
  "status": "green",
  "last_block": 171234567,
  "last_block_time": "2026-11-03T12:00:00Z",
  "last_checked_at": "2026-11-03T12:00:04Z",
  "interval_seconds": 30,
  "debounce_snapshots": 4,
  "tokens_total": 112,
  "tokens_by_status": {"green": 110, "amber": 1, "red": 0, "unknown": 1},
  "open_incidents": 0,
  "providers": [
    {"name": "fastnear", "url": "https://free.rpc.fastnear.com", "healthy": true, "last_error": ""}
  ],
  "version": "0.1.0"
}`
		},
		{
			id: 'tokens',
			path: '/v1/tokens',
			title: 'All tokens',
			desc: 'Every tracked token with its latest snapshot, sorted by deficit risk: red, amber, unknown, green; within a status by gap relative to owed, most negative first. `latest` is null for a token with no successful snapshot yet.',
			tryPath: '/v1/tokens',
			response: `{
  "block_height": 171234567,
  "tokens": [
    {
      "id": "nep141:wrap.near",
      "standard": "nep141",
      "contract": "wrap.near",
      "inner_id": "",
      "symbol": "wNEAR",
      "decimals": 24,
      "chain": "NEAR",
      "status": "green",
      "negative_streak": 0,
      "stale": false,
      "last_error": "",
      "latest": {
        "block_height": 171234567,
        "block_time": "2026-11-03T12:00:00Z",
        "owed": "123456789000000000000000000000",
        "held": "123456789500000000000000000000",
        "gap":  "500000000000000000000",
        "provider": "fastnear"
      },
      "usd_price": "3.21"
    }
  ]
}`,
			note: '`chain` labels the source network (symbols repeat across chains). `stale` is true when the most recent read failed; the status is kept from the last good read. `usd_price` is optional and display-only.'
		},
		{
			id: 'token',
			path: '/v1/tokens/{id}',
			title: 'One token',
			desc: 'Same object as one element of /v1/tokens, plus `open_incident` (an Incident or null). `{id}` is the URL-escaped full token id.',
			example: '/v1/tokens/nep141%3Awrap.near',
			tryPath: '/v1/tokens/nep141%3Awrap.near'
		},
		{
			id: 'history',
			path: '/v1/tokens/{id}/history',
			title: 'Token history',
			desc: 'Snapshots for one token. When the range has more points than `limit`, the server downsamples evenly but always keeps every snapshot with gap < 0.',
			params: [
				{ name: 'from', desc: 'RFC 3339 time or block height. Default: 24 h before `to`.' },
				{ name: 'to', desc: 'RFC 3339 time or block height. Default: now.' },
				{ name: 'limit', desc: 'Default 2000, max 10000.' }
			],
			example: '/v1/tokens/nep141%3Awrap.near/history?from=2026-11-02T00:00:00Z&limit=500',
			tryPath: '/v1/tokens/nep141%3Awrap.near/history?limit=50',
			response: `{
  "token": {"id": "nep141:wrap.near", "symbol": "wNEAR", "decimals": 24},
  "points": [
    {"block_height": 171234500, "block_time": "2026-11-03T11:59:30Z",
     "owed": "...", "held": "...", "gap": "..."}
  ]
}`
		},
		{
			id: 'incidents',
			path: '/v1/incidents',
			title: 'Incidents',
			desc: 'Red periods, newest first.',
			params: [
				{ name: 'status', desc: 'open | closed | all (default all).' },
				{ name: 'limit', desc: 'Maximum incidents returned.' }
			],
			tryPath: '/v1/incidents?status=all',
			response: `{
  "incidents": [
    {
      "id": 7,
      "token_id": "nep141:eth.omft.near",
      "symbol": "ETH",
      "decimals": 18,
      "start_block": 170001000,
      "red_block": 170001004,
      "end_block": 170001100,
      "opened_at": "2026-10-01T03:12:00Z",
      "closed_at": "2026-10-01T04:00:00Z",
      "duration_seconds": 2880,
      "max_deficit": "1000000000000000000",
      "max_deficit_block": 170001090,
      "summary": ""
    }
  ]
}`
		},
		{
			id: 'incident',
			path: '/v1/incidents/{id}',
			title: 'One incident',
			desc: 'The incident plus `events` (intents.near NEP-245 and DIP-4 events in its block range: block_height, receipt_id, tx_hash, standard, event, token_id, amount, raw) and `snapshots` for its block range.',
			example: '/v1/incidents/7'
		},
		{
			id: 'export',
			path: '/v1/export.csv',
			title: 'CSV export',
			type: 'text/csv',
			desc: 'Raw history as CSV. Without `token`, all tokens. Same range rules as history, no downsampling, max 100000 rows.',
			params: [
				{ name: 'token', desc: 'Full token id (optional).' },
				{ name: 'from / to', desc: 'RFC 3339 times or block heights.' }
			],
			response: `token_id,symbol,decimals,block_height,block_time,owed,held,gap,provider`
		},
		{
			id: 'replay',
			path: '/v1/replay',
			title: 'October 2026 replay',
			desc: 'The incident replay result, loaded from data/replay/2026-10-01.json. Returns 404 {"error":"replay not available"} until that file exists.',
			tryPath: '/v1/replay'
		},
		{
			id: 'badge',
			path: '/v1/badge/{id}.svg',
			title: 'Status badge (P1)',
			type: 'image/svg+xml',
			desc: 'Shields-style SVG badge for one token, or /v1/badge/all.svg for the overall status.'
		},
		{
			id: 'health',
			path: '/healthz',
			title: 'Health',
			type: 'text/plain',
			desc: '200 ok when the process is up. /readyz returns 200 once at least one snapshot round has completed.'
		}
	];

	function md(s: string) {
		// Tiny inline formatter: `code` only. Content is static, authored above.
		return s.split(/(`[^`]+`)/g).map((part) => (part.startsWith('`') ? { code: true, t: part.slice(1, -1) } : { code: false, t: part }));
	}
</script>

<svelte:head><title>API · Argus</title></svelte:head>

<div class="container">
	<PageHeader eyebrow="Reference · v1" title="Public API">
		Read-only JSON over HTTP. No auth, no keys: the data is public. Everything on this dashboard comes from these
		endpoints, so anything you see here you can fetch, script and verify.
		{#snippet aside()}
			<a class="btn" href={API_DOC_URL} target="_blank" rel="noopener noreferrer">docs/api.md <Icon name="external" size={13} /></a>
		{/snippet}
	</PageHeader>

	<div class="layout">
		<nav class="toc" aria-label="Endpoints">
			<p class="eyebrow">Endpoints</p>
			<ul>
				<li><a href="#conventions">Conventions</a></li>
				{#each endpoints as e (e.id)}
					<li><a href="#{e.id}"><span class="mono">{e.path}</span></a></li>
				{/each}
			</ul>
		</nav>

		<div class="content">
			<section id="conventions" class="card conv">
				<h2>Conventions</h2>
				<dl>
					<div>
						<dt>Base URL</dt>
						<dd><code class="mono">{origin}</code></dd>
					</div>
					<div>
						<dt>Amounts</dt>
						<dd>
							Base-10 <strong>integer strings in the token's smallest unit</strong> (wNEAR: 10<sup>−24</sup>). Format with
							<code>decimals</code>; parse with a big-integer type, never a float. <code>gap = held − owed</code> and may be negative.
						</dd>
					</div>
					<div><dt>Time</dt><dd>RFC 3339, UTC.</dd></div>
					<div>
						<dt>Errors</dt>
						<dd><code>{'{"error": "<message>"}'}</code> with a 4xx or 5xx status.</dd>
					</div>
					<div>
						<dt>Rate limit</dt>
						<dd>60 requests per minute per IP by default. Over the limit: <code>429</code> with <code>Retry-After</code>.</dd>
					</div>
					<div><dt>CORS</dt><dd><code>Access-Control-Allow-Origin: *</code></dd></div>
				</dl>
				<Code label="Quick start" lang="sh" code={`curl -s ${origin}/v1/status | jq '.status, .last_block'`} />
			</section>

			{#each endpoints as e (e.id)}
				<section id={e.id} class="ep">
					<div class="ep-head">
						<span class="method">GET</span>
						<code class="path">{e.path}</code>
						{#if e.type}<span class="ctype mono">{e.type}</span>{/if}
						{#if e.tryPath}
							<a class="try" href={`${API_BASE}${e.tryPath}`} target="_blank" rel="noopener noreferrer">Open <Icon name="external" size={12} /></a>
						{/if}
					</div>
					<h3>{e.title}</h3>
					<p class="desc">
						{#each md(e.desc) as part, i (i)}{#if part.code}<code>{part.t}</code>{:else}{part.t}{/if}{/each}
					</p>
					{#if e.params}
						<table class="params">
							<thead><tr><th scope="col">Query parameter</th><th scope="col">Description</th></tr></thead>
							<tbody>
								{#each e.params as p (p.name)}
									<tr><td><code>{p.name}</code></td><td>{p.desc}</td></tr>
								{/each}
							</tbody>
						</table>
					{/if}
					{#if e.example}<Code label="Example" code={`GET ${e.example}`} />{/if}
					{#if e.response}<Code label={e.type === 'text/csv' ? 'Header row' : 'Response'} lang={e.type === 'text/csv' ? 'csv' : 'json'} code={e.response} />{/if}
					{#if e.note}
						<p class="note">
							{#each md(e.note) as part, i (i)}{#if part.code}<code>{part.t}</code>{:else}{part.t}{/if}{/each}
						</p>
					{/if}
				</section>
			{/each}
		</div>
	</div>
</div>

<style>
	.layout {
		display: grid;
		grid-template-columns: 220px minmax(0, 1fr);
		gap: var(--s-12);
		align-items: start;
	}
	.toc {
		position: sticky;
		top: calc(var(--nav-h) + var(--s-6));
	}
	.toc ul {
		margin-top: var(--s-3);
		display: grid;
		gap: 1px;
		border-left: 1px solid var(--border);
	}
	.toc a {
		display: block;
		padding: 5px 0 5px 14px;
		margin-left: -1px;
		border-left: 1px solid transparent;
		font-size: var(--text-sm);
		color: var(--text-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.toc a .mono {
		font-size: 12px;
	}
	.toc a:hover {
		color: var(--text);
		border-left-color: var(--text);
	}
	.content {
		max-width: 860px;
		min-width: 0;
		display: grid;
		gap: var(--s-10);
	}
	section {
		scroll-margin-top: calc(var(--nav-h) + var(--s-6));
	}
	.conv {
		padding: var(--s-6);
		display: grid;
		gap: var(--s-5);
	}
	.conv h2 {
		font-size: var(--text-lg);
	}
	.conv dl {
		display: grid;
	}
	.conv dl div {
		display: grid;
		grid-template-columns: 120px minmax(0, 1fr);
		gap: var(--s-4);
		padding: 10px 0;
		border-top: 1px solid var(--border-soft);
		font-size: var(--text-sm);
	}
	.conv dt {
		color: var(--text-3);
	}
	.conv dd {
		color: var(--text-2);
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.conv dd strong {
		color: var(--text);
		font-weight: 600;
	}
	code {
		font-size: 0.88em;
		color: var(--text);
	}
	.desc code,
	.note code,
	.conv dd code,
	.params code {
		background: var(--surface-2);
		border: 1px solid var(--border-soft);
		padding: 1px 5px;
		border-radius: var(--r-xs);
	}
	.ep {
		display: grid;
		gap: var(--s-3);
		padding-top: var(--s-8);
		border-top: 1px solid var(--border);
	}
	.ep-head {
		display: flex;
		align-items: center;
		gap: var(--s-2);
		flex-wrap: wrap;
	}
	.method {
		display: inline-flex;
		align-items: center;
		height: 22px;
		padding: 0 7px;
		border-radius: var(--r-xs);
		background: var(--ok-bg);
		border: 1px solid var(--ok-border);
		color: var(--ok);
		font-family: var(--font-mono);
		font-size: 11px;
		font-weight: 600;
	}
	.path {
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.ctype {
		font-size: 11px;
		color: var(--text-3);
		padding: 2px 6px;
		border: 1px solid var(--border);
		border-radius: var(--r-xs);
	}
	.try {
		margin-left: auto;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
	}
	.try:hover {
		color: var(--text);
	}
	h3 {
		font-size: var(--text-xl);
		font-weight: 620;
	}
	.desc,
	.note {
		color: var(--text-2);
		line-height: var(--leading-loose);
	}
	.note {
		font-size: var(--text-sm);
	}
	.params {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-sm);
		border: 1px solid var(--border);
		border-radius: var(--r);
		overflow: hidden;
	}
	.params th {
		text-align: left;
		font-weight: 500;
		font-size: var(--text-xs);
		color: var(--text-3);
		background: var(--surface-2);
		padding: 8px 12px;
		border-bottom: 1px solid var(--border);
	}
	.params td {
		padding: 9px 12px;
		border-bottom: 1px solid var(--border-soft);
		color: var(--text-2);
		vertical-align: top;
	}
	.params tr:last-child td {
		border-bottom: 0;
	}
	.params td:first-child {
		white-space: nowrap;
		width: 1%;
	}
	@media (max-width: 960px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.toc {
			display: none;
		}
	}
	@media (max-width: 560px) {
		.conv {
			padding: var(--s-4);
		}
		.conv dl div {
			grid-template-columns: 1fr;
			gap: 2px;
		}
	}
</style>
