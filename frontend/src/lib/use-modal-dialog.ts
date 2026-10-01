import { useEffect, useRef, type RefObject } from 'react';

const focusableSelector = 'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])';

/** Keep keyboard focus and scrolling inside an open overlay; restore both on close. */
export function useModalDialog<T extends HTMLElement>(open: boolean, dialogRef: RefObject<T | null>, onClose: () => void, fallbackFocusRef?: RefObject<HTMLElement | null>) {
	const onCloseRef = useRef(onClose);
	onCloseRef.current = onClose;

	useEffect(() => {
		const dialog = dialogRef.current;
		if (!open || !dialog) return;
		const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
		const previousOverflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		const focusableElements = () => Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector))
			.filter(element => !element.closest('[hidden], [inert]') && element.getClientRects().length > 0);
		(dialog.querySelector<HTMLElement>('[data-dialog-autofocus]') || focusableElements()[0] || dialog).focus({ preventScroll: true });

		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === 'Escape') {
				event.preventDefault();
				event.stopPropagation();
				onCloseRef.current();
				return;
			}
			if (event.key !== 'Tab') return;
			const elements = focusableElements();
			const first = elements[0];
			const last = elements[elements.length - 1];
			if (!first) {
				event.preventDefault();
				dialog.focus();
			} else if (!dialog.contains(document.activeElement) || document.activeElement === dialog) {
				event.preventDefault();
				(event.shiftKey ? last : first).focus();
			} else if (event.shiftKey && document.activeElement === first) {
				event.preventDefault();
				last.focus();
			} else if (!event.shiftKey && document.activeElement === last) {
				event.preventDefault();
				first.focus();
			}
		};
		const onFocusIn = (event: FocusEvent) => {
			if (event.target instanceof Node && !dialog.contains(event.target)) {
				(focusableElements()[0] || dialog).focus({ preventScroll: true });
			}
		};
		document.addEventListener('keydown', onKeyDown);
		document.addEventListener('focusin', onFocusIn);
		return () => {
			document.removeEventListener('keydown', onKeyDown);
			document.removeEventListener('focusin', onFocusIn);
			document.body.style.overflow = previousOverflow;
			if (previouslyFocused?.isConnected) previouslyFocused.focus({ preventScroll: true });
			// A mobile navigation opener may have disappeared while opening settings.
			if (document.activeElement === document.body) fallbackFocusRef?.current?.focus({ preventScroll: true });
		};
	}, [open, dialogRef, fallbackFocusRef]);
}
