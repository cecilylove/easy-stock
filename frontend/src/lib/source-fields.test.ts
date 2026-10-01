import { describe, expect, it } from 'vitest';
import type { SourceMeta } from './backend';
import { sourceFieldAvailable, sourceVolumeInShares, priceBasis } from './source-fields';
const meta: SourceMeta = { source: 'tencent:stock-kline', fetched_at:'2026-09-30T15:00:00+08:00',latency_ms:0,stale:false };
describe('source field and price convention contract', () => {
	it('uses row validity before list fields and preserves known-empty', () => {
		expect(sourceFieldAvailable({...meta,fields_known:true,available_fields:[]},'amount',{...meta,available_fields:['amount']})).toBe(false);
		expect(sourceFieldAvailable({...meta,fields_known:true,available_fields:['volume']},'volume')).toBe(true);
		expect(sourceFieldAvailable(meta,'amount')).toBe(true);
	});
	it('prefers explicit share units over legacy provider conversion', () => {
		expect(sourceVolumeInShares(100,{...meta,source:'eastmoney',volume_unit:'shares'})).toBe(100);
		expect(sourceVolumeInShares(100,{...meta,volume_unit:'lots'})).toBe(10000);
		expect(sourceVolumeInShares(100,{...meta,volume_unit:'unknown'})).toBeNull();
		expect(priceBasis({...meta,basis_id:'tencent:hfq'})).not.toBe(priceBasis({...meta,basis_id:'tencent:qfq'}));
	});
});
