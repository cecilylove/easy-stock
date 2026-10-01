// @vitest-environment jsdom
import { act, useRef, type RefObject } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useModalDialog } from './use-modal-dialog';

let root: Root;
let container: HTMLDivElement;
let trigger: HTMLButtonElement;
const onClose = vi.fn();
function Dialog({ open, close = onClose, fallbackFocusRef }: { open: boolean; close?: () => void; fallbackFocusRef?: RefObject<HTMLElement | null> }) {
	const ref = useRef<HTMLDivElement>(null);
	useModalDialog(open, ref, close, fallbackFocusRef);
	return open ? <div ref={ref} role="dialog" tabIndex={-1}><button id="first">first</button><button disabled>disabled</button><button id="last">last</button></div> : null;
}
beforeEach(() => {
	(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
	container = document.createElement('div');
	trigger = document.createElement('button');
	document.body.append(trigger, container);
	trigger.focus();
	root = createRoot(container);
	vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{ width: 1, height: 1 }] as unknown as DOMRectList);
	vi.clearAllMocks();
});
afterEach(() => { act(() => root.unmount()); container.remove(); trigger.remove(); document.body.style.overflow = ''; vi.restoreAllMocks(); });
const key = (key: string, shiftKey = false) => act(() => document.dispatchEvent(new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })));

describe('modal keyboard and scroll lifecycle', () => {
	it('locks background scroll, traps Tab in both directions and restores focus and scroll on close', () => {
		document.body.style.overflow = 'auto';
		act(() => root.render(<Dialog open />));
		expect(document.body.style.overflow).toBe('hidden');
		expect(document.activeElement?.id).toBe('first');
		key('Tab', true);
		expect(document.activeElement?.id).toBe('last');
		key('Tab');
		expect(document.activeElement?.id).toBe('first');
		act(() => root.render(<Dialog open={false} />));
		expect(document.activeElement).toBe(trigger);
		expect(document.body.style.overflow).toBe('auto');
	});

	it('redirects escaped focus and uses the latest close callback without stealing focus on rerender', () => {
		act(() => root.render(<Dialog open />));
		trigger.focus();
		expect(document.activeElement?.id).toBe('first');
		(container.querySelector('#last') as HTMLButtonElement).focus();
		const latest = vi.fn();
		act(() => root.render(<Dialog open close={latest} />));
		expect(document.activeElement?.id).toBe('last');
		key('Escape');
		expect(latest).toHaveBeenCalledTimes(1);
		expect(onClose).not.toHaveBeenCalled();
	});

	it('restores a fallback focus target when the original opener disappears', () => {
		const fallback = document.createElement('button');
		document.body.append(fallback);
		const fallbackFocusRef = { current: fallback };
		act(() => root.render(<Dialog open fallbackFocusRef={fallbackFocusRef} />));
		trigger.remove();
		act(() => root.render(<Dialog open={false} fallbackFocusRef={fallbackFocusRef} />));
		expect(document.activeElement).toBe(fallback);
		fallback.remove();
	});

	it('keeps focus on the dialog if all controls become unavailable', () => {
		act(() => root.render(<Dialog open />));
		container.querySelectorAll('button').forEach(button => { button.disabled = true; });
		key('Tab');
		expect(document.activeElement?.getAttribute('role')).toBe('dialog');
	});
});
