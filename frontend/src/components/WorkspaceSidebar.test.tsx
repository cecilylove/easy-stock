// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { WorkspaceSidebar, readSidebarExpanded, sidebarStorageKey, useSidebarPreference } from './WorkspaceSidebar';

let container: HTMLDivElement;
let root: Root;
const navigate = vi.fn();
const close = vi.fn();

beforeEach(() => {
	(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
	vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
	container = document.createElement('div');
	document.body.append(container);
	root = createRoot(container);
	window.localStorage.clear();
	vi.clearAllMocks();
});
afterEach(() => { act(() => root.unmount()); container.remove(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const renderSidebar = (mobileOpen = false, expanded = true) => act(() => root.render(<WorkspaceSidebar mode="themes" expanded={expanded} mobileOpen={mobileOpen} onNavigate={navigate} onToggle={() => {}} onMobileClose={close} onSettings={() => {}} />));

describe('workspace navigation', () => {
	it('groups workspaces and exposes the active page even with collapsed labels', () => {
		renderSidebar(false, false);
		expect(container.textContent).toContain('市场观察');
		expect(container.textContent).toContain('研究与决策');
		expect(container.textContent).toContain('复盘与学习');
		const current = container.querySelector('[aria-current="page"]');
		expect(current?.getAttribute('aria-label')).toBe('趋势题材');
		act(() => (container.querySelector('[aria-label="个股详情"]') as HTMLButtonElement).click());
		expect(navigate).toHaveBeenCalledWith('stock-detail');
	});

	it('does not render the collapse label in desktop compact mode', () => {
		renderSidebar(false, false);
		const toggle = container.querySelector('.sidebar-toggle');
		expect(toggle?.querySelector('span')).toBeNull();
		expect(toggle?.getAttribute('aria-label')).toBe('展开侧边栏');
		renderSidebar(false, true);
		expect(container.querySelector('.sidebar-toggle span')?.textContent).toBe('收起侧栏');
	});

	it('exposes an accessible mobile dialog and closes on Escape or backdrop click', () => {
		renderSidebar(true);
		expect(container.querySelector('[role="dialog"]')?.getAttribute('aria-modal')).toBe('true');
		expect(document.activeElement?.getAttribute('aria-label')).toBe('关闭功能导航');
		act(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
		expect(close).toHaveBeenCalledTimes(1);
		act(() => (container.querySelector('.mobile-nav-backdrop') as HTMLDivElement).click());
		expect(close).toHaveBeenCalledTimes(2);
	});

	it('defaults to expanded and remembers explicit collapse preferences', () => {
		expect(readSidebarExpanded()).toBe(true);
		function Preference() {
			const { expanded, toggle } = useSidebarPreference();
			return <button onClick={toggle} aria-expanded={expanded}>toggle</button>;
		}
		act(() => root.render(<Preference />));
		act(() => (container.querySelector('button') as HTMLButtonElement).click());
		expect(window.localStorage.getItem(sidebarStorageKey)).toBe('false');
		expect(readSidebarExpanded()).toBe(false);
		act(() => (container.querySelector('button') as HTMLButtonElement).click());
		expect(readSidebarExpanded()).toBe(true);
	});

	it('keeps navigation working when local storage cannot be read', () => {
		vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('unavailable'); });
		expect(readSidebarExpanded()).toBe(true);
	});
});
