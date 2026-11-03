<script lang="ts" module>
	export interface GapMarker {
		block: number;
		label: string;
		tone: 'warn' | 'bad' | 'ok' | 'neutral';
	}
</script>

<script lang="ts">
	import { scaleLinear } from 'd3-scale';
	import { area, line, curveMonotoneX, curveStepAfter } from 'd3-shape';
	import type { Snapshot, Status } from '$lib/api';
	import { chartId, nearestIndex } from '$lib/chart';
	import { formatAxis, formatInteger, formatTokens, formatUtc, parseAmount, toTokenNumber } from '$lib/format';
	import { statusMeta } from '$lib/status';

	/**
	 * Gap (held − owed) across block heights, with annotated markers.
	 * Used for incident timelines and the October replay.
	 */
	let {
		points,
		decimals,
		symbol,
		markers = [],
		height = 280,
		step = false
	}: {
		points: (Snapshot & { label?: string; status?: Status })[];
		decimals: number;
		symbol: string;
		markers?: GapMarker[];
		height?: number;
		/** Draw as steps (discrete snapshots) instead of a smooth curve. */
		step?: boolean;
	} = $props();

	const id = chartId('gc');
	let width = $state(0);
	const M = { top: 44, right: 14, bottom: 30, left: 62 };
	const innerW = $derived(Math.max(10, width - M.left - M.right));
	const innerH = $derived(height - M.top - M.bottom);

	const data = $derived(
		points
			.map((p) => {
				const g = parseAmount(p.gap);
				return { b: p.block_height, gap: g === null ? NaN : toTokenNumber(g, decimals), gapBig: g, raw: p };
			})
			.filter((p) => Number.isFinite(p.gap))
			.sort((a, b) => a.b - b.b)
	);
	type D = (typeof data)[number];

	const x = $derived.by(() => {
		const lo = data[0]?.b ?? 0;
		const hi = data[data.length - 1]?.b ?? 1;
		return scaleLinear()
			.domain([lo, hi === lo ? lo + 1 : hi])
			.range([0, innerW]);
	});
	const y = $derived.by(() => {
		let lo = 0;
		let hi = 0;
		for (const p of data) {
			lo = Math.min(lo, p.gap);
			hi = Math.max(hi, p.gap);
		}
		if (lo === 0 && hi === 0) hi = 1;
		// Keep a visible band above zero even when surplus is tiny.
		if (hi < Math.abs(lo) * 0.12) hi = Math.abs(lo) * 0.12;
		const pad = (hi - lo) * 0.06;
		return scaleLinear()
			.domain([lo - pad, hi + pad])
			.range([innerH, 0])
			.nice(4);
	});
	const zeroY = $derived(y(0));
	const curve = $derived(step ? curveStepAfter : curveMonotoneX);
	const areaPath = $derived(
		area<D>()
			.x((p) => x(p.b))
			.y0(() => zeroY)
			.y1((p) => y(p.gap))
			.curve(curve)(data) ?? ''
	);
	const linePath = $derived(
		line<D>()
			.x((p) => x(p.b))
			.y((p) => y(p.gap))
			.curve(curve)(data) ?? ''
	);
	const yTicks = $derived(y.ticks(4));
	const xTicks = $derived.by(() => {
		const n = Math.max(2, Math.floor(innerW / 150));
		return x.ticks(n).filter((t) => Number.isInteger(t));
	});
	const xs = $derived(data.map((p) => p.b));

	// Lay out marker labels so neighbours don't collide (two rows).
	const placed = $derived.by(() => {
		const sorted = [...markers].sort((a, b) => a.block - b.block);
		const rows = [-Infinity, -Infinity];
		return sorted.map((m) => {
			const px = x(m.block);
			const w = m.label.length * 6.2 + 16;
			let row = rows.findIndex((end) => px - 6 > end);
			if (row < 0) row = 1;
			const anchorRight = px + w > innerW;
			rows[row] = anchorRight ? px : px + w;
			return { ...m, px, row, anchorRight };
		});
	});

	let active = $state<number | null>(null);
	const hp = $derived(active !== null ? data[active] : null);

	function onMove(e: PointerEvent) {
		const r = (e.currentTarget as SVGElement).getBoundingClientRect();
		active = nearestIndex(xs, x.invert(e.clientX - r.left - M.left));
	}
	function onKey(e: KeyboardEvent) {
		if (!data.length) return;
		const last = data.length - 1;
		let next = active;
		if (e.key === 'ArrowRight') next = active === null ? 0 : Math.min(last, active + 1);
		else if (e.key === 'ArrowLeft') next = active === null ? last : Math.max(0, active - 1);
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = last;
		else if (e.key === 'Escape') next = null;
		else return;
		e.preventDefault();
		active = next;
	}
	const tipLeft = $derived(hp ? M.left + x(hp.b) : 0);
	const summary = $derived(
		`Gap of ${symbol} across ${data.length} snapshots from block ${formatInteger(data[0]?.b)} to ${formatInteger(data[data.length - 1]?.b)}. ` +
			markers.map((m) => `${m.label} at block ${formatInteger(m.block)}`).join('. ') +
			'. Use arrow keys to inspect each snapshot.'
	);
