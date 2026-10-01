import type { SourceMeta } from './backend';

// Prefer per-row presence over list capability schema. Explicit known-empty is
// no valid data, while absent masks keep compatibility with legacy responses.
export function sourceFieldAvailable(row: SourceMeta | null | undefined, field: string, list?: SourceMeta | null) {
	const meta = row?.fields_known || row?.available_fields?.length ? row : list;
	if (!meta) return true;
	if (meta.fields_known) return (meta.available_fields || []).includes(field);
	return !meta.available_fields?.length || meta.available_fields.includes(field);
}

export function sourceVolumeInShares(volume: number, meta: SourceMeta | undefined) {
	if (!meta) return null;
	if (meta.volume_unit === 'shares') return volume;
	if (meta.volume_unit === 'lots') return volume * 100;
	// Legacy suppliers retained their original units before the explicit contract.
	if (!meta.volume_unit && meta.source === 'sina') return volume;
	if (!meta.volume_unit && meta.source === 'eastmoney') return volume * 100;
	return null;
}

export function priceBasis(meta: SourceMeta | undefined) {
	return meta?.basis_id || `${meta?.source || ''}:${meta?.effective_adjustment || ''}:${meta?.volume_unit || ''}`;
}
