<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type ReplayResponse, type ReplayToken } from '$lib/api';
	import { Resource } from '$lib/live.svelte';
	import { formatDuration, formatInteger, formatUtc } from '$lib/format';
	import { href, REPO_URL } from '$lib/links';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import GapChart, { type GapMarker } from '$lib/components/GapChart.svelte';
	import BlockLink from '$lib/components/BlockLink.svelte';
	import Amount from '$lib/components/Amount.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import OfflineBanner from '$lib/components/OfflineBanner.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import ChainChip from '$lib/components/ChainChip.svelte';
	import CopyButton from '$lib/components/CopyButton.svelte';

	const replay = new Resource<ReplayResponse>((s) => api.replay(s));
	onMount(() => {
		replay.load();
	});

	const pending = $derived(replay.error?.notFound ?? false);

	function markers(t: ReplayToken): GapMarker[] {
		const m: GapMarker[] = [];
		if (t.first_negative_block) m.push({ block: t.first_negative_block, label: 'First negative block', tone: 'warn' });
		if (t.detected_red_block) m.push({ block: t.detected_red_block, label: 'Argus turns red', tone: 'bad' });
		return m;
	}
	function detect(t: ReplayToken) {
		const a = t.points.find((p) => p.block_height === t.first_negative_block);
		const b = t.points.find((p) => p.block_height === t.detected_red_block);
		const blocks = t.detected_red_block && t.first_negative_block ? t.detected_red_block - t.first_negative_block : null;
		// Prefer the backend's figure ((K-1) x cadence on sparse samples); fall back to block times.
		const secs =
			t.time_to_detect_seconds != null
				? t.time_to_detect_seconds
				: a && b
					? (new Date(b.block_time).getTime() - new Date(a.block_time).getTime()) / 1000
					: null;
		return { blocks, secs };
	}
	const EVIDENCE_URL = `${REPO_URL}/tree/main/evidence`;
</script>

<svelte:head><title>October replay · Argus</title></svelte:head>