</script>

<div class="chart" bind:clientWidth={width} style:min-height="{height}px">
	{#if width > 0 && data.length > 0}
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg
			{width}
			{height}
			role="application"
			tabindex="0"
			aria-label={summary}
			onpointermove={onMove}
			onpointerleave={() => (active = null)}
			onkeydown={onKey}
			onblur={() => (active = null)}
		>
			<defs>
				<clipPath id="{id}-pos"><rect x="-2" y="-2" width={innerW + 4} height={zeroY + 3} /></clipPath>
				<clipPath id="{id}-neg"><rect x="-2" y={zeroY + 1} width={innerW + 4} height={innerH - zeroY + 4} /></clipPath>
			</defs>
			<g transform="translate({M.left},{M.top})">
				{#each yTicks as t (t)}
					<line class="grid" x1="0" x2={innerW} y1={y(t)} y2={y(t)} />
					<text class="tick" x="-10" y={y(t)} dy="0.32em" text-anchor="end">{formatAxis(t)}</text>
				{/each}
				<line class="zero" x1="0" x2={innerW} y1={zeroY} y2={zeroY} />

				{#each placed as m (m.block + m.label)}
					<g class="marker {m.tone}">
						<line x1={m.px} x2={m.px} y1={-30 + m.row * 16} y2={innerH} />
						<circle cx={m.px} cy={-30 + m.row * 16} r="3" />
						<text x={m.px + (m.anchorRight ? -8 : 8)} y={-30 + m.row * 16} dy="0.34em" text-anchor={m.anchorRight ? 'end' : 'start'}
							>{m.label}</text
						>
					</g>
				{/each}

				<g clip-path="url(#{id}-pos)">
					<path class="area pos" d={areaPath} />
					<path class="ln pos" d={linePath} />
				</g>
				<g clip-path="url(#{id}-neg)">
					<path class="area neg" d={areaPath} />
					<path class="ln neg" d={linePath} />
				</g>

				{#each data as p (p.b)}
					{#if data.length <= 80}
						<circle class="pt" class:neg={p.gap < 0} cx={x(p.b)} cy={y(p.gap)} r="2.5" />
					{/if}
				{/each}

				{#each xTicks as t (t)}
					<text class="tick" x={x(t)} y={innerH + 20} text-anchor="middle">{formatInteger(t)}</text>
				{/each}

				{#if hp}
					<line class="cross" x1={x(hp.b)} x2={x(hp.b)} y1="0" y2={innerH} />
					<circle class="hdot" class:neg={hp.gap < 0} cx={x(hp.b)} cy={y(hp.gap)} r="4.5" />
				{/if}
			</g>
			<rect x={M.left} y={M.top} width={innerW} height={innerH} fill="transparent" />
		</svg>
		{#if hp}
			<div class="tip" class:flip={tipLeft > width - 240} style:left="{tipLeft}px" style:top="{M.top}px" role="status" aria-live="polite">
				<p class="tb mono">Block {formatInteger(hp.b)}</p>
				<p class="tt">{formatUtc(hp.raw.block_time)}</p>
				{#if hp.raw.label}<p class="tl">{hp.raw.label}</p>{/if}
				<dl>
					<div>
						<dt>Gap</dt>
						<dd class="num" class:neg={hp.gap < 0}>{formatTokens(hp.gapBig, decimals, { sign: true })} {symbol}</dd>
					</div>
					<div><dt>Owed</dt><dd class="num">{formatTokens(parseAmount(hp.raw.owed), decimals)}</dd></div>
					<div><dt>Held</dt><dd class="num">{formatTokens(parseAmount(hp.raw.held), decimals)}</dd></div>
					{#if hp.raw.status}<div><dt>Status</dt><dd>{statusMeta(hp.raw.status).label}</dd></div>{/if}
				</dl>
			</div>
		{/if}
	{/if}
</div>

<style>
	.chart {
		position: relative;
		width: 100%;
	}
	svg {
		display: block;
		overflow: visible;
		touch-action: pan-y;
		user-select: none;
	}
	svg:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 4px;
	}
	.grid {
		stroke: var(--grid);
		shape-rendering: crispEdges;
	}
	.zero {
		stroke: var(--axis);
		shape-rendering: crispEdges;
	}
	.tick {
		fill: var(--text-3);
		font-size: 11px;
		font-variant-numeric: tabular-nums;
	}
	.area.pos {
		fill: var(--gap-pos);
		opacity: 0.12;
	}
	.area.neg {
		fill: var(--gap-neg);
		opacity: 0.16;
	}
	.ln {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
	}
	.ln.pos {
		stroke: var(--gap-pos);
	}
	.ln.neg {
		stroke: var(--gap-neg);
	}
	.pt {
		fill: var(--gap-pos);
		stroke: var(--surface);
		stroke-width: 1.5;
	}
	.pt.neg {
		fill: var(--gap-neg);
	}
	.marker line {
		stroke-width: 1;
		stroke-dasharray: 3 3;
	}
	.marker text {
		font-size: 11px;
		font-weight: 600;
		fill: var(--text);
	}
	.marker.warn line,
	.marker.warn circle {
		stroke: var(--warn-mark);
		fill: var(--warn-mark);
	}
	.marker.bad line,
	.marker.bad circle {
		stroke: var(--bad-mark);
		fill: var(--bad-mark);
	}
	.marker.ok line,
	.marker.ok circle {
		stroke: var(--ok-mark);
		fill: var(--ok-mark);
	}
	.marker.neutral line,
	.marker.neutral circle {
		stroke: var(--text-3);
		fill: var(--text-3);
	}
	.cross {
		stroke: var(--text-3);
		opacity: 0.6;
	}
	.hdot {
		fill: var(--gap-pos);
		stroke: var(--surface);
		stroke-width: 2;
	}
	.hdot.neg {
		fill: var(--gap-neg);
	}
	.tip {
		position: absolute;
		transform: translateX(16px);
		width: 220px;
		padding: 10px 12px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: var(--shadow-pop);
		pointer-events: none;
		font-size: var(--text-xs);
		z-index: 5;
	}
	.tip.flip {
		transform: translateX(calc(-100% - 16px));
	}
	.tb {
		font-weight: 600;
		color: var(--text);
		font-size: 12px;
	}
	.tt {
		color: var(--text-3);
		font-size: 11px;
	}
	.tl {
		margin-top: 6px;
		color: var(--text-2);
		font-weight: 500;
	}
	.tip dl {
		margin-top: 8px;
		padding-top: 8px;
		border-top: 1px solid var(--border-soft);
		display: grid;
		gap: 4px;
	}
	.tip dl div {
		display: flex;
		justify-content: space-between;
		gap: 12px;
	}
	.tip dt {
		color: var(--text-3);
	}
	.tip dd {
		font-weight: 600;
		text-align: right;
	}
	.tip dd.neg {
		color: var(--bad);
	}
</style>
