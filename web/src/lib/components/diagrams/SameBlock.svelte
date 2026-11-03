<script lang="ts">
	/** Diagram: both sides of the invariant read at the same final block H. */
</script>

<figure class="dg">
	<!-- Wide layout -->
	<svg class="wide" viewBox="0 0 960 400" role="img" aria-labelledby="sb-t sb-d">
		<title id="sb-t">Same-block read</title>
		<desc id="sb-d">
			At final block H, Argus calls intents.near mt_batch_supply to get owed, and the token contract's
			ft_balance_of for intents.near to get held, then computes gap equals held minus owed.
		</desc>
		<defs>
			<marker id="sb-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
				<path d="M0 1 L9 5 L0 9 Z" class="ah" />
			</marker>
		</defs>

		<!-- chain -->
		<line x1="40" y1="52" x2="920" y2="52" class="rail" />
		<g class="blk faded"><rect x="140" y="24" width="120" height="56" rx="8" /><text x="200" y="47" class="bl">H − 2</text><text x="200" y="65" class="bs">final</text></g>
		<g class="blk"><rect x="280" y="24" width="120" height="56" rx="8" /><text x="340" y="47" class="bl">H − 1</text><text x="340" y="65" class="bs">final</text></g>
		<g class="blk on"><rect x="420" y="20" width="120" height="64" rx="9" /><text x="480" y="46" class="bl">Block H</text><text x="480" y="66" class="bs">latest final</text></g>
		<g class="blk next"><rect x="560" y="24" width="120" height="56" rx="8" /><text x="620" y="47" class="bl">H + 1</text><text x="620" y="65" class="bs">not final yet</text></g>

		<!-- fan out -->
		<path d="M480 84 C480 130 240 118 240 164" class="flow" marker-end="url(#sb-arrow)" />
		<path d="M480 84 C480 130 720 118 720 164" class="flow" marker-end="url(#sb-arrow)" />
		<text x="480" y="128" class="cap" text-anchor="middle">block_id = H</text>

		<!-- left: owed -->
		<g class="box">
			<rect x="60" y="168" width="360" height="100" rx="10" />
			<text x="84" y="198" class="acct">intents.near</text>
			<text x="84" y="222" class="call">mt_batch_supply({'{'}token_ids: [T]{'}'})</text>
			<text x="84" y="250" class="what">What the ledger says users own</text>
			<g class="chip owed"><rect x="322" y="183" width="78" height="24" rx="12" /><line x1="334" y1="195" x2="348" y2="195" /><text x="354" y="199">owed</text></g>
		</g>
		<!-- right: held -->
		<g class="box">
			<rect x="540" y="168" width="360" height="100" rx="10" />
			<text x="564" y="198" class="acct">wrap.near (token contract)</text>
			<text x="564" y="222" class="call">ft_balance_of({'{'}account_id: "intents.near"{'}'})</text>
			<text x="564" y="250" class="what">What the contract actually holds</text>
			<g class="chip held"><rect x="808" y="183" width="74" height="24" rx="12" /><line x1="820" y1="195" x2="834" y2="195" /><text x="840" y="199">held</text></g>
		</g>

		<!-- merge -->
		<path d="M240 268 C240 318 290 342 336 342" class="flow" marker-end="url(#sb-arrow)" />
		<path d="M720 268 C720 318 670 342 624 342" class="flow" marker-end="url(#sb-arrow)" />
		<g class="cmp">
			<rect x="340" y="314" width="280" height="56" rx="10" />
			<text x="480" y="338" class="eq" text-anchor="middle">gap = held − owed</text>
			<text x="480" y="358" class="sub" text-anchor="middle"><tspan class="g">≥ 0 backed</tspan>  ·  <tspan class="r">&lt; 0 deficit</tspan></text>
		</g>
	</svg>

	<!-- Narrow layout -->
	<svg class="narrow" viewBox="0 0 360 560" role="img" aria-labelledby="sbn-t">
		<title id="sbn-t">Same-block read: owed from intents.near and held from the token contract, both at block H</title>
		<defs>
			<marker id="sbn-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
				<path d="M0 1 L9 5 L0 9 Z" class="ah" />
			</marker>
		</defs>
		<line x1="0" y1="48" x2="360" y2="48" class="rail" />
		<g class="blk"><rect x="16" y="22" width="92" height="52" rx="8" /><text x="62" y="44" class="bl">H − 1</text><text x="62" y="62" class="bs">final</text></g>
		<g class="blk on"><rect x="128" y="18" width="104" height="60" rx="9" /><text x="180" y="43" class="bl">Block H</text><text x="180" y="62" class="bs">latest final</text></g>
		<g class="blk next"><rect x="252" y="22" width="92" height="52" rx="8" /><text x="298" y="44" class="bl">H + 1</text><text x="298" y="62" class="bs">pending</text></g>

		<path d="M180 78 L180 112" class="flow" marker-end="url(#sbn-arrow)" />
		<text x="190" y="100" class="cap">block_id = H</text>

		<g class="box">
			<rect x="16" y="118" width="328" height="112" rx="10" />
			<text x="34" y="146" class="acct">intents.near</text>
			<text x="34" y="170" class="call">mt_batch_supply({'{'}token_ids: [T]{'}'})</text>
			<text x="34" y="210" class="what">What the ledger says users own</text>
			<g class="chip owed"><rect x="250" y="132" width="78" height="24" rx="12" /><line x1="262" y1="144" x2="276" y2="144" /><text x="282" y="148">owed</text></g>
		</g>
		<g class="box">
			<rect x="16" y="246" width="328" height="112" rx="10" />
			<text x="34" y="274" class="acct">wrap.near</text>
			<text x="34" y="298" class="call">ft_balance_of({'{'}account_id:</text>
			<text x="34" y="316" class="call">  "intents.near"{'}'})</text>
			<text x="34" y="340" class="what">What the contract actually holds</text>
			<g class="chip held"><rect x="254" y="260" width="74" height="24" rx="12" /><line x1="266" y1="272" x2="280" y2="272" /><text x="286" y="276">held</text></g>
		</g>
		<path d="M180 358 L180 410" class="flow" marker-end="url(#sbn-arrow)" />
		<g class="cmp">
			<rect x="40" y="416" width="280" height="60" rx="10" />
			<text x="180" y="442" class="eq" text-anchor="middle">gap = held − owed</text>
			<text x="180" y="463" class="sub" text-anchor="middle"><tspan class="g">≥ 0 backed</tspan>  ·  <tspan class="r">&lt; 0 deficit</tspan></text>
		</g>
		<text x="180" y="512" class="cap" text-anchor="middle">Same provider, same block, every token.</text>
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
		max-width: 420px;
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
	.rail {
		stroke: var(--border-strong);
		stroke-width: 1;
		stroke-dasharray: 2 4;
	}
	.blk rect {
		fill: var(--surface);
		stroke: var(--border-strong);
	}
	.blk .bl {
		font-size: 13px;
		font-weight: 600;
		fill: var(--text);
		text-anchor: middle;
	}
	.blk .bs {
		font-size: 10.5px;
		fill: var(--text-3);
		text-anchor: middle;
	}
	.blk.faded {
		opacity: 0.55;
	}
	.blk.next rect {
		stroke-dasharray: 4 3;
		fill: transparent;
	}
	.blk.next text {
		fill: var(--text-3);
	}
	.blk.on rect {
		fill: var(--text);
		stroke: var(--text);
	}
	.blk.on .bl {
		fill: var(--text-inverse);
	}
	.blk.on .bs {
		fill: var(--text-inverse);
		opacity: 0.7;
	}
	.flow {
		fill: none;
		stroke: var(--text-3);
		stroke-width: 1.5;
	}
	.ah {
		fill: var(--text-3);
	}
	.cap {
		font-size: 11px;
		fill: var(--text-3);
		font-family: var(--font-mono);
	}
	.box rect {
		fill: var(--surface);
		stroke: var(--border-strong);
	}
	.acct {
		font-size: 14px;
		font-weight: 650;
		fill: var(--text);
	}
	.call {
		font-size: 12px;
		fill: var(--text-2);
		font-family: var(--font-mono);
	}
	.what {
		font-size: 12px;
		fill: var(--text-3);
	}
	.chip rect {
		fill: var(--surface-2);
		stroke: var(--border);
	}
	.chip text {
		font-size: 12px;
		font-weight: 600;
		fill: var(--text);
	}
	.chip line {
		stroke-width: 2;
		stroke-linecap: round;
	}
	.chip.owed line {
		stroke: var(--series-owed);
		stroke-dasharray: 4 3;
	}
	.chip.held line {
		stroke: var(--series-held);
	}
	.cmp rect {
		fill: var(--surface-2);
		stroke: var(--text);
		stroke-width: 1.25;
	}
	.eq {
		font-size: 15px;
		font-weight: 650;
		fill: var(--text);
		font-family: var(--font-mono);
	}
	.sub {
		font-size: 11.5px;
		fill: var(--text-3);
	}
	.g {
		fill: var(--ok);
		font-weight: 600;
	}
	.r {
		fill: var(--bad);
		font-weight: 600;
	}
</style>
