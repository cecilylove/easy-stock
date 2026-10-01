import { useState } from 'react';

// Expand only on a real boundary crossing; never shrink on a shorter snapshot.
// Context changes (stock, period, trading date) start a fresh scale.
export function useExpandingChartValue(context: string, required: number, step: number, minimum: number, padding = 1) {
	const demand = Number.isFinite(required) && required >= 0 ? required : minimum;
	const bucket = Math.max(minimum, Math.ceil(demand * padding / step) * step);
	const [saved, setSaved] = useState({ context, value: bucket });
	const value = saved.context !== context ? bucket : required > saved.value ? Math.max(saved.value, bucket) : saved.value;
	if (saved.context !== context || saved.value !== value) setSaved({ context, value });
	return value;
}

export function useChartBaseline(context: string, candidate: number, verified = true) {
	const [saved, setSaved] = useState({ context, value: candidate, verified });
	if (saved.context !== context || saved.value <= 0 || (!saved.verified && verified)) {
		setSaved({ context, value: candidate, verified });
		return candidate;
	}
	return saved.value;
}
