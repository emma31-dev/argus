<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Token, Status } from '#lib/api.ts';
	import { STATUS_RANK } from '#lib/api.ts';
	import { formatPercent, middleEllipsis, parseAmount, ratioPercent } from '#lib/format.ts';
	import { href } from '#lib/links.ts';
	import Amount from './Amount.svelte';
	import BlockLink from './BlockLink.svelte';
	import StatusBadge from './StatusBadge.svelte';
	import Segmented from './Segmented.svelte';
	import EmptyState from './EmptyState.svelte';
	import Icon from './Icon.svelte';
	import ChainChip from './ChainChip.svelte';
	import { chainLabel } from '#lib/chain.ts';
	import { summarizeReadError } from '#lib/errors.ts';

	let { tokens, loading = false }: { tokens: Token[] | null; loading?: boolean } = $props();

	type Filter = 'all' | Status;
	let filter = $state<Filter>('all');
	let query = $state('');

	interface Row {
		t: Token;
		gap: bigint | null;
		pct: number | null;
	}

	const rows = $derived.by<Row[]>(() => {
		const list = (tokens ?? []).map((t) => {
			const gap = parseAmount(t.latest?.gap);
			const owed = parseAmount(t.latest?.owed);
			return { t, gap, pct: ratioPercent(gap, owed) };
		});
		// Deficit risk: status rank, then gap relative to owed, most negative first.
		return list.sort((a, b) => {
			const r = STATUS_RANK[a.t.status] - STATUS_RANK[b.t.status];
			if (r) return r;
			const pa = a.pct ?? Number.POSITIVE_INFINITY;
			const pb = b.pct ?? Number.POSITIVE_INFINITY;
			if (pa !== pb) return pa - pb;
			return a.t.symbol.localeCompare(b.t.symbol);
		});
	});

	const counts = $derived.by(() => {
		const c: Record<Filter, number> = { all: 0, red: 0, amber: 0, unknown: 0, green: 0 };
		for (const r of rows) {
			c.all++;
			c[r.t.status] = (c[r.t.status] ?? 0) + 1;
		}
		return c;
	});

	const visible = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return rows.filter(
			(r) =>
				(filter === 'all' || r.t.status === filter) &&
				(!q ||
					r.t.symbol.toLowerCase().includes(q) ||
					r.t.id.toLowerCase().includes(q) ||
					chainLabel(r.t.id, r.t.chain).toLowerCase().includes(q) ||
					r.t.contract.toLowerCase().includes(q))
		);
	});

	const options = $derived([
		{ value: 'all' as Filter, label: 'All', count: counts.all },
		{ value: 'red' as Filter, label: 'Deficit', count: counts.red, dot: 'red' },
		{ value: 'amber' as Filter, label: 'Settling', count: counts.amber, dot: 'amber' },
		{ value: 'unknown' as Filter, label: 'Unknown', count: counts.unknown, dot: 'unknown' },
		{ value: 'green' as Filter, label: 'Backed', count: counts.green, dot: 'green' }
	]);

	let searchEl: HTMLInputElement | undefined = $state();
	function onWindowKey(e: KeyboardEvent) {
		const target = e.target as HTMLElement | null;
		const typing = target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable);
		if (e.key === '/' && !typing) {
			e.preventDefault();
			searchEl?.focus();
		}
	}

	function open(id: string) {
		goto(href.token(id));
	}

	function rowClick(e: MouseEvent, id: string) {
		// Let real links and text selection behave normally.
		if ((e.target as HTMLElement).closest('a,button')) return;
		if (window.getSelection()?.toString()) return;
		open(id);
	}

	function shortId(t: Token) {
		const [std, ...rest] = t.id.split(':');
		const tail = rest.join(':');
		return `${std}:${tail.length > 30 ? middleEllipsis(tail, 10, 8) : tail}`;
	}
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="toolbar">
	<Segmented {options} bind:value={filter} label="Filter by status" />
	<label class="search">
		<Icon name="search" size={14} />
		<span class="sr-only">Search tokens</span>
		<input
			bind:this={searchEl}
			bind:value={query}
			class="input"
			type="search"
			placeholder="Search symbol, chain or token id"
			autocomplete="off"
			spellcheck="false"
		/>
		{#if !query}<kbd aria-hidden="true">/</kbd>{/if}
	</label>
</div>

<div class="card table-card" class:refreshing={loading && tokens !== null}>
	{#if tokens === null}
		<table class="tbl" aria-busy="true">
			<thead>
				<tr>
					<th class="c-status">Status</th>
					<th class="c-token">Token</th>
					<th class="r">Owed</th>
					<th class="r">Held</th>
					<th class="r">Gap</th>
					<th class="r">Gap / owed</th>
					<th class="r c-block">Block</th>
				</tr>
			</thead>
			<tbody>
				{#each Array(8) as _, i (i)}
					<tr class="sk">
						<td><span class="skeleton" style="width:64px;height:20px;border-radius:99px"></span></td>
						<td>
							<span class="skeleton" style="width:{56 + ((i * 17) % 40)}px;height:12px"></span>
							<span class="skeleton" style="width:{120 + ((i * 29) % 80)}px;height:10px;margin-top:6px"></span>
						</td>
						{#each Array(5) as __, j (j)}
							<td class="r"><span class="skeleton" style="width:{60 + ((i + j) * 13) % 40}px;height:12px;margin-left:auto"></span></td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>
	{:else if rows.length === 0}
		<EmptyState icon="activity" title="No tokens tracked yet">
			Argus discovers tokens from intents.near on startup. The first snapshot round will appear here.
		</EmptyState>
	{:else if visible.length === 0}
		<EmptyState icon="search" title="No tokens match" compact>
			Nothing matches {query ? `“${query}”` : 'this filter'}{filter !== 'all' ? ` with status ${filter}` : ''}.
			{#snippet actions()}
				<button class="btn" type="button" onclick={() => ((query = ''), (filter = 'all'))}>Clear filters</button>
			{/snippet}
		</EmptyState>
	{:else}
		<table class="tbl">
			<caption class="sr-only">Tokens on intents.near, sorted by deficit risk</caption>
			<thead>
				<tr>
					<th scope="col" class="c-status">Status</th>
					<th scope="col" class="c-token">Token</th>
					<th scope="col" class="r">Owed</th>
					<th scope="col" class="r">Held</th>
					<th scope="col" class="r">Gap <span class="hint">held − owed</span></th>
					<th scope="col" class="r">Gap / owed</th>
					<th scope="col" class="r c-block">Block</th>
				</tr>
			</thead>
			<tbody>
				{#each visible as r (r.t.id)}
					<tr class="row {r.t.status}" onclick={(e) => rowClick(e, r.t.id)}>
						<td class="c-status"><StatusBadge status={r.t.status} size="sm" /></td>
						<td class="c-token">
							<span class="symrow">
								<a class="sym" href={href.token(r.t.id)}>{r.t.symbol || '—'}</a>
								<ChainChip id={r.t.id} chain={r.t.chain} />
							</span>
							<span class="tid mono" title={r.t.id}>{shortId(r.t)}</span>
						</td>
						{#if r.t.latest}
							<td class="r"><Amount value={r.t.latest.owed} decimals={r.t.decimals} symbol={r.t.symbol} compact /></td>
							<td class="r"><Amount value={r.t.latest.held} decimals={r.t.decimals} symbol={r.t.symbol} compact /></td>
							<td class="r gap"><Amount value={r.t.latest.gap} decimals={r.t.decimals} symbol={r.t.symbol} compact sign tone /></td>
							<td class="r pct num" class:neg={r.pct !== null && r.pct < 0}>{formatPercent(r.pct)}</td>
							<td class="r c-block">
								{#if r.t.stale}
									<span class="stale" title="{summarizeReadError(r.t.last_error)} Showing the last successful snapshot.">
										<Icon name="clock" size={12} stroke={2.25} /><span class="sr-only">Stale:</span>
									</span>
								{/if}
								<BlockLink height={r.t.latest.block_height} />
							</td>
						{:else}
							<td class="r muted" colspan="5" title={r.t.stale ? summarizeReadError(r.t.last_error) : undefined}>
								No successful snapshot yet{r.t.stale ? ' · last read failed' : ''}
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>

		<!-- Phone layout: one card per token -->
		<ul class="cards">
			{#each visible as r (r.t.id)}
				<li>
					<a class="tcard {r.t.status}" href={href.token(r.t.id)}>
						<div class="tc-head">
							<span class="symrow"><span class="sym">{r.t.symbol || '—'}</span><ChainChip id={r.t.id} chain={r.t.chain} /></span>
							<span class="tc-badges">
								{#if r.t.stale}<span class="stale" title="Last read failed"><Icon name="clock" size={12} stroke={2.25} /><span class="sr-only">Stale</span></span>{/if}
								<StatusBadge status={r.t.status} size="sm" />
							</span>
						</div>
						<span class="tid mono">{shortId(r.t)}</span>
						{#if r.t.latest}
							<dl class="tc-grid">
								<div><dt>Owed</dt><dd><Amount value={r.t.latest.owed} decimals={r.t.decimals} symbol={r.t.symbol} compact /></dd></div>
								<div><dt>Held</dt><dd><Amount value={r.t.latest.held} decimals={r.t.decimals} symbol={r.t.symbol} compact /></dd></div>
								<div>
									<dt>Gap</dt>
									<dd><Amount value={r.t.latest.gap} decimals={r.t.decimals} symbol={r.t.symbol} compact sign tone /></dd>
								</div>
								<div>
									<dt>Gap / owed</dt>
									<dd class="num" class:neg={r.pct !== null && r.pct < 0}>{formatPercent(r.pct)}</dd>
								</div>
							</dl>
						{:else}
							<p class="muted tc-none">No successful snapshot yet{r.t.stale ? ' · last read failed' : ''}</p>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</div>
{#if tokens && visible.length > 0}
	<p class="foot-note">
		Showing <span class="num">{visible.length}</span> of <span class="num">{rows.length}</span> tokens, sorted by
		deficit risk. Amounts in whole tokens; hover any number for the exact smallest-unit value.
	</p>
{/if}

<style>
	.toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--s-3);
		margin-bottom: var(--s-3);
		flex-wrap: wrap;
	}
	.search {
		position: relative;
		display: flex;
		align-items: center;
		flex: 0 1 320px;
		min-width: 0;
		color: var(--text-3);
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
		pointer-events: none;
	}
	.search input {
		width: 100%;
		padding-left: 32px;
		padding-right: 32px;
	}
	.search input::-webkit-search-cancel-button {
		-webkit-appearance: none;
	}
	.search kbd {
		position: absolute;
		right: 8px;
		pointer-events: none;
	}

	.table-card {
		transition: opacity var(--dur) var(--ease);
	}
	.refreshing {
		opacity: 1;
	}

	.tbl {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		font-size: var(--text-sm);
	}
	th {
		position: sticky;
		top: var(--nav-h);
		z-index: 2;
		background: var(--surface-2);
		color: var(--text-3);
		font-size: var(--text-xs);
		font-weight: 500;
		text-align: left;
		padding: 9px 14px;
		border-bottom: 1px solid var(--border);
		white-space: nowrap;
	}
	th:first-child {
		border-top-left-radius: var(--r-lg);
	}
	th:last-child {
		border-top-right-radius: var(--r-lg);
	}
	th .hint {
		color: var(--text-4);
		font-weight: 450;
		margin-left: 4px;
	}
	td {
		padding: 11px 14px;
		border-bottom: 1px solid var(--border-soft);
		vertical-align: middle;
		white-space: nowrap;
	}
	tbody tr:last-child td {
		border-bottom: 0;
	}
	.r {
		text-align: right;
	}
	.c-status {
		width: 1%;
		padding-right: 4px;
	}
	.c-token {
		max-width: 320px;
	}
	.c-block {
		color: var(--text-2);
		font-size: var(--text-xs);
	}
	.row {
		cursor: pointer;
		transition: background var(--dur) var(--ease);
	}
	.row:hover td {
		background: var(--surface-2);
	}
	.row.red td {
		background: color-mix(in srgb, var(--bad-bg) 70%, transparent);
	}
	.row.red:hover td {
		background: var(--bad-bg);
	}
	.row.red td:first-child {
		box-shadow: inset 2px 0 0 var(--bad-mark);
	}
	.row.amber td:first-child {
		box-shadow: inset 2px 0 0 var(--warn-mark);
	}
	.symrow {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
	}
	.stale {
		display: inline-flex;
		vertical-align: -1px;
		margin-right: 6px;
		color: var(--warn);
	}
	.tc-badges {
		display: inline-flex;
		align-items: center;
	}
	.sym {
		display: block;
		font-weight: 600;
		color: var(--text);
		font-size: var(--text-sm);
	}
	.sym:hover {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.tid {
		display: block;
		margin-top: 1px;
		font-size: 11.5px;
		color: var(--text-3);
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.pct {
		color: var(--text-2);
	}
	.neg {
		color: var(--bad);
		font-weight: 550;
	}
	.gap :global(.amount:not(.neg)) {
		color: var(--text-2);
	}
	.sk td {
		padding-block: 14px;
	}
	.foot-note {
		margin-top: var(--s-3);
		font-size: var(--text-xs);
		color: var(--text-3);
	}

	.cards {
		display: none;
	}

	@media (max-width: 880px) {
		.tbl {
			display: none;
		}
		.table-card {
			background: transparent;
			border: 0;
			box-shadow: none;
		}
		.cards {
			display: grid;
			gap: var(--s-2);
		}
		.tcard {
			display: block;
			padding: var(--s-4);
			border: 1px solid var(--border);
			border-radius: var(--r-lg);
			background: var(--surface);
		}
		.tcard.red {
			border-color: var(--bad-border);
			background: var(--bad-bg);
		}
		.tcard.amber {
			border-color: var(--warn-border);
		}
		.tc-head {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: var(--s-2);
		}
		.tc-head .sym {
			font-size: var(--text-md);
		}
		.tc-grid {
			display: grid;
			grid-template-columns: 1fr 1fr;
			gap: var(--s-3) var(--s-4);
			margin-top: var(--s-3);
			padding-top: var(--s-3);
			border-top: 1px solid var(--border-soft);
		}
		.tc-grid dt {
			font-size: var(--text-2xs);
			color: var(--text-3);
			font-weight: 500;
			text-transform: uppercase;
			letter-spacing: var(--tracking-caps);
		}
		.tc-grid dd {
			margin-top: 2px;
			font-size: var(--text-sm);
		}
		.tc-none {
			margin-top: var(--s-2);
			font-size: var(--text-sm);
		}
		.search {
			flex: 1 1 100%;
			order: -1;
		}
		.toolbar :global(.seg) {
			width: 100%;
		}
	}
</style>
