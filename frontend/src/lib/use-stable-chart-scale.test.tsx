// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { useChartBaseline, useExpandingChartValue } from './use-stable-chart-scale';

it('keeps scales through corrections, expands only past bounds and resets on context change', () => {
	vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	const host = document.createElement('div'); const root = createRoot(host);
	function Probe({ context, required, baseline, verified }: { context: string; required: number; baseline: number; verified: boolean }) {
		const range = useExpandingChartValue(context, required, .5, .5, 1.18);
		const base = useChartBaseline(context, baseline, verified);
		return <span data-range={range} data-base={base} />;
	}
	const render = (context: string, required: number, baseline = 10, verified = true) => act(() => root.render(<Probe {...{ context, required, baseline, verified }} />));
	try {
		render('stock-a:day', 1); expect(host.firstElementChild?.getAttribute('data-range')).toBe('1.5');
		render('stock-a:day', .5, 11); expect(host.firstElementChild?.getAttribute('data-range')).toBe('1.5'); expect(host.firstElementChild?.getAttribute('data-base')).toBe('10');
		render('stock-a:day', 1.4); expect(host.firstElementChild?.getAttribute('data-range')).toBe('1.5');
		render('stock-a:day', 1.6); expect(host.firstElementChild?.getAttribute('data-range')).toBe('2');
		render('stock-b:day', .2, 20, false); expect(host.firstElementChild?.getAttribute('data-range')).toBe('0.5'); expect(host.firstElementChild?.getAttribute('data-base')).toBe('20');
		render('stock-invalid', NaN); expect(host.firstElementChild?.getAttribute('data-range')).toBe('1');
		render('stock-invalid', Infinity); expect(host.firstElementChild?.getAttribute('data-range')).toBe('1');
		render('stock-b:day', .2, 19, true); expect(host.firstElementChild?.getAttribute('data-base')).toBe('19');
	} finally { act(() => root.unmount()); vi.unstubAllGlobals(); }
});