<div class="container">
	<PageHeader eyebrow="Incident replay · 2026-10-01" title="Would Argus have caught it?">
		Argus's same-block check, re-run with archival RPC at block heights before, during and after the October 2026
		NEAR Intents exploit. Every block height and query is committed to the repository, so the result can be reproduced.
	</PageHeader>

	{#if replay.error && !pending}
		<OfflineBanner error={replay.error} retry={() => replay.load()} />
	{/if}

	{#if pending}
		<section class="card pending" aria-labelledby="pending-h">
			<div class="pending-art" aria-hidden="true">
				<svg viewBox="0 0 520 160" preserveAspectRatio="none">
					<line x1="0" y1="62" x2="520" y2="62" class="zero" />
					<path d="M0 54 C60 52 100 56 150 54 S 200 52 214 54" class="known" />
					<path d="M214 54 C 250 64 280 92 320 112 S 400 132 430 128 S 470 70 520 56" class="unknown" />
					<line x1="214" y1="16" x2="214" y2="150" class="mk" />
					<line x1="300" y1="16" x2="300" y2="150" class="mk" />
				</svg>
				<span class="art-l" style="left:41.2%;top:14px">first negative?</span>
				<span class="art-l" style="left:57.7%;top:34px">red?</span>
			</div>
			<div class="pending-body">
				<span class="pill"><Icon name="clock" size={12} stroke={2.25} />Pending archival data</span>
				<h2 id="pending-h">The replay hasn't been published yet</h2>
				<p>
					Reconstructing the exploit needs historical state from around 2026-10-01, which only archival RPC nodes keep.
					Free archival access is heavily rate-limited, so the replay is run once, offline, with every response cached
					and committed to <code>evidence/replay-2026-10-01.json</code>. This page fills in as soon as that file exists.
				</p>
				<div class="will">
					<p class="eyebrow">What it will show</p>
					<ol>
						<li><span class="n">1</span>The first block where the ledger owed more than intents.near held</li>
						<li><span class="n">2</span>How the gap grew with each failed deposit</li>
						<li><span class="n">3</span>The block at which Argus's debounce would have turned red</li>
						<li><span class="n">4</span>Every block height and RPC query, so anyone can re-run it</li>
					</ol>
				</div>
				<div class="actions">
					<a class="btn" href={href.how('#context')}>Background on the incident</a>
					<a class="btn btn-ghost" href={EVIDENCE_URL} target="_blank" rel="noopener noreferrer">Evidence folder <Icon name="external" size={13} /></a>
				</div>
			</div>
		</section>
	{:else if replay.data}
		{@const r = replay.data}
		<div class="meta">
			<span><span class="mk">Replay</span> {r.title}</span>
			<span class="dot">·</span>
			<span><span class="mk">Generated</span> {formatUtc(r.generated_at, { seconds: false })}</span>
			<span class="dot">·</span>
			<span><span class="mk">Archival RPC</span> <span class="mono">{r.rpc_provider}</span></span>
		</div>
		{#if r.notes}<p class="notes">{r.notes}</p>{/if}

		{#each r.tokens as t (t.id)}
			{@const d = detect(t)}
			<section class="tok" aria-labelledby="rt-{t.id}">
				<div class="tok-head">
					<h2 id="rt-{t.id}">{t.symbol}</h2>
					<ChainChip id={t.id} chain={t.chain} size="md" />
					<code class="mono">{t.id}</code>
				</div>

				<div class="card stats">
					<div class="st">
						<p class="k"><i class="sw warn"></i>First negative block</p>
						<p class="v">{#if t.first_negative_block}<BlockLink height={t.first_negative_block} />{:else}<span class="muted">None</span>{/if}</p>
						<p class="s">{t.first_negative_block ? 'ledger owes more than it holds' : 'no sampled block had a negative gap'}</p>
					</div>
					<div class="st">
						<p class="k"><i class="sw bad"></i>Argus turns red</p>
						<p class="v">{#if t.detected_red_block}<BlockLink height={t.detected_red_block} />{:else}<span class="muted">Never</span>{/if}</p>
						<p class="s">{t.detected_red_block ? 'after the debounce window' : 'debounce did not fire on these samples'}</p>
					</div>
					<div class="st">
						<p class="k">Time to detect</p>
						<p class="v num">{d.secs !== null ? formatDuration(d.secs) : '—'}</p>
						<p class="s">{d.blocks !== null ? `${formatInteger(d.blocks)} blocks after the first negative` : 'not detected'}</p>
					</div>
					<div class="st">
						<p class="k">Max deficit</p>
						<p class="v bad"><Amount value={t.max_deficit} decimals={t.decimals} symbol={t.symbol} /><span class="u">{t.symbol}</span></p>
						<p class="s">largest shortfall in the window</p>
					</div>
				</div>

				{#if t.detection_note}<p class="dnote">{t.detection_note}</p>{/if}

				<div class="card chart">
					<div class="ch">
						<h3>Gap across the exploit window</h3>
						<p class="muted">held − owed in {t.symbol} at each replayed block</p>
					</div>
					<GapChart points={t.points} decimals={t.decimals} symbol={t.symbol} markers={markers(t)} height={320} step />
				</div>

				<div class="card tbl-card">
					<div class="tbl-scroll">
						<table>
							<caption class="sr-only">Replayed snapshots for {t.symbol}</caption>
							<thead>
								<tr>
									<th scope="col">Block</th>
									<th scope="col">Time (UTC)</th>
									<th scope="col" class="r">Owed</th>
									<th scope="col" class="r">Held</th>
									<th scope="col" class="r">Gap</th>
									<th scope="col">Label</th>
									<th scope="col">Argus</th>
									<th scope="col">Queries</th>
								</tr>
							</thead>
							<tbody>
								{#each t.points as p (p.block_height)}
									<tr
										class:neg={p.gap.startsWith('-')}
										class:key={p.block_height === t.first_negative_block || p.block_height === t.detected_red_block}
									>
										<td><BlockLink height={p.block_height} /></td>
										<td class="num t">{formatUtc(p.block_time).replace(' UTC', '')}</td>
										<td class="r"><Amount value={p.owed} decimals={t.decimals} symbol={t.symbol} /></td>
										<td class="r"><Amount value={p.held} decimals={t.decimals} symbol={t.symbol} /></td>
										<td class="r"><Amount value={p.gap} decimals={t.decimals} symbol={t.symbol} sign tone /></td>
										<td class="lbl">{p.label}</td>
										<td><StatusBadge status={p.status} size="sm" /></td>
										<td class="q">
											{#if p.owed_query}<span class="qb">owed<CopyButton value={p.owed_query} label="Copy owed query" /></span>{/if}
											{#if p.held_query}<span class="qb">held<CopyButton value={p.held_query} label="Copy held query" /></span>{/if}
											{#if !p.owed_query && !p.held_query}<span class="muted">—</span>{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			</section>
		{/each}
	{:else if !replay.error}
		<div class="card" style="padding:24px" aria-busy="true">
			<span class="skeleton" style="height:84px"></span>
			<span class="skeleton" style="height:280px;margin-top:20px"></span>
		</div>
	{/if}
</div>

<style>
	.pending {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		overflow: hidden;
	}
	.pending-art {
		height: 160px;
		border-bottom: 1px solid var(--border);
		background:
			linear-gradient(to right, var(--surface) 0, transparent 12%, transparent 88%, var(--surface) 100%),
			repeating-linear-gradient(0deg, var(--grid) 0 1px, transparent 1px 40px),
			var(--surface-2);
	}
	.pending-art svg {
		width: 100%;
		height: 100%;
	}
	.pending-art .zero {
		stroke: var(--axis);
	}
	.pending-art .known {
		fill: none;
		stroke: var(--gap-pos);
		stroke-width: 2;
		vector-effect: non-scaling-stroke;
	}
	.pending-art .unknown {
		fill: none;
		stroke: var(--text-4);
		stroke-width: 1.5;
		stroke-dasharray: 3 5;
		vector-effect: non-scaling-stroke;
	}
	.pending-art .mk {
		stroke: var(--text-4);
		stroke-dasharray: 2 3;
		vector-effect: non-scaling-stroke;
	}
	.pending-art {
		position: relative;
	}
	.art-l {
		position: absolute;
		margin-left: 8px;
		font-size: 11px;
		color: var(--text-3);
		font-family: var(--font-mono);
		white-space: nowrap;
	}
	.pending-body {
		padding: var(--s-8);
		max-width: 760px;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		height: 24px;
		padding: 0 10px 0 8px;
		border-radius: var(--r-pill);
		background: var(--warn-bg);
		border: 1px solid var(--warn-border);
		color: var(--warn);
		font-size: var(--text-xs);
		font-weight: 550;
	}
	.pending h2 {
		margin-top: var(--s-4);
		font-size: var(--text-2xl);
		letter-spacing: -0.026em;
	}
	.pending-body > p {
		margin-top: var(--s-3);
		color: var(--text-2);
		line-height: var(--leading-loose);
	}
	.pending code {
		font-size: 0.86em;
		color: var(--text);
		background: var(--surface-2);
		padding: 1px 5px;
		border-radius: var(--r-xs);
		border: 1px solid var(--border-soft);
		overflow-wrap: anywhere;
	}
	.will {
		margin-top: var(--s-6);
		padding-top: var(--s-5);
		border-top: 1px solid var(--border-soft);
	}
	.will ol {
		margin-top: var(--s-3);
		display: grid;
		gap: var(--s-2);
	}
	.will li {
		display: flex;
		gap: var(--s-3);
		align-items: baseline;
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.will .n {
		flex: none;
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		border: 1px solid var(--border-strong);
		font-size: 10.5px;
		font-weight: 600;
		color: var(--text-2);
		transform: translateY(-1px);
	}
	.actions {
		display: flex;
		gap: var(--s-2);
		flex-wrap: wrap;
		margin-top: var(--s-6);
	}

	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 10px;
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.meta .mk {
		color: var(--text-3);
		margin-right: 4px;
	}
	.meta .dot {
		color: var(--text-4);
	}
	.notes {
		margin-top: var(--s-2);
		font-size: var(--text-sm);
		color: var(--text-3);
	}
	.tok {
		margin-top: var(--s-8);
		display: grid;
		gap: var(--s-4);
	}
	.tok-head {
		display: flex;
		align-items: baseline;
		gap: var(--s-3);
		flex-wrap: wrap;
	}
	.tok-head h2 {
		font-size: var(--text-xl);
	}
	.tok-head code {
		font-size: 12px;
		color: var(--text-3);
		overflow-wrap: anywhere;
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
	}
	.st {
		padding: var(--s-5) var(--s-6);
		min-width: 0;
	}
	.st + .st {
		border-left: 1px solid var(--border);
	}
	.k {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: var(--text-xs);
		color: var(--text-3);
		font-weight: 500;
	}
	.sw {
		width: 8px;
		height: 8px;
		border-radius: 50%;
	}
	.sw.warn {
		background: var(--warn-mark);
	}
	.sw.bad {
		background: var(--bad-mark);
	}
	.v {
		margin-top: var(--s-2);
		font-size: var(--text-xl);
		font-weight: 600;
		letter-spacing: -0.015em;
		white-space: nowrap;
	}
	.v :global(.block) {
		font-weight: 500;
		font-size: 0.9em;
	}
	.v.bad {
		color: var(--bad);
	}
	.u {
		font-size: var(--text-sm);
		margin-left: 6px;
		color: var(--text-3);
		font-weight: 500;
	}
	.s {
		margin-top: 2px;
		font-size: var(--text-xs);
		color: var(--text-3);
	}
	.chart {
		padding: var(--s-6);
	}
	.ch {
		margin-bottom: var(--s-3);
	}
	.ch h3 {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.ch p {
		font-size: var(--text-sm);
		margin-top: 2px;
	}
	.tbl-card {
		overflow: hidden;
	}
	.tbl-scroll {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-xs);
		white-space: nowrap;
	}
	th {
		text-align: left;
		background: var(--surface-2);
		color: var(--text-3);
		font-weight: 500;
		padding: 9px 14px;
		border-bottom: 1px solid var(--border);
	}
	th.r,
	td.r {
		text-align: right;
	}
	td {
		padding: 8px 14px;
		border-bottom: 1px solid var(--border-soft);
	}
	tr.neg td {
		background: color-mix(in srgb, var(--bad-bg) 50%, transparent);
	}
	tr.key td {
		box-shadow: inset 0 1px 0 var(--bad-border), inset 0 -1px 0 var(--bad-border);
	}
	.t {
		color: var(--text-2);
	}
	.lbl {
		color: var(--text-2);
	}
	.q {
		white-space: nowrap;
	}
	.qb {
		display: inline-flex;
		align-items: center;
		gap: 0;
		margin-right: 6px;
		color: var(--text-3);
		font-family: var(--font-mono);
		font-size: 11px;
	}
	.dnote {
		font-size: var(--text-sm);
		color: var(--text-2);
		line-height: var(--leading-loose);
		max-width: 80ch;
	}

	@media (max-width: 900px) {
		.stats {
			grid-template-columns: 1fr 1fr;
		}
		.st:nth-child(3) {
			border-left: 0;
		}
		.st:nth-child(n + 3) {
			border-top: 1px solid var(--border);
		}
	}
	@media (max-width: 560px) {
		.pending-body {
			padding: var(--s-5);
		}
		.pending h2 {
			font-size: var(--text-xl);
		}
		.st {
			padding: var(--s-4);
		}
		.v {
			font-size: var(--text-lg);
		}
		.chart {
			padding: var(--s-4);
		}
	}
</style>
