<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api, exportCsvUrl, type HistoryResponse, type TokenDetail } from '#lib/api.ts';
	import { monitor } from '#lib/monitor.svelte.ts';
	import { poll, Resource } from '#lib/live.svelte.ts';
	import {
		formatAmount,
		formatDuration,
		formatExact,
		formatInteger,
		formatPercent,
		formatUtc,
		parseAmount,
		ratioPercent
	} from '#lib/format.ts';
	import { explorer, href } from '#lib/links.ts';
	import StatusBadge from '#lib/components/StatusBadge.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import BlockLink from '#lib/components/BlockLink.svelte';
	import Amount from '#lib/components/Amount.svelte';
	import Ago from '#lib/components/Ago.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Banner from '#lib/components/Banner.svelte';
	import Segmented from '#lib/components/Segmented.svelte';
	import TokenChart from '#lib/components/TokenChart.svelte';
	import RpcProof from '#lib/components/RpcProof.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import OfflineBanner from '#lib/components/OfflineBanner.svelte';
	import StaleBanner from '#lib/components/StaleBanner.svelte';
	import { statusMeta } from '#lib/status.ts';
	import ChainChip from '#lib/components/ChainChip.svelte';
	import { chainLabel } from '#lib/chain.ts';
	import { summarizeReadError } from '#lib/errors.ts';

	type Range = '1h' | '24h' | '7d' | '30d';
	const RANGES: Record<Range, number> = { '1h': 3600_000, '24h': 86400_000, '7d': 7 * 86400_000, '30d': 30 * 86400_000 };

	const id = $derived(page.url.searchParams.get('id') ?? '');
	let range = $state<Range>('24h');
	let view = $state({ from: Date.now() - RANGES['24h'], to: Date.now() });

	const token = new Resource<TokenDetail>((s) => api.token(id, s));
	const history = new Resource<HistoryResponse>((s) => {
		const to = Date.now();
		const from = to - RANGES[range];
		return api.history(id, { from: new Date(from).toISOString(), to: new Date(to).toISOString(), limit: 2000 }, s).then((h) => {
			view = { from, to };
			return h;
		});
	});

	// Reload everything when the id changes; poll at the monitor's interval.
	$effect(() => {
		if (!id) return;
		token.reset();
		history.reset();
		// untrack: the first tick runs synchronously and must not subscribe this effect to `range`.
		return untrack(() => poll(() => Promise.all([token.load(), history.load()]), () => monitor.intervalMs));
	});

	// Range change: refetch history only, keeping the old chart dimmed meanwhile.
	let lastRange: Range = '24h';
	$effect(() => {
		if (range !== lastRange) {
			lastRange = range;
			history.load();
		}
	});

	const t = $derived(token.data);
	const latest = $derived(t?.latest ?? null);
	const gap = $derived(parseAmount(latest?.gap));
	const owed = $derived(parseAmount(latest?.owed));
	const pct = $derived(ratioPercent(gap, owed));
	const meta = $derived(statusMeta(t?.status));
	const rpcUrl = $derived(
		monitor.status.data?.providers.find((p) => p.name === latest?.provider)?.url ??
			monitor.status.data?.providers[0]?.url ??
			'https://free.rpc.fastnear.com'
	);
	const usdGap = $derived.by(() => {
		if (!t?.usd_price || gap === null) return null;
		const price = Number(t.usd_price);
		if (!Number.isFinite(price)) return null;
		// gap in tokens * price; display only.
		const tokens = Number(formatExact(gap, t.decimals));
		return tokens * price;
	});
	const notFound = $derived(token.error?.notFound ?? false);
</script>

<svelte:head>
	<title>{t ? `${t.symbol} (${chainLabel(t.id, t.chain)}) · Argus` : 'Token · Argus'}</title>
</svelte:head>

