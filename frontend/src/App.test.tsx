// @vitest-environment jsdom
import { renderToStaticMarkup } from 'react-dom/server';
import { beforeEach, describe, expect, it } from 'vitest';
import { App } from './App';

beforeEach(() => window.localStorage.clear());

describe('workspace shell', () => {
	it.each(['themes', 'market', 'limit-up', 'stock-detail', 'stock-ai', 'portfolio-inspection', 'ai', 'reviews', 'mastery', 'token-usage'])('renders the %s workspace with navigation and a keyboard skip target', mode => {
		window.location.hash = mode;
		const html = renderToStaticMarkup(<App />);
		expect(html).toContain('id="workspace-navigation"');
		expect(html).toContain('id="workspace-content"');
		expect(html).toContain(`workspace-${mode}`);
		expect(html).toContain('跳转到工作区');
		expect(html).toContain('刷新当前工作台');
		expect(html).not.toContain('mobile-nav-open');
		if (mode === 'stock-ai') {
			expect(html).toContain('aria-label="工作台模式"');
			expect(html).toContain('aria-pressed="true"');
		} else {
			expect(html).not.toContain('aria-label="工作台模式"');
		}
	});
});
