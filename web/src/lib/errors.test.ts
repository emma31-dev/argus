import { describe, expect, it } from 'vitest';
import { hostOf, shortError, summarizeReadError } from './errors';

const RAW =
	'rpc: block failed on 3 provider(s): rpc: provider unavailable (provider free.rpc.fastnear.com, method block): Post "https://free.rpc.fastnear.com": Forbidden\n' +
	'rpc: provider unavailable (provider rpc.mainnet.fastnear.com, method block): Post "https://rpc.mainnet.fastnear.com": Forbidden\nrpc: provider ';

describe('errors', () => {
	it('extracts the reason from a provider error', () => {
		expect(
			shortError('rpc: provider unavailable (provider x, method block): Post "https://rpc.mainnet.near.org": Forbidden')
		).toBe('Forbidden');
		expect(shortError('context deadline exceeded')).toBe('context deadline exceeded');
		expect(shortError('')).toBe('');
	});
	it('summarizes a multi-provider failure in one line', () => {
		expect(summarizeReadError(RAW)).toBe('All 3 RPC providers failed for this round (Forbidden).');
		expect(summarizeReadError('rpc: block failed on 1 provider(s): x: timeout')).toBe(
			'The only RPC provider failed for this round (timeout).'
		);
		expect(summarizeReadError('')).toBe('The last read of this token failed.');
		expect(summarizeReadError('rpc fastnear: context deadline exceeded after 10s')).toBe(
			'The last read failed: context deadline exceeded after 10s.'
		);
	});
	it('reduces urls to hosts', () => {
		expect(hostOf('https://free.rpc.fastnear.com')).toBe('free.rpc.fastnear.com');
		expect(hostOf('not a url')).toBe('not a url');
	});
});
