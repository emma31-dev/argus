<script lang="ts">
	/** Diagram: the debounce state machine (green -> amber -> red -> green). */
	let { k = 4, intervalS = 30 }: { k?: number; intervalS?: number } = $props();
	const mins = $derived(Math.round((k * intervalS) / 60));
</script>

<figure class="dg">
	<svg class="wide" viewBox="0 72 900 216" role="img" aria-labelledby="db-t db-d">
		<title id="db-t">Alert state machine</title>
		<desc id="db-d">
			Backed turns Settling when the gap goes below zero. Settling turns Deficit after {k} consecutive negative
			snapshots. Settling returns to Backed without an alert if the gap recovers. Deficit returns to Backed when the
			gap recovers, closing the incident.
		</desc>
		<defs>
			<marker id="db-a" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
				<path d="M0 1 L9 5 L0 9 Z" class="ah" />
			</marker>
		</defs>

		<!-- forward transitions -->
		<path d="M244 120 L346 120" class="fw" marker-end="url(#db-a)" />
		<text x="295" y="110" class="tl" text-anchor="middle">gap &lt; 0</text>
		<text x="295" y="140" class="tk" text-anchor="middle">1 snapshot</text>

		<path d="M554 120 L656 120" class="fw" marker-end="url(#db-a)" />
		<text x="605" y="110" class="tl" text-anchor="middle">{k} in a row</text>
		<text x="605" y="140" class="tk" text-anchor="middle">≈ {mins} min, or</text>
		<text x="605" y="155" class="tk" text-anchor="middle">over threshold</text>

		<!-- return transitions -->
		<path d="M450 172 C450 222 180 222 180 176" class="bk" marker-end="url(#db-a)" />
		<text x="315" y="200" class="tl" text-anchor="middle">gap ≥ 0 · settled, no alert</text>

		<path d="M760 172 C760 280 110 280 110 176" class="bk" marker-end="url(#db-a)" />
		<text x="435" y="276" class="tl" text-anchor="middle">gap ≥ 0 · incident closed with duration and max deficit</text>

		<!-- nodes -->
		<g class="node ok">
			<rect x="40" y="88" width="204" height="84" rx="12" />
			<circle cx="66" cy="114" r="6" />
			<text x="82" y="119" class="nt">Backed</text>
			<text x="58" y="146" class="ns">held ≥ owed</text>
			<text x="58" y="162" class="ns">recorded, no alert</text>
		</g>
		<g class="node warn">
			<rect x="350" y="88" width="204" height="84" rx="12" />
			<circle cx="376" cy="114" r="6" />
			<text x="392" y="119" class="nt">Settling</text>
			<text x="368" y="146" class="ns">gap &lt; 0, waiting it out</text>
			<text x="368" y="162" class="ns">deposits take a few blocks</text>
		</g>
		<g class="node bad">
			<rect x="660" y="88" width="204" height="84" rx="12" />
			<circle cx="686" cy="114" r="6" />
			<text x="702" y="119" class="nt">Deficit</text>
			<text x="678" y="146" class="ns">incident opened</text>
			<text x="678" y="162" class="ns">alerts sent, range recorded</text>
		</g>
	</svg>

	<svg class="narrow" viewBox="0 0 340 520" role="img" aria-labelledby="dbn-t">
		<title id="dbn-t">Alert state machine: Backed to Settling on a negative gap, Settling to Deficit after {k} in a row, both return to Backed when the gap recovers</title>
		<defs>
			<marker id="dbn-a" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
				<path d="M0 1 L9 5 L0 9 Z" class="ah" />
			</marker>
		</defs>
		<g class="node ok">
			<rect x="16" y="16" width="220" height="76" rx="12" />
			<circle cx="40" cy="40" r="6" /><text x="56" y="45" class="nt">Backed</text>
			<text x="34" y="72" class="ns">held ≥ owed · no alert</text>
		</g>
		<path d="M80 92 L80 186" class="fw" marker-end="url(#dbn-a)" />
		<text x="92" y="132" class="tl">gap &lt; 0</text>
		<text x="92" y="150" class="tk">1 snapshot</text>
		<g class="node warn">
			<rect x="16" y="190" width="220" height="76" rx="12" />
			<circle cx="40" cy="214" r="6" /><text x="56" y="219" class="nt">Settling</text>
			<text x="34" y="246" class="ns">gap &lt; 0, waiting it out</text>
		</g>
		<path d="M80 266 L80 360" class="fw" marker-end="url(#dbn-a)" />
		<text x="92" y="306" class="tl">{k} in a row</text>
		<text x="92" y="324" class="tk">≈ {mins} min</text>
		<g class="node bad">
			<rect x="16" y="364" width="220" height="76" rx="12" />
			<circle cx="40" cy="388" r="6" /><text x="56" y="393" class="nt">Deficit</text>
			<text x="34" y="420" class="ns">incident opened · alerts</text>
		</g>
		<path d="M236 228 C280 228 280 60 240 60" class="bk" marker-end="url(#dbn-a)" />
		<path d="M236 402 C326 402 326 40 240 40" class="bk" marker-end="url(#dbn-a)" />
		<text x="170" y="476" class="tl" text-anchor="middle">Both return to Backed when gap ≥ 0.</text>
		<text x="170" y="496" class="tk" text-anchor="middle">From Deficit, that closes the incident.</text>
	</svg>
</figure>

<style>
	.dg {
		margin: 0;
	}
	svg {
		width: 100%;
		height: auto;
		font-family: var(--font-sans);
	}
	.narrow {
		display: none;
		max-width: 400px;
		margin: 0 auto;
	}
	@media (max-width: 640px) {
		.wide {
			display: none;
		}
		.narrow {
			display: block;
		}
	}
	.ah {
		fill: var(--text-3);
	}
	.fw {
		fill: none;
		stroke: var(--text-2);
		stroke-width: 1.5;
	}
	.bk {
		fill: none;
		stroke: var(--text-4);
		stroke-width: 1.25;
		stroke-dasharray: 4 4;
	}
	.tl {
		font-size: 12px;
		font-weight: 600;
		fill: var(--text);
	}
	.tk {
		font-size: 11px;
		fill: var(--text-3);
	}
	.node rect {
		stroke-width: 1.25;
	}
	.node .nt {
		font-size: 15px;
		font-weight: 650;
		fill: var(--text);
	}
	.node .ns {
		font-size: 11.5px;
		fill: var(--text-2);
	}
	.ok rect {
		fill: var(--ok-bg);
		stroke: var(--ok-border);
	}
	.ok circle {
		fill: var(--ok-mark);
	}
	.warn rect {
		fill: var(--warn-bg);
		stroke: var(--warn-border);
	}
	.warn circle {
		fill: var(--warn-mark);
	}
	.bad rect {
		fill: var(--bad-bg);
		stroke: var(--bad-border);
	}
	.bad circle {
		fill: var(--bad-mark);
	}
</style>
