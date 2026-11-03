<script lang="ts">
	import type { ApiError } from '$lib/api';
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
			<button class="btn" type="button" onclick={onRetry} disabled={busy}>
				<Icon name="refresh" size={14} />{busy ? 'Retrying…' : 'Retry now'}
			</button>
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
