<script lang="ts">
	import { page } from '$app/state';
	import { href } from '$lib/links';
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head><title>{page.status} · Argus</title></svelte:head>

<div class="container">
	<EmptyState icon={page.status === 404 ? 'search' : 'alert'} title={page.status === 404 ? 'Page not found' : 'Something went wrong'}>
		{page.status === 404 ? 'There is nothing at this address.' : (page.error?.message ?? 'An unexpected error occurred.')}
		{#snippet actions()}<a class="btn" href={href.home()}>Back to overview</a>{/snippet}
	</EmptyState>
</div>