<div class="container page">
	<nav class="crumbs" aria-label="Breadcrumb">
		<a href={href.home()}><Icon name="arrowLeft" size={14} /> All tokens</a>
	</nav>

	{#if !id}
		<EmptyState icon="search" title="No token selected">
			Pick a token from the overview to see its history.
			{#snippet actions()}<a class="btn" href={href.home()}>Go to overview</a>{/snippet}
		</EmptyState>
	{:else if notFound}
		<EmptyState icon="search" title="Token not tracked">
			Argus does not track <code class="mono">{id}</code>. It may not be listed on intents.near, or it is an NFT
			(out of scope).
			{#snippet actions()}<a class="btn" href={href.home()}>Back to all tokens</a>{/snippet}
		</EmptyState>
	{:else}
		<header class="head">
			<div class="title">
				{#if t}
					<div class="title-row">
						<h1>{t.symbol}</h1>
						<ChainChip id={t.id} chain={t.chain} size="md" />
						<StatusBadge status={t.status} size="lg" />
					</div>
				{:else}
					<span class="skeleton" style="width:180px;height:34px"></span>
				{/if}
				<div class="tid">
					<code class="mono">{id}</code>
					<CopyButton value={id} label="Copy token id" />
				</div>
			</div>
			<div class="actions">
				<a class="btn" href={exportCsvUrl(id, { from: new Date(view.from).toISOString(), to: new Date(view.to).toISOString() })} download
					><Icon name="download" size={14} />CSV · {range}</a
				>
				{#if t}
					<a class="btn" href={explorer.account(t.contract)} target="_blank" rel="noopener noreferrer"
						>Contract <Icon name="external" size={13} /></a
					>
				{/if}
			</div>
		</header>

		<div class="stack">
			{#if token.error && !notFound}
				<OfflineBanner error={token.error} retry={() => Promise.all([token.load(), history.load()])} hasData={!!t} />
			{/if}
			{#if t?.stale}
				<Banner tone="warn" icon="clock" title="Latest read failed">
					{summarizeReadError(t.last_error)}
					{#if latest}
						Figures below are from the last successful snapshot (block {formatInteger(latest.block_height)}); the status stays
						as it was until a read succeeds.
					{:else}
						Argus has no successful snapshot of this token yet, so there are no figures to show.
					{/if}
					{#if t.last_error}
						<details class="raw-err">
							<summary>Raw error</summary>
							<pre>{t.last_error}</pre>
						</details>
					{/if}
				</Banner>
			{:else}
				<StaleBanner />
			{/if}

			{#if t?.open_incident}
				{@const inc = t.open_incident}
				<Banner tone="bad" title="Open incident #{inc.id}: the ledger owes more {t.symbol} than intents.near holds">
					Negative since block <BlockLink height={inc.start_block} />, red at <BlockLink height={inc.red_block} />
					(opened <Ago iso={inc.opened_at} />). Largest deficit so far
					<strong class="num">{formatAmount(inc.max_deficit, inc.decimals)} {t.symbol}</strong> at block
					<BlockLink height={inc.max_deficit_block} />.
					{#snippet action()}
						<a class="btn" href={href.incident(inc.id)}>View incident <Icon name="arrowRight" size={14} /></a>
					{/snippet}
				</Banner>
			{:else if t?.status === 'amber'}
				<Banner tone="warn" title="Gap below zero, settling">
					The latest {t.negative_streak} snapshot{t.negative_streak === 1 ? '' : 's'} showed held &lt; owed. This is often
					a deposit or withdrawal in flight; it turns red after {monitor.status.data?.debounce_snapshots ?? 4} consecutive
					negative snapshots.
				</Banner>
			{/if}

			<!-- Current numbers -->
			<section class="card stats" aria-label="Latest snapshot">
				{#if t && latest}
					<div class="stat">
						<p class="label">Owed <span>ledger total, mt_supply</span></p>
						<p class="value"><Amount value={latest.owed} decimals={t.decimals} symbol={t.symbol} /><span class="unit">{t.symbol}</span></p>
						<p class="raw"><span class="mono" title={latest.owed}>{latest.owed}</span><CopyButton value={latest.owed} label="Copy raw owed" /></p>
					</div>
					<div class="stat">
						<p class="label">Held <span>balance of intents.near</span></p>
						<p class="value"><Amount value={latest.held} decimals={t.decimals} symbol={t.symbol} /><span class="unit">{t.symbol}</span></p>
						<p class="raw"><span class="mono" title={latest.held}>{latest.held}</span><CopyButton value={latest.held} label="Copy raw held" /></p>
					</div>
					<div class="stat gapstat {meta.tone}">
						<p class="label">Gap <span>held − owed</span></p>
						<p class="value">
							<Amount value={latest.gap} decimals={t.decimals} symbol={t.symbol} sign tone /><span class="unit">{t.symbol}</span>
						</p>
						<p class="sub">
							<span class="num pct">{formatPercent(pct)}</span> of owed
							{#if usdGap !== null}<span class="num"> · ≈ ${Math.abs(usdGap).toLocaleString('en-US', { maximumFractionDigits: 0 })}</span>{/if}
						</p>
						<p class="raw"><span class="mono" title={latest.gap}>{latest.gap}</span><CopyButton value={latest.gap} label="Copy raw gap" /></p>
					</div>
					<div class="stat-foot">
						<span>Snapshot at block <BlockLink height={latest.block_height} /></span>
						<span class="sep">·</span>
						<span>{formatUtc(latest.block_time)}</span>
						<span class="sep">·</span>
						<span><Ago iso={latest.block_time} /></span>
						{#if latest.provider}<span class="sep">·</span><span>via <span class="mono">{latest.provider}</span></span>{/if}
					</div>
				{:else if t && !latest}
					<div class="stat-empty">
						<Icon name="clock" size={16} />
						<p>
							<strong>No successful snapshot yet.</strong>
							Owed, held and gap appear here after the first same-block read of {t.symbol} succeeds. Argus retries every
							{formatDuration(monitor.intervalMs / 1000)}.
						</p>
					</div>
				{:else}
					{#each Array(3) as _, i (i)}
						<div class="stat">
							<span class="skeleton" style="width:90px;height:10px"></span>
							<span class="skeleton" style="width:160px;height:28px;margin-top:14px"></span>
							<span class="skeleton" style="width:200px;height:10px;margin-top:14px"></span>
						</div>
					{/each}
				{/if}
			</section>

			<!-- Chart -->
			<section class="card chart-card" aria-labelledby="chart-h">
				<div class="chart-head">
					<div>
						<h2 id="chart-h">Owed vs held</h2>
						<ul class="legend" aria-label="Legend">
							<li><i class="k owed"></i>Owed (ledger)</li>
							<li><i class="k held"></i>Held (contract balance)</li>
							<li><i class="k area pos"></i>Surplus</li>
							<li><i class="k area neg"></i>Deficit</li>
						</ul>
					</div>
					<Segmented
						label="Time range"
						size="sm"
						bind:value={range}
						options={[
							{ value: '1h', label: '1H' },
							{ value: '24h', label: '24H' },
							{ value: '7d', label: '7D' },
							{ value: '30d', label: '30D' }
						]}
					/>
				</div>
				{#if history.data && t}
					<TokenChart
						points={history.data.points}
						decimals={t.decimals}
						symbol={t.symbol}
						from={view.from}
						to={view.to}
						busy={history.loading}
					/>
					<p class="chart-foot" hidden={history.data.points.length === 0}>
						{formatInteger(history.data.points.length)} snapshots. Long ranges are downsampled evenly, but every snapshot with a
						negative gap is kept. Hover or focus the chart and use ← → to inspect points, <kbd>N</kbd> for the next negative one.
					</p>
				{:else if history.error}
					<EmptyState icon="offline" title="History unavailable" compact>{history.error.message}</EmptyState>
				{:else}
					<div class="chart-sk" aria-busy="true">
						<span class="skeleton" style="height:220px"></span>
						<span class="skeleton" style="height:110px;margin-top:36px"></span>
					</div>
				{/if}
			</section>

			<div class="cols">
				<section class="card meta" aria-labelledby="meta-h">
					<h2 id="meta-h">Token</h2>
					{#if t}
						<dl>
							<div><dt>Symbol</dt><dd>{t.symbol}</dd></div>
							<div><dt>Chain</dt><dd>{chainLabel(t.id, t.chain)}</dd></div>
							<div><dt>Standard</dt><dd><span class="tag mono">{t.standard}</span></dd></div>
							<div>
								<dt>Contract</dt>
								<dd class="wrap">
									<a class="link mono" href={explorer.account(t.contract)} target="_blank" rel="noopener noreferrer">{t.contract}</a>
									<CopyButton value={t.contract} label="Copy contract" />
								</dd>
							</div>
							{#if t.inner_id}
								<div><dt>Inner id</dt><dd class="wrap"><span class="mono">{t.inner_id}</span><CopyButton value={t.inner_id} label="Copy inner id" /></dd></div>
							{/if}
							<div><dt>Decimals</dt><dd class="num">{t.decimals} <span class="muted">· 1 {t.symbol} = 10<sup>{t.decimals}</sup> units</span></dd></div>
							<div><dt>Negative streak</dt><dd class="num">{t.negative_streak} snapshot{t.negative_streak === 1 ? '' : 's'}</dd></div>
							<div><dt>Holder checked</dt><dd><a class="link mono" href={explorer.account('intents.near')} target="_blank" rel="noopener noreferrer">intents.near</a></dd></div>
						</dl>
					{:else}
						{#each Array(5) as _, i (i)}
							<span class="skeleton" style="height:14px;margin-top:14px;width:{60 + i * 7}%"></span>
						{/each}
					{/if}
				</section>

				<section class="card verify" aria-labelledby="verify-h">
					<details open>
						<summary>
							<span>
								<h2 id="verify-h">How this number is computed</h2>
								<span class="muted">Don't trust, verify: the exact RPC calls behind this snapshot.</span>
							</span>
							<Icon name="chevronDown" size={16} />
						</summary>
						{#if t}
							<div class="verify-body"><RpcProof token={t} snapshot={latest} {rpcUrl} /></div>
						{/if}
					</details>
				</section>
			</div>
		</div>
	{/if}
</div>

<style>
	.page {
		padding-top: var(--s-6);
	}
	.crumbs a {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: var(--text-sm);
		color: var(--text-3);
		font-weight: 500;
	}
	.crumbs a:hover {
		color: var(--text);
	}
	.head {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: var(--s-4);
		flex-wrap: wrap;
		padding: var(--s-6) 0 var(--s-6);
	}
	.title {
		min-width: 0;
		flex: 1 1 400px;
	}
	.title-row {
		display: flex;
		align-items: center;
		gap: var(--s-3);
		flex-wrap: wrap;
	}
	h1 {
		font-size: var(--text-3xl);
		font-weight: 650;
		letter-spacing: -0.032em;
	}
	.tid {
		display: flex;
		align-items: center;
		gap: 4px;
		margin-top: var(--s-2);
		min-width: 0;
	}
	.tid code {
		font-size: var(--text-sm);
		color: var(--text-2);
		word-break: break-all;
	}
	.actions {
		display: flex;
		gap: var(--s-2);
		flex-wrap: wrap;
	}
	.stack {
		display: grid;
		gap: var(--s-4);
	}

	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
	}
	.stat {
		padding: var(--s-6);
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.stat + .stat {
		border-left: 1px solid var(--border);
	}
	.label {
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
	}
	.label span {
		color: var(--text-4);
		font-weight: 450;
		margin-left: 4px;
	}
	.value {
		margin-top: var(--s-2);
		font-size: var(--text-2xl);
		font-weight: 600;
		letter-spacing: -0.02em;
		line-height: 1.2;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.value :global(.amount) {
		font-variant-numeric: normal;
	}
	.unit {
		margin-left: 6px;
		font-size: var(--text-sm);
		color: var(--text-3);
		font-weight: 500;
		letter-spacing: 0;
	}
	.sub {
		font-size: var(--text-xs);
		color: var(--text-3);
		margin-top: 2px;
	}
	.gapstat.bad .pct,
	.gapstat.warn .pct {
		color: var(--bad);
		font-weight: 600;
	}
	.gapstat.bad {
		background: color-mix(in srgb, var(--bad-bg) 60%, transparent);
	}
	.raw {
		display: flex;
		align-items: center;
		gap: 2px;
		margin-top: auto;
		padding-top: var(--s-3);
		font-size: 11.5px;
		color: var(--text-3);
		min-width: 0;
	}
	.raw .mono {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: 11.5px;
	}
	.stat-foot {
		grid-column: 1 / -1;
		display: flex;
		flex-wrap: wrap;
		gap: 4px 8px;
		padding: 10px var(--s-6);
		border-top: 1px solid var(--border);
		background: var(--surface-2);
		font-size: var(--text-xs);
		color: var(--text-3);
		border-radius: 0 0 var(--r-lg) var(--r-lg);
	}
	.sep {
		color: var(--text-4);
	}
	.stat-empty {
		grid-column: 1 / -1;
		display: flex;
		gap: var(--s-3);
		align-items: flex-start;
		padding: var(--s-5) var(--s-6);
		color: var(--text-3);
		font-size: var(--text-sm);
	}
	.stat-empty :global(svg) {
		flex: none;
		margin-top: 2px;
	}
	.stat-empty strong {
		color: var(--text);
		font-weight: 600;
	}
	.raw-err {
		margin-top: var(--s-2);
	}
	.raw-err summary {
		cursor: pointer;
		width: max-content;
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
	}
	.raw-err pre {
		margin: 6px 0 0;
		padding: 8px 10px;
		border-radius: var(--r-sm);
		background: var(--surface);
		border: 1px solid var(--warn-border);
		font-size: 11.5px;
		line-height: 1.55;
		color: var(--text-2);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 220px;
		overflow-y: auto;
	}

	.chart-card {
		padding: var(--s-6);
	}
	.chart-head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: var(--s-4);
		flex-wrap: wrap;
		margin-bottom: var(--s-5);
	}
	h2 {
		font-size: var(--text-md);
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 16px;
		margin-top: 6px;
		font-size: var(--text-xs);
		color: var(--text-2);
	}
	.legend li {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.k {
		display: inline-block;
		width: 14px;
		height: 2px;
		border-radius: 1px;
	}
	.k.owed {
		background: var(--series-owed);
	}
	.k.held {
		background: var(--series-held);
	}
	.k.area {
		height: 10px;
		width: 10px;
		border-radius: 2px;
	}
	.k.area.pos {
		background: color-mix(in srgb, var(--gap-pos) 22%, transparent);
		box-shadow: inset 0 -2px 0 var(--gap-pos);
	}
	.k.area.neg {
		background: color-mix(in srgb, var(--gap-neg) 26%, transparent);
		box-shadow: inset 0 2px 0 var(--gap-neg);
	}
	.chart-foot {
		margin-top: var(--s-4);
		font-size: var(--text-xs);
		color: var(--text-3);
		line-height: var(--leading-loose);
	}
	.chart-sk {
		display: flex;
		flex-direction: column;
		padding-left: 58px;
	}

	.cols {
		display: grid;
		grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
		gap: var(--s-4);
		align-items: start;
	}
	.meta {
		padding: var(--s-6);
	}
	.meta dl {
		margin-top: var(--s-4);
		display: grid;
	}
	.meta dl div {
		display: grid;
		grid-template-columns: 120px minmax(0, 1fr);
		gap: var(--s-3);
		padding: 9px 0;
		border-top: 1px solid var(--border-soft);
		font-size: var(--text-sm);
		align-items: center;
	}
	.meta dt {
		color: var(--text-3);
	}
	.meta dd {
		min-width: 0;
	}
	.meta .wrap {
		display: flex;
		align-items: center;
		gap: 2px;
		min-width: 0;
	}
	.meta .wrap > :first-child {
		word-break: break-all;
		font-size: 12.5px;
	}
	.tag {
		display: inline-block;
		padding: 1px 6px;
		border-radius: var(--r-xs);
		background: var(--surface-3);
		font-size: 12px;
	}
	sup {
		font-size: 0.7em;
	}
	.verify details > summary {
		list-style: none;
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--s-3);
		padding: var(--s-6);
		cursor: pointer;
	}
	.verify details > summary::-webkit-details-marker {
		display: none;
	}
	.verify summary > span {
		display: grid;
		gap: 4px;
	}
	.verify summary .muted {
		font-size: var(--text-sm);
	}
	.verify summary :global(svg) {
		color: var(--text-3);
		transition: transform var(--dur) var(--ease);
		margin-top: 2px;
	}
	.verify details[open] > summary :global(svg) {
		transform: rotate(180deg);
	}
	.verify-body {
		padding: 0 var(--s-6) var(--s-6);
	}

	@media (max-width: 960px) {
		.cols {
			grid-template-columns: 1fr;
		}
	}
	@media (max-width: 720px) {
		.stats {
			grid-template-columns: 1fr;
		}
		.stat + .stat {
			border-left: 0;
			border-top: 1px solid var(--border);
		}
		.stat {
			padding: var(--s-4) var(--s-5);
		}
		.value {
			font-size: var(--text-xl);
		}
		.chart-card,
		.meta {
			padding: var(--s-4);
		}
		.verify details > summary {
			padding: var(--s-4);
		}
		.verify-body {
			padding: 0 var(--s-4) var(--s-4);
		}
		.stat-foot {
			padding: 10px var(--s-5);
		}
		h1 {
			font-size: var(--text-2xl);
		}
		.meta dl div {
			grid-template-columns: 100px minmax(0, 1fr);
		}
	}
</style>
