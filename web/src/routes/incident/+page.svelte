<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api, type IncidentDetail, type IncidentEvent } from '$lib/api';
	import { monitor } from '$lib/monitor.svelte';
	import { clock, poll, Resource } from '$lib/live.svelte';
	import { formatAmount, formatDuration, formatInteger, formatUtc, middleEllipsis } from '$lib/format';
	import { explorer, href } from '$lib/links';
	import Icon from '$lib/components/Icon.svelte';
	import IncidentBadge from '$lib/components/IncidentBadge.svelte';
	import BlockLink from '$lib/components/BlockLink.svelte';
	import Amount from '$lib/components/Amount.svelte';
	import CopyButton from '$lib/components/CopyButton.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import OfflineBanner from '$lib/components/OfflineBanner.svelte';
	import GapChart, { type GapMarker } from '$lib/components/GapChart.svelte';
	import Code from '$lib/components/Code.svelte';
	import ChainChip from '$lib/components/ChainChip.svelte';

	const id = $derived(page.url.searchParams.get('id') ?? '');
	const inc = new Resource<IncidentDetail>((s) => api.incident(id, s));

	$effect(() => {
		if (!id) return;
		inc.reset();
		return untrack(() => poll(() => inc.load(), () => (inc.data && inc.data.closed_at ? 10 * 60_000 : monitor.intervalMs)));
	});

	const d = $derived(inc.data);
	const open = $derived(!!d && !d.closed_at);
	const durationS = $derived.by(() => {
		if (!d) return null;
		if (!open && d.duration_seconds != null) return d.duration_seconds;
		return (clock.now - new Date(d.opened_at).getTime()) / 1000;
	});

	const markers = $derived.by<GapMarker[]>(() => {
		if (!d) return [];
		const m: GapMarker[] = [
			{ block: d.start_block, label: 'First negative', tone: 'warn' },
			{ block: d.red_block, label: 'Turned red', tone: 'bad' }
		];
		if (d.max_deficit_block && d.max_deficit_block !== d.red_block)
			m.push({ block: d.max_deficit_block, label: 'Max deficit', tone: 'neutral' });
		if (d.end_block) m.push({ block: d.end_block, label: 'Recovered', tone: 'ok' });
		return m;
	});

	type Item =
		| { kind: 'milestone'; block: number; title: string; tone: 'warn' | 'bad' | 'ok' | 'neutral'; text: string }
		| { kind: 'event'; block: number; ev: IncidentEvent };

	const timeline = $derived.by<Item[]>(() => {
		if (!d) return [];
		const items: Item[] = [
			{
				kind: 'milestone',
				block: d.start_block,
				title: 'Gap went negative',
				tone: 'warn',
				text: 'First snapshot of the streak where held < owed. Status amber.'
			},
			{
				kind: 'milestone',
				block: d.red_block,
				title: 'Turned red, incident opened',
				tone: 'bad',
				text: `Still negative after ${monitor.status.data?.debounce_snapshots ?? 4} consecutive snapshots. Alerts sent.`
			}
		];
		if (d.max_deficit_block)
			items.push({
				kind: 'milestone',
				block: d.max_deficit_block,
				title: `Largest deficit: ${formatAmount(d.max_deficit, d.decimals)} ${d.symbol}`,
				tone: 'neutral',
				text: 'Peak shortfall observed during the incident.'
			});
		if (d.end_block)
			items.push({
				kind: 'milestone',
				block: d.end_block,
				title: 'Recovered, incident closed',
				tone: 'ok',
				text: 'held ≥ owed again.'
			});
		for (const ev of d.events ?? []) items.push({ kind: 'event', block: ev.block_height, ev });
		return items.sort((a, b) => a.block - b.block || (a.kind === 'milestone' ? -1 : 1));
	});

	function prettyRaw(raw: string) {
		try {
			return JSON.stringify(JSON.parse(raw), null, 2);
		} catch {
			return raw;
		}
	}
	const EVENT_LABEL: Record<string, string> = {
		mt_mint: 'Ledger credit (mint)',
		mt_burn: 'Ledger debit (burn)',
		mt_transfer: 'Internal transfer',
		ft_withdraw: 'Withdrawal (FT)',
		mt_withdraw: 'Withdrawal (MT)',
		native_withdraw: 'Withdrawal (native)',
		token_diff: 'Swap (token_diff)'
	};
</script>

<svelte:head><title>{d ? `Incident #${d.id} · ${d.symbol} · Argus` : 'Incident · Argus'}</title></svelte:head>

