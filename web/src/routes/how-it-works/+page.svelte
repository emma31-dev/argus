<script lang="ts">
	import { monitor } from '$lib/monitor.svelte';
	import { href, REPO_URL } from '$lib/links';
	import { exportCsvUrl } from '$lib/api';
	import Icon from '$lib/components/Icon.svelte';
	import SameBlock from '$lib/components/diagrams/SameBlock.svelte';
	import Debounce from '$lib/components/diagrams/Debounce.svelte';
	import StatusDot from '$lib/components/StatusDot.svelte';

	const k = $derived(monitor.status.data?.debounce_snapshots ?? 4);
	const interval = $derived(monitor.status.data?.interval_seconds ?? 30);

	const toc = [
		{ id: 'invariant', label: 'The invariant' },
		{ id: 'same-block', label: 'Same-block reads' },
		{ id: 'debounce', label: 'When it alerts' },
		{ id: 'coverage', label: 'What it covers' },
		{ id: 'context', label: 'Why Argus exists' },
		{ id: 'verify', label: 'Verify it yourself' }
	];

	type S = 'green' | 'amber' | 'red';
	const inflight: S[] = ['green', 'green', 'amber', 'amber', 'green', 'green', 'green', 'green', 'green', 'green'];
	const deficit: S[] = ['green', 'green', 'amber', 'amber', 'amber', 'red', 'red', 'red', 'red', 'red'];
</script>

<svelte:head>
	<title>How it works · Argus</title>
	<meta name="description" content="How Argus checks that NEAR Intents holds every token it owes: one invariant, read at the same block, with a debounced alert." />
</svelte:head>

