import { describe, expect, it } from 'vitest';
import { chainLabel, chainOf } from './chain';

describe('chainOf', () => {
	it('maps native NEAR tokens', () => {
		expect(chainOf('nep141:wrap.near')).toBe('NEAR');
		expect(chainOf('nep141:usdt.tether-token.near')).toBe('NEAR');
		expect(chainOf('nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1')).toBe('NEAR');
	});
	it('maps omft bridged tokens by prefix', () => {
		expect(chainOf('nep141:eth.omft.near')).toBe('Ethereum');
		expect(chainOf('nep141:eth-0xdac17f958d2ee523a2206206994597c13d831ec7.omft.near')).toBe('Ethereum');
		expect(chainOf('nep141:sol-c800a4bd850783ccb82c2b2c7e84175443606352.omft.near')).toBe('Solana');
		expect(chainOf('nep141:tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near')).toBe('Tron');
		expect(chainOf('nep141:base.omft.near')).toBe('Base');
		expect(chainOf('nep141:arb-0xaf88d065e77c8cc2239327c5edb3a432268e5831.omft.near')).toBe('Arbitrum');
		expect(chainOf('nep141:btc.omft.near')).toBe('Bitcoin');
	});
	it('maps Rainbow / Omni bridge tokens', () => {
		expect(chainOf('nep141:eth.bridge.near')).toBe('Ethereum (Rainbow)');
		expect(chainOf('nep141:nbtc.bridge.near')).toBe('Bitcoin (Omni)');
	});
	it('maps HOT omni nep245 tokens by EVM chain id', () => {
		expect(chainOf('nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g')).toBe('BNB Chain');
		expect(chainOf('nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr')).toBe('Polygon');
		expect(chainOf('nep245:v2_1.omni.hot.tg:999999_x')).toBe('HOT Omni 999999');
	});
	it('prefers the API label', () => {
		expect(chainLabel('nep141:wrap.near', 'NEAR Protocol')).toBe('NEAR Protocol');
		expect(chainLabel('nep141:sol.omft.near')).toBe('Solana');
	});
});