<div class="container page">
	<nav class="crumbs" aria-label="Breadcrumb">
		<a href={href.incidents()}><Icon name="arrowLeft" size={14} /> Incidents</a>
	</nav>

	{#if !id}
		<EmptyState icon="search" title="No incident selected">
			{#snippet actions()}<a class="btn" href={href.incidents()}>All incidents</a>{/snippet}
		</EmptyState>
	{:else if inc.error?.notFound}
		<EmptyState icon="search" title="Incident not found">
			There is no incident #{id}.
			{#snippet actions()}<a class="btn" href={href.incidents()}>All incidents</a>{/snippet}
		</EmptyState>
	{:else}
		<header class="head">
			{#if d}
				<div class="title-row">
					<h1>Incident <span class="num">#{d.id}</span></h1>
					<IncidentBadge {open} />
				</div>
				<p class="lede">
					<a class="link" href={href.token(d.token_id)}><strong>{d.symbol}</strong></a> <ChainChip id={d.token_id} />: the intents.near ledger owed more than
					the contract held, for longer than the settling window.
					<span class="tid mono">{d.token_id}</span>
				</p>
			{:else}
				<span class="skeleton" style="width:240px;height:34px"></span>
				<span class="skeleton" style="width:60%;height:14px;margin-top:14px"></span>
			{/if}
		</header>

		{#if inc.error}
			<div class="mb"><OfflineBanner error={inc.error} retry={() => inc.load()} hasData={!!d} /></div>
		{/if}

		{#if d}
			<section class="card facts" aria-label="Incident summary">
				<div class="fact wide">
					<p class="k">Max deficit</p>
					<p class="v bad"><Amount value={d.max_deficit} decimals={d.decimals} symbol={d.symbol} /><span class="unit">{d.symbol}</span></p>
					<p class="s">at block <BlockLink height={d.max_deficit_block} /></p>
				</div>
				<div class="fact">
					<p class="k">Duration</p>
					<p class="v num">{formatDuration(durationS)}</p>
					<p class="s">{open ? 'ongoing' : 'red until recovery'}</p>
				</div>
				<div class="fact">
					<p class="k">Opened</p>
					<p class="v sm num">{formatUtc(d.opened_at, { seconds: false }).replace(' UTC', '')}</p>
					<p class="s">UTC</p>
				</div>
				<div class="fact">
					<p class="k">Closed</p>
					<p class="v sm num">{d.closed_at ? formatUtc(d.closed_at, { seconds: false }).replace(' UTC', '') : 'Open'}</p>
					<p class="s">{d.closed_at ? 'UTC' : 'still negative'}</p>
				</div>
				<div class="blocks">
					<span class="b"><span class="bl">First negative</span><BlockLink height={d.start_block} /></span>
					<span class="chev"><Icon name="chevronRight" size={14} /></span>
					<span class="b"><span class="bl">Red</span><BlockLink height={d.red_block} /></span>
					<span class="chev"><Icon name="chevronRight" size={14} /></span>
					<span class="b"><span class="bl">Recovered</span>{#if d.end_block}<BlockLink height={d.end_block} />{:else}<span class="muted">not yet</span>{/if}</span>
				</div>
			</section>

			{#if d.summary}
				<section class="card summary">
					<p class="eyebrow">Summary</p>
					<p>{d.summary}</p>
					<p class="note">Machine-written from the incident record. It never affects alert state.</p>
				</section>
			{/if}

			<section class="card chart-card" aria-labelledby="gap-h">
				<div class="ch">
					<h2 id="gap-h">Gap during the incident</h2>
					<p class="muted">held − owed in {d.symbol}, per snapshot, by block height</p>
				</div>
				{#if d.snapshots?.length}
					<GapChart points={d.snapshots} decimals={d.decimals} symbol={d.symbol} {markers} height={300} />
				{:else}
					<EmptyState icon="activity" title="No snapshots attached" compact />
				{/if}
			</section>

			<div class="cols">
				<section class="card tl" aria-labelledby="tl-h">
					<div class="ch">
						<h2 id="tl-h">Timeline</h2>
						<p class="muted">State changes and intents.near events in the incident's block range</p>
					</div>
					<ol class="timeline">
						{#each timeline as item, i (i)}
							<li class={item.kind === 'milestone' ? `ms ${item.tone}` : 'ev'}>
								<span class="node"></span>
								<div class="tl-body">
									<p class="tl-block"><BlockLink height={item.block} /></p>
									{#if item.kind === 'milestone'}
										<p class="tl-title">{item.title}</p>
										<p class="tl-text">{item.text}</p>
									{:else}
										{@const ev = item.ev}
										<p class="tl-title">
											<span class="evname mono">{ev.event}</span>
											<span class="muted">{EVENT_LABEL[ev.event] ?? ev.standard}</span>
											{#if ev.amount}<span class="evamt num">{formatAmount(ev.amount, d.decimals)} {d.symbol}</span>{/if}
										</p>
										<p class="tl-meta">
											{#if ev.tx_hash}
												<a class="link mono" href={explorer.tx(ev.tx_hash)} target="_blank" rel="noopener noreferrer">tx {middleEllipsis(ev.tx_hash, 8, 6)}</a>
												<CopyButton value={ev.tx_hash} label="Copy transaction hash" />
											{/if}
											<span class="mono muted">receipt {middleEllipsis(ev.receipt_id, 8, 6)}</span>
											<CopyButton value={ev.receipt_id} label="Copy receipt id" />
										</p>
										<details>
											<summary>EVENT_JSON</summary>
											<Code code={prettyRaw(ev.raw)} lang="json" />
										</details>
									{/if}
								</div>
							</li>
						{/each}
					</ol>
					{#if !d.events?.length}
						<p class="muted none">No intents.near events were attached to this incident's block range.</p>
					{/if}
				</section>

				<section class="card snaps" aria-labelledby="sn-h">
					<div class="ch">
						<h2 id="sn-h">Snapshots</h2>
						<p class="muted">{formatInteger(d.snapshots?.length ?? 0)} same-block reads</p>
					</div>
					<div class="snap-scroll">
						<table>
							<thead>
								<tr><th scope="col">Block</th><th scope="col" class="r">Gap ({d.symbol})</th></tr>
							</thead>
							<tbody>
								{#each d.snapshots ?? [] as s (s.block_height)}
									<tr class:neg={s.gap.startsWith('-')}>
										<td><BlockLink height={s.block_height} /></td>
										<td class="r"><Amount value={s.gap} decimals={d.decimals} symbol={d.symbol} sign tone /></td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</section>
			</div>
		{:else if !inc.error}
			<div class="card" style="padding:24px">
				<span class="skeleton" style="height:90px"></span>
				<span class="skeleton" style="height:240px;margin-top:24px"></span>
			</div>
		{/if}
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
		padding: var(--s-6) 0 var(--s-6);
	}
	.title-row {
		display: flex;
		align-items: center;
		gap: var(--s-3);
	}
	h1 {
		font-size: var(--text-3xl);
		font-weight: 650;
		letter-spacing: -0.032em;
	}
	.lede {
		margin-top: var(--s-2);
		color: var(--text-2);
		font-size: var(--text-md);
		max-width: 72ch;
	}
	.tid {
		display: block;
		margin-top: 4px;
		font-size: 12px;
		color: var(--text-3);
		overflow-wrap: anywhere;
	}
	.mb {
		margin-bottom: var(--s-4);
	}
	.card + .card,
	.card + .cols {
		margin-top: var(--s-4);
	}

	.facts {
		display: grid;
		grid-template-columns: 1.5fr 1fr 1fr 1fr;
	}
	.fact {
		padding: var(--s-5) var(--s-6);
		min-width: 0;
	}
	.fact + .fact {
		border-left: 1px solid var(--border);
	}
	.k {
		font-size: var(--text-xs);
		color: var(--text-3);
		font-weight: 500;
	}
	.v {
		margin-top: var(--s-2);
		font-size: var(--text-2xl);
		font-weight: 600;
		letter-spacing: -0.02em;
		line-height: 1.2;
		white-space: nowrap;
	}
	.v.sm {
		font-size: var(--text-lg);
		padding-top: 6px;
	}
	.v.bad {
		color: var(--bad);
	}
	.unit {
		margin-left: 6px;
		font-size: var(--text-sm);
		color: var(--text-3);
		font-weight: 500;
		letter-spacing: 0;
	}
	.s {
		margin-top: 2px;
		font-size: var(--text-xs);
		color: var(--text-3);
	}
	.blocks {
		grid-column: 1 / -1;
		display: flex;
		align-items: center;
		gap: var(--s-3);
		flex-wrap: wrap;
		padding: 12px var(--s-6);
		border-top: 1px solid var(--border);
		background: var(--surface-2);
		border-radius: 0 0 var(--r-lg) var(--r-lg);
		font-size: var(--text-xs);
	}
	.b {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.bl {
		color: var(--text-3);
	}
	.chev {
		color: var(--text-4);
	}

	.summary {
		padding: var(--s-5) var(--s-6);
	}
	.summary p + p {
		margin-top: var(--s-2);
	}
	.note {
		font-size: var(--text-xs);
		color: var(--text-3);
	}

	.chart-card {
		padding: var(--s-6);
	}
	.ch {
		margin-bottom: var(--s-4);
	}
	.ch h2 {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.ch p {
		font-size: var(--text-sm);
		margin-top: 2px;
	}

	.cols {
		display: grid;
		grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr);
		gap: var(--s-4);
		align-items: start;
	}
	.tl,
	.snaps {
		padding: var(--s-6);
	}
	.timeline {
		position: relative;
	}
	.timeline li {
		position: relative;
		display: grid;
		grid-template-columns: 20px minmax(0, 1fr);
		gap: var(--s-3);
		padding-bottom: var(--s-5);
	}
	.timeline li::before {
		content: '';
		position: absolute;
		left: 9px;
		top: 14px;
		bottom: -2px;
		width: 1px;
		background: var(--border);
	}
	.timeline li:last-child::before {
		display: none;
	}
	.node {
		width: 9px;
		height: 9px;
		margin: 5px 0 0 5px;
		border-radius: 50%;
		background: var(--surface);
		border: 2px solid var(--border-strong);
		position: relative;
		z-index: 1;
	}
	.ms .node {
		width: 13px;
		height: 13px;
		margin: 3px 0 0 3px;
		border: 0;
		box-shadow: 0 0 0 3px var(--surface);
	}
	.ms.warn .node {
		background: var(--warn-mark);
	}
	.ms.bad .node {
		background: var(--bad-mark);
	}
	.ms.ok .node {
		background: var(--ok-mark);
	}
	.ms.neutral .node {
		background: var(--text-3);
	}
	.tl-block {
		font-size: var(--text-xs);
		color: var(--text-3);
	}
	.tl-title {
		margin-top: 2px;
		font-weight: 600;
		font-size: var(--text-sm);
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 4px 8px;
	}
	.ev .tl-title {
		font-weight: 500;
	}
	.evname {
		font-size: 12px;
		padding: 1px 6px;
		border-radius: var(--r-xs);
		background: var(--surface-3);
		color: var(--text);
	}
	.evamt {
		margin-left: auto;
		font-weight: 600;
	}
	.tl-text {
		font-size: var(--text-sm);
		color: var(--text-2);
		margin-top: 2px;
	}
	.tl-meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px 6px;
		margin-top: 4px;
		font-size: 11.5px;
	}
	.tl-meta .mono {
		font-size: 11.5px;
	}
	details summary {
		cursor: pointer;
		font-size: var(--text-xs);
		color: var(--text-3);
		padding: 4px 0;
		width: max-content;
		font-family: var(--font-mono);
	}
	details[open] summary {
		margin-bottom: 6px;
	}
	.none {
		font-size: var(--text-sm);
	}

	.snap-scroll {
		max-height: 560px;
		overflow: auto;
		margin: 0 calc(-1 * var(--s-6)) calc(-1 * var(--s-6));
		border-top: 1px solid var(--border);
	}
	.snaps table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-xs);
	}
	.snaps th {
		position: sticky;
		top: 0;
		background: var(--surface-2);
		text-align: left;
		font-weight: 500;
		color: var(--text-3);
		padding: 8px var(--s-6);
		border-bottom: 1px solid var(--border);
	}
	.snaps td {
		padding: 7px var(--s-6);
		border-bottom: 1px solid var(--border-soft);
	}
	.snaps tr.neg td {
		background: color-mix(in srgb, var(--bad-bg) 55%, transparent);
	}
	.snaps .r,
	.r {
		text-align: right;
	}

	@media (max-width: 960px) {
		.cols {
			grid-template-columns: 1fr;
		}
		.facts {
			grid-template-columns: 1fr 1fr;
		}
		.fact:nth-child(3) {
			border-left: 0;
		}
		.fact:nth-child(n + 3) {
			border-top: 1px solid var(--border);
		}
	}
	@media (max-width: 560px) {
		h1 {
			font-size: var(--text-2xl);
		}
		.fact {
			padding: var(--s-4);
		}
		.v {
			font-size: var(--text-xl);
		}
		.v.sm {
			font-size: var(--text-md);
		}
		.chart-card,
		.tl,
		.snaps {
			padding: var(--s-4);
		}
		.snap-scroll {
			margin: 0 calc(-1 * var(--s-4)) calc(-1 * var(--s-4));
		}
		.blocks {
			padding: 12px var(--s-4);
		}
		.chev {
			display: none;
		}
	}
</style>
