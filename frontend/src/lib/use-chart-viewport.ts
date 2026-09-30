import { useLayoutEffect, useRef, useState } from 'react';

// Match SVG coordinates to the scroll container so a capped height does not
// letterbox a fixed 960px viewBox on wide screens. Narrow charts scroll locally.
export function useChartViewport(enabled = true, minWidth = 720) {
	const containerRef = useRef<HTMLDivElement | null>(null);
	const [containerWidth, setContainerWidth] = useState(960);
	useLayoutEffect(() => {
		const container = containerRef.current;
		if (!enabled || !container) return;
		const resize = (size: number) => {
			if (size > 0) setContainerWidth(Math.floor(size));
		};
		resize(container.clientWidth);
		const observer = new ResizeObserver(([entry]) => resize(entry.contentRect.width));
		observer.observe(container);
		return () => observer.disconnect();
	}, [enabled, minWidth]);
	return { containerRef, width: Math.max(minWidth, containerWidth), scrollable: containerWidth < minWidth };
}
