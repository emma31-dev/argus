<script lang="ts">
	import type { Snapshot, Token } from '$lib/api';
	import { formatInteger } from '$lib/format';
	import { INTENTS_CONTRACT } from '$lib/links';
	import Code from './Code.svelte';
	import BlockLink from './BlockLink.svelte';

	/**
	 * "How this number is computed": the exact view calls Argus makes, at the
	 * exact block, so anyone can re-run them against any NEAR RPC node.
	 */
	let {
		token,
		snapshot,
		rpcUrl = 'https://free.rpc.fastnear.com'
	}: { token: Token; snapshot: Snapshot | null; rpcUrl?: string } = $props();

	const block = $derived(snapshot?.block_height ?? null);
	const blockArg = $derived(block ?? '<block_height>');

	const owedArgs = $derived({ token_ids: [token.id] });
	const heldMethod = $derived(token.standard === 'nep245' ? 'mt_balance_of' : 'ft_balance_of');
	const heldArgs = $derived(
		token.standard === 'nep245'
			? { account_id: INTENTS_CONTRACT, token_id: token.inner_id }
			: { account_id: INTENTS_CONTRACT }
	);

	function b64(obj: unknown) {
		const bytes = new TextEncoder().encode(JSON.stringify(obj));
		let s = '';
		for (const b of bytes) s += String.fromCharCode(b);
		return btoa(s);
	}

	function curl(account: string, method: string, args: unknown) {
		const body = {
			jsonrpc: '2.0',
			id: 'argus',
			method: 'query',
			params: {
				request_type: 'call_function',
				block_id: block ?? 0,
				account_id: account,
				method_name: method,
				args_base64: b64(args)
			}
		};
		return `curl -s ${rpcUrl} \\\n  -H 'content-type: application/json' \\\n  -d '${JSON.stringify(body)}' \\\n  | jq -r '.result.result | implode'`;
	}

	const owedCall = $derived(`${INTENTS_CONTRACT}.mt_batch_supply(${JSON.stringify(owedArgs)})\n  at block_id ${blockArg}`);
	const heldCall = $derived(`${token.contract}.${heldMethod}(${JSON.stringify(heldArgs)})\n  at block_id ${blockArg}`);
	const compare = $derived(
		snapshot
			? `held  ${snapshot.held}\nowed  ${snapshot.owed}\ngap   ${snapshot.gap}   (held − owed, smallest units)`
			: 'No snapshot yet.'
	);
</script>

<div class="proof">
	<p class="intro">
		Both sides are read from the same RPC provider at the same block
		{#if block}(<BlockLink height={block} />){/if}, so deposits and withdrawals in flight can't create a false gap.
		Re-run these calls against any NEAR archival or recent-state RPC node to check the numbers yourself.
	</p>

	<ol class="steps">
		<li>
			<span class="n">1</span>
			<div class="step">
				<p class="h">Owed: total the ledger says users own</p>
				<p class="d">NEP-245 total supply of this token inside <code>{INTENTS_CONTRACT}</code>.</p>
				<Code code={owedCall} label="View call" lang="near" />
				<details>
					<summary>JSON-RPC request</summary>
					<Code code={curl(INTENTS_CONTRACT, 'mt_batch_supply', owedArgs)} lang="sh" />
				</details>
			</div>
		</li>
		<li>
			<span class="n">2</span>
			<div class="step">
				<p class="h">Held: tokens the contract actually has</p>
				<p class="d">
					Balance of <code>{INTENTS_CONTRACT}</code> on the token's own
					{token.standard === 'nep245' ? 'NEP-245 multi-token' : 'NEP-141 fungible-token'} contract.
				</p>
				<Code code={heldCall} label="View call" lang="near" />
				<details>
					<summary>JSON-RPC request</summary>
					<Code code={curl(token.contract, heldMethod, heldArgs)} lang="sh" />
				</details>
			</div>
		</li>
		<li>
			<span class="n">3</span>
			<div class="step">
				<p class="h">Gap = held − owed</p>
				<p class="d">
					Exact integer arithmetic in the smallest unit (10<sup>−{token.decimals}</sup> {token.symbol}). A negative
					gap means the ledger owes more than it holds.
					{#if block}Snapshot at block {formatInteger(block)}.{/if}
				</p>
				<Code code={compare} label="Result" />
			</div>
		</li>
	</ol>
</div>

<style>
	.proof {
		display: grid;
		gap: var(--s-5);
	}
	.intro {
		color: var(--text-2);
		font-size: var(--text-sm);
		line-height: var(--leading-loose);
	}
	.steps {
		display: grid;
		gap: var(--s-5);
	}
	.steps li {
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr);
		gap: var(--s-3);
	}
	.n {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		border: 1px solid var(--border-strong);
		font-size: var(--text-2xs);
		font-weight: 600;
		color: var(--text-2);
		margin-top: 1px;
	}
	.step {
		display: grid;
		gap: var(--s-2);
		min-width: 0;
	}
	.h {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.d {
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.d code {
		font-size: 0.9em;
		color: var(--text);
	}
	details summary {
		cursor: pointer;
		font-size: var(--text-xs);
		color: var(--text-3);
		font-weight: 500;
		padding: 4px 0;
		width: max-content;
	}
	details summary:hover {
		color: var(--text);
	}
	details[open] summary {
		margin-bottom: var(--s-2);
	}
</style>
