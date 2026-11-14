<script lang="ts">
	import type { ApiError } from '#lib/api.ts';
	import { href } from '#lib/links.ts';
	import Banner from './Banner.svelte';
	import Icon from './Icon.svelte';

	let {
		error,
		retry,
		hasData = false
	}: { error: ApiError; retry: () => void; hasData?: boolean } = $props();
	let busy = $state(false);
	async function onRetry() {
		busy = true;
		try {
			await retry();
		} finally {
			busy = false;
		}
	}
</script>

{#if error.offline}
	<Banner tone="bad" icon="offline" title="Monitor offline">
		The Argus API could not be reached{hasData ? ', so the numbers below are from the last successful check' : ''}.
		Argus will keep retrying automatically.
		{#snippet action()}
			<div class="acts">
				{#if !hasData}
					<a class="btn btn-ghost" href={`${href.home()}#setup`}>Run it locally</a>
				{/if}
				<button class="btn" type="button" onclick={onRetry} disabled={busy}>
					<Icon name="refresh" size={14} />{busy ? 'Retrying…' : 'Retry now'}
				</button>
			</div>
		{/snippet}
	</Banner>
{:else}
	<Banner tone="bad" title="The API returned an error">
		<span class="mono">{error.status}</span> · {error.message}
		{#snippet action()}
			<button class="btn" type="button" onclick={onRetry} disabled={busy}>
				<Icon name="refresh" size={14} />{busy ? 'Retrying…' : 'Retry'}
			</button>
		{/snippet}
	</Banner>
{/if}

<style>
	.acts {
		display: flex;
		align-items: center;
		gap: var(--s-2);
		flex-wrap: wrap;
	}
</style>