<div class="container">
	<header class="hero">
		<p class="eyebrow">Methodology</p>
		<h1>One invariant, checked every block.</h1>
		<p class="lede">
			NEAR Intents keeps an internal ledger of every user's balance. Users are only safe if, for every token, the contract
			really holds at least what that ledger says it owes. Argus checks exactly that, in public, and keeps the history.
		</p>
	</header>

	<div class="layout">
		<aside class="toc" aria-label="On this page">
			<p class="eyebrow">On this page</p>
			<ol>
				{#each toc as item, i (item.id)}
					<li><a href="#{item.id}"><span class="num">{String(i + 1).padStart(2, '0')}</span>{item.label}</a></li>
				{/each}
			</ol>
		</aside>

		<article class="content">
			<section id="invariant" class="sec">
				<div class="invariant card">
					<p class="eyebrow">For every token T on intents.near</p>
					<p class="formula" aria-label="held of T is greater than or equal to owed of T">
						<span class="term held">held<span class="paren">(T)</span></span>
						<span class="op">≥</span>
						<span class="term owed">owed<span class="paren">(T)</span></span>
					</p>
					<div class="defs">
						<div>
							<p class="dk"><i class="key held"></i>held(T)</p>
							<p class="dv">The balance of <code>intents.near</code> on the token's own contract.</p>
							<p class="dc mono">ft_balance_of / mt_balance_of</p>
						</div>
						<div>
							<p class="dk"><i class="key owed"></i>owed(T)</p>
							<p class="dv">The total the Intents ledger says its users own, its internal supply of T.</p>
							<p class="dc mono">intents.near.mt_batch_supply</p>
						</div>
					</div>
				</div>
				<div class="prose">
					<p>
						The difference <strong>gap = held − owed</strong> is computed in exact integer arithmetic in the token's smallest
						unit (wNEAR has 24 decimals; no floating point anywhere). A <strong>positive gap is normal</strong>: fees,
						donations and dust leave the contract holding slightly more than it owes. Argus records it but never alerts on
						it. A <strong>negative gap</strong> means the ledger has promised users tokens that are not there.
					</p>
				</div>
			</section>

			<section id="same-block" class="sec">
				<h2><span class="sn">01</span>Both sides, same block</h2>
				<div class="prose">
					<p>
						Every {interval} seconds Argus takes the latest <strong>final</strong> block height <code>H</code> and reads
						both sides of the invariant at <code>block_id = H</code>, from the same RPC provider. Reading them at different
						blocks would show a false gap every time someone deposits or withdraws between the two calls.
					</p>
				</div>
				<div class="figure card"><SameBlock /></div>
				<p class="figcap">
					<code>owed</code> is batched for all tokens (50 per call). <code>held</code> depends on the standard:
					<code>ft_balance_of</code> for NEP-141 tokens such as wNEAR, USDC and bridged <code>*.omft.near</code> tokens;
					<code>mt_balance_of</code> with the inner token id for NEP-245 tokens such as HOT omni tokens.
				</p>
			</section>

			<section id="debounce" class="sec">
				<h2><span class="sn">02</span>When it alerts</h2>
				<div class="prose">
					<p>
						A deposit or withdrawal crosses contracts over a few blocks, so a single negative reading is often a transfer in
						flight, not a hole. Argus therefore debounces: one negative snapshot makes a token <strong>amber</strong>; it
						only turns <strong>red</strong> after <strong>{k} consecutive negative snapshots</strong> (about
						{Math.round((k * interval) / 60)} minutes) or if the deficit exceeds an absolute threshold. Red opens an incident,
						sends alerts and records the block range and the intents.near events inside it.
					</p>
				</div>
				<div class="figure card"><Debounce {k} intervalS={interval} /></div>

				<div class="strips card">
					<div class="strip">
						<div class="strip-head">
							<p class="sh">Deposit in flight</p>
							<p class="sd">Two negative readings, then the transfer settles. Amber, back to green, no alert.</p>
						</div>
						<ol class="cells" aria-label="Snapshots: {inflight.join(', ')}">
							{#each inflight as s, i (i)}
								<li class={s}><span class="num">{i + 1}</span></li>
							{/each}
						</ol>
					</div>
					<div class="strip">
						<div class="strip-head">
							<p class="sh">Real deficit</p>
							<p class="sd">The {k === 4 ? 'fourth' : `${k}th`} consecutive negative reading turns red and opens an incident.</p>
						</div>
						<ol class="cells" aria-label="Snapshots: {deficit.join(', ')}">
							{#each deficit as s, i (i)}
								<li class={s}><span class="num">{i + 1}</span></li>
							{/each}
						</ol>
					</div>
					<ul class="strip-legend">
						<li><StatusDot status="green" size={7} />held ≥ owed</li>
						<li><StatusDot status="amber" size={7} />negative, settling</li>
						<li><StatusDot status="red" size={7} />negative {k}× in a row</li>
						<li class="muted">one cell = one snapshot, {interval}s apart</li>
					</ul>
				</div>
			</section>

			<section id="coverage" class="sec">
				<h2><span class="sn">03</span>What it covers, and what it doesn't</h2>
				<div class="cov">
					<div class="card cov-card">
						<p class="cov-h"><span class="ci ok"><Icon name="check" size={14} stroke={2.25} /></span>Checks</p>
						<ul>
							<li>Every fungible token (<code>nep141</code>) the Intents ledger tracks</li>
							<li>Multi-tokens (<code>nep245</code>), such as HOT omni tokens, by inner id</li>
							<li>Both sides at the same final block, from the same provider</li>
							<li>A full history, keeping every negative snapshot, as JSON and CSV</li>
							<li>Incidents with block range, max deficit and intents.near events</li>
						</ul>
					</div>
					<div class="card cov-card">
						<p class="cov-h"><span class="ci no"><Icon name="x" size={14} stroke={2.25} /></span>Does not check</p>
						<ul>
							<li>
								<strong>Origin-chain backing of bridged tokens.</strong> Argus checks the NEAR-side layer: is the ledger backed by
								the bridged token intents.near holds? Whether, say, <code>eth.omft.near</code> is backed by ETH on Ethereum is
								the bridge layer, which NEAR Intents already monitors.
							</li>
							<li><strong>NFTs</strong> (<code>nep171</code>) are out of scope.</li>
							<li>Individual user balances, or the correctness of any single trade.</li>
							<li>It never pauses, signs or sends anything: no keys, read-only.</li>
						</ul>
					</div>
				</div>
			</section>

			<section id="context" class="sec">
				<h2><span class="sn">04</span>Why Argus exists</h2>
				<div class="prose">
					<p>
						On <strong>2026-10-01</strong>, a deposit path in NEAR Intents notified a third-party contract. An attacker used
						a contract built to make that notification fail, so the token contract refunded the deposit while the Intents
						ledger kept the credit. Repeated across 23 failed deposits, this created roughly
						<strong>$3.8M of unbacked balances</strong>. The funds were returned within about 36 hours.
					</p>
					<p>
						That account comes from the official postmortem, <em>Postmortem: NEAR Intents October 1 Exploit</em> by Alex
						Shevchenko (2026-10-16). The fix removed deposit notifications altogether
						(<a class="link" href="https://github.com/near/intents/commit/16d04f6" target="_blank" rel="noopener noreferrer"><code>near/intents@16d04f6</code></a>,
						released as <code>defuse/v0.4.5</code> on 2026-10-07).
					</p>
					<p>
						The reported gap: monitoring compared each token's origin-chain balance with its NEAR-side total, but not users'
						internal balances against what the Intents contract actually held, which is exactly the invariant above.
						NEAR Intents says it has since added that check internally and auto-pauses on a discrepancy.
					</p>
				</div>
				<div class="disclaim">
					<Icon name="info" size={16} />
					<p>
						<strong>Argus did not discover this bug</strong>, and NEAR Intents already monitors this internally. What Argus
						adds is a check that anyone outside the team can see, re-run and be alerted by.
					</p>
				</div>
				<div class="values">
					<div class="card val">
						<p class="vh">Independence</p>
						<p class="vt">Run by someone other than the operator, from public RPC, with no keys and no special access.</p>
					</div>
					<div class="card val">
						<p class="vh">Transparency</p>
						<p class="vt">Every number links to its block and to the exact view call that produced it.</p>
					</div>
					<div class="card val">
						<p class="vh">History</p>
						<p class="vt">Every snapshot is stored, so a past gap can be audited after the fact, not just seen live.</p>
					</div>
				</div>
				<p class="sources">
					Also covered by
					<a class="link" href="https://theblock.co/news/ecosystems/2026-10-01-near-intents-halts-services-after-3-8-million-exploit-promises-full-compensation-417404" target="_blank" rel="noopener noreferrer">The Block</a>,
					<a class="link" href="https://thedefiant.io/news/security/near-intents-postmortem-links-3-8-million-exploit-to-deposit-bug" target="_blank" rel="noopener noreferrer">The Defiant</a>,
					<a class="link" href="https://www.kucoin.com/news/flash/near-intents-releases-post-mortem-report-on-3-8m-vulnerability" target="_blank" rel="noopener noreferrer">KuCoin News</a>.
					Which tokens and blocks were affected is not asserted here; the replay reads it from Argus's own archival run.
				</p>
				<a class="replay-cta card" href={href.replay()}>
					<span>
						<span class="eyebrow">Replay</span>
						<span class="rc-t">Would Argus have caught it? The October incident, block by block.</span>
					</span>
					<Icon name="arrowRight" size={16} />
				</a>
			</section>

			<section id="verify" class="sec">
				<h2><span class="sn">05</span>Verify it yourself</h2>
				<div class="prose">
					<p>
						Don't trust, verify. Every token page shows the exact RPC calls behind its latest number, ready to paste into a
						terminal. The data is public and unauthenticated.
					</p>
				</div>
				<div class="links">
					<a class="card lk" href={href.api()}>
						<Icon name="code" size={18} />
						<span><span class="lt">API reference</span><span class="ld">JSON endpoints for status, tokens, history and incidents</span></span>
					</a>
					<a class="card lk" href={exportCsvUrl()}>
						<Icon name="download" size={18} />
						<span><span class="lt">CSV export</span><span class="ld">Last 24 hours of snapshots for every token</span></span>
					</a>
					<a class="card lk" href={REPO_URL} target="_blank" rel="noopener noreferrer">
						<Icon name="file" size={18} />
						<span><span class="lt">Source code</span><span class="ld">The monitor, the replay and this dashboard</span></span>
					</a>
				</div>
			</section>
		</article>
	</div>
</div>

<style>
	.hero {
		padding: var(--s-16) 0 var(--s-10);
		max-width: 820px;
	}
	.hero h1 {
		margin-top: var(--s-3);
		font-size: var(--text-4xl);
		font-weight: 660;
		letter-spacing: -0.038em;
		line-height: 1.08;
	}
	.lede {
		margin-top: var(--s-5);
		font-size: var(--text-lg);
		line-height: 1.6;
		color: var(--text-2);
		max-width: 64ch;
	}

	.layout {
		display: grid;
		grid-template-columns: 200px minmax(0, 1fr);
		gap: var(--s-12);
		align-items: start;
	}
	.toc {
		position: sticky;
		top: calc(var(--nav-h) + var(--s-6));
	}
	.toc ol {
		margin-top: var(--s-3);
		display: grid;
		gap: 2px;
		border-left: 1px solid var(--border);
	}
	.toc a {
		display: flex;
		gap: 10px;
		padding: 5px 0 5px 14px;
		margin-left: -1px;
		border-left: 1px solid transparent;
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.toc a:hover {
		color: var(--text);
		border-left-color: var(--text);
	}
	.toc .num {
		color: var(--text-4);
		font-size: var(--text-xs);
		padding-top: 1px;
	}
	.content {
		max-width: 860px;
		min-width: 0;
	}
	.sec + .sec {
		margin-top: var(--s-16);
	}
	.sec {
		scroll-margin-top: calc(var(--nav-h) + var(--s-6));
	}
	h2 {
		display: flex;
		align-items: baseline;
		gap: var(--s-3);
		font-size: var(--text-2xl);
		font-weight: 640;
		letter-spacing: -0.028em;
		margin-bottom: var(--s-4);
	}
	.sn {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		color: var(--text-4);
		font-weight: 500;
		letter-spacing: 0;
	}

	.invariant {
		padding: var(--s-10) var(--s-8) var(--s-8);
		text-align: center;
		margin-bottom: var(--s-6);
		background:
			radial-gradient(ellipse at 50% 0%, var(--surface-2), transparent 70%),
			var(--surface);
	}
	.formula {
		margin: var(--s-5) 0 var(--s-8);
		display: flex;
		align-items: baseline;
		justify-content: center;
		gap: 0.4em;
		font-size: clamp(2.2rem, 7vw, 4.25rem);
		font-weight: 600;
		letter-spacing: -0.04em;
		line-height: 1;
		flex-wrap: wrap;
	}
	.term {
		white-space: nowrap;
	}
	.paren {
		color: var(--text-3);
		font-weight: 400;
	}
	.op {
		color: var(--text-3);
		font-weight: 400;
	}
	.held {
		background-image: linear-gradient(var(--series-held), var(--series-held));
		background-size: 100% 3px;
		background-repeat: no-repeat;
		background-position: 0 100%;
		padding-bottom: 6px;
	}
	.owed {
		background-image: repeating-linear-gradient(90deg, var(--series-owed) 0 8px, transparent 8px 13px);
		background-size: 100% 3px;
		background-repeat: no-repeat;
		background-position: 0 100%;
		padding-bottom: 6px;
	}
	.defs {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--s-4);
		text-align: left;
		border-top: 1px solid var(--border);
		padding-top: var(--s-6);
	}
	.dk {
		display: flex;
		align-items: center;
		gap: 8px;
		font-family: var(--font-mono);
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.key {
		display: inline-block;
		width: 16px;
		height: 2px;
	}
	.key.held {
		background: var(--series-held);
		padding: 0;
	}
	.key.owed {
		background: repeating-linear-gradient(90deg, var(--series-owed) 0 4px, transparent 4px 7px);
		padding: 0;
	}
	.dv {
		margin-top: 6px;
		color: var(--text-2);
		font-size: var(--text-sm);
	}
	.dv code {
		font-size: 0.92em;
	}
	.dc {
		margin-top: 6px;
		color: var(--text-3);
		font-size: 11.5px;
	}

	.figure {
		padding: var(--s-6);
		margin-top: var(--s-6);
	}
	.figcap {
		margin-top: var(--s-3);
		font-size: var(--text-sm);
		color: var(--text-3);
		line-height: var(--leading-loose);
	}
	.figcap code {
		color: var(--text-2);
		font-size: 0.88em;
	}

	.strips {
		margin-top: var(--s-4);
		padding: var(--s-6);
		display: grid;
		gap: var(--s-5);
	}
	.strip {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--s-4);
		align-items: center;
	}
	.sh {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.sd {
		font-size: var(--text-sm);
		color: var(--text-2);
		margin-top: 2px;
	}
	.cells {
		display: grid;
		grid-template-columns: repeat(10, 30px);
		gap: 4px;
	}
	.cells li {
		height: 30px;
		border-radius: 6px;
		display: grid;
		place-items: center;
		font-size: 10.5px;
		font-weight: 600;
		border: 1px solid;
	}
	.cells .green {
		background: var(--ok-bg);
		border-color: var(--ok-border);
		color: var(--ok);
	}
	.cells .amber {
		background: var(--warn-mark);
		border-color: var(--warn-mark);
		color: #fff;
	}
	.cells .red {
		background: var(--bad-mark);
		border-color: var(--bad-mark);
		color: #fff;
	}
	.strip-legend {
		display: flex;
		flex-wrap: wrap;
		gap: 6px 18px;
		padding-top: var(--s-4);
		border-top: 1px solid var(--border-soft);
		font-size: var(--text-xs);
		color: var(--text-2);
	}
	.strip-legend li {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}

	.cov {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--s-4);
	}
	.cov-card {
		padding: var(--s-6);
	}
	.cov-h {
		display: flex;
		align-items: center;
		gap: 10px;
		font-weight: 600;
	}
	.ci {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border-radius: 6px;
	}
	.ci.ok {
		background: var(--ok-bg);
		color: var(--ok);
	}
	.ci.no {
		background: var(--surface-3);
		color: var(--text-2);
	}
	.cov ul {
		margin-top: var(--s-4);
		display: grid;
		gap: var(--s-3);
	}
	.cov li {
		position: relative;
		padding-left: 16px;
		font-size: var(--text-sm);
		color: var(--text-2);
		line-height: 1.6;
	}
	.cov li::before {
		content: '';
		position: absolute;
		left: 2px;
		top: 0.7em;
		width: 5px;
		height: 1px;
		background: var(--text-4);
	}
	.cov li strong {
		color: var(--text);
		font-weight: 600;
	}
	.cov code {
		font-size: 0.88em;
		color: var(--text);
	}

	.disclaim {
		display: flex;
		gap: var(--s-3);
		margin-top: var(--s-6);
		padding: var(--s-4) var(--s-5);
		border-radius: var(--r);
		border: 1px solid var(--border);
		background: var(--surface-2);
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.disclaim :global(svg) {
		flex: none;
		margin-top: 2px;
		color: var(--text-3);
	}
	.disclaim strong {
		color: var(--text);
	}
	.values {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: var(--s-3);
		margin-top: var(--s-4);
	}
	.val {
		padding: var(--s-5);
	}
	.vh {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.vt {
		margin-top: 4px;
		font-size: var(--text-sm);
		color: var(--text-2);
		line-height: 1.55;
	}
	.sources {
		margin-top: var(--s-4);
		font-size: var(--text-xs);
		color: var(--text-3);
	}
	.sources .link {
		color: var(--text-2);
	}
	.replay-cta {
		margin-top: var(--s-5);
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--s-4);
		padding: var(--s-5) var(--s-6);
		transition: border-color var(--dur) var(--ease);
	}
	.replay-cta:hover {
		border-color: var(--text-3);
	}
	.replay-cta > span {
		display: grid;
		gap: 4px;
	}
	.rc-t {
		font-weight: 600;
	}

	.links {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: var(--s-3);
		margin-top: var(--s-5);
	}
	.lk {
		display: flex;
		gap: var(--s-3);
		padding: var(--s-5);
		transition: border-color var(--dur) var(--ease);
	}
	.lk:hover {
		border-color: var(--text-3);
	}
	.lk :global(svg) {
		flex: none;
		color: var(--text-2);
		margin-top: 1px;
	}
	.lk > span {
		display: grid;
		gap: 2px;
	}
	.lt {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.ld {
		font-size: var(--text-xs);
		color: var(--text-3);
		line-height: 1.5;
	}

	@media (max-width: 1080px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.toc {
			display: none;
		}
	}
	@media (max-width: 760px) {
		.hero {
			padding: var(--s-10) 0 var(--s-8);
		}
		.hero h1 {
			font-size: var(--text-3xl);
		}
		.lede {
			font-size: var(--text-md);
		}
		.cov,
		.values,
		.links,
		.defs {
			grid-template-columns: 1fr;
		}
		.invariant {
			padding: var(--s-8) var(--s-5) var(--s-6);
		}
		.figure {
			padding: var(--s-4);
		}
		.strip {
			grid-template-columns: 1fr;
		}
		.cells {
			grid-template-columns: repeat(10, minmax(0, 1fr));
		}
		.strips {
			padding: var(--s-4);
		}
		.sec + .sec {
			margin-top: var(--s-12);
		}
		h2 {
			font-size: var(--text-xl);
		}
	}
</style>
