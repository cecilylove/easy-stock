// @vitest-environment jsdom
import { renderToStaticMarkup } from 'react-dom/server';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { App, nodeMembershipIdentity, nodeMembershipLabel } from './App';
import type { SectorMapNode } from './lib/backend';
import type { useThemeConstituents } from './lib/use-theme-constituents';

const constituentsFixture = vi.hoisted(() => ({ current: null as ReturnType<typeof useThemeConstituents> | null }));
vi.mock('./lib/use-theme-constituents', async (importOriginal) => {
	const original = await importOriginal<typeof import('./lib/use-theme-constituents')>();
	return { ...original, useThemeConstituents: (...args: Parameters<typeof useThemeConstituents>) => constituentsFixture.current || original.useThemeConstituents(...args) };
});
beforeEach(() => { window.localStorage.clear(); constituentsFixture.current = null; });

describe('theme node membership contracts', () => {
	const ref = { provider: 'tencent', native_code: 'hy01', dimension: 'industry', name: '行业' };
	const node = (kind: 'native' | 'candidate' | 'leader', complete: boolean): SectorMapNode => ({ id: 'x', name: '行业', change_percent: 0, main_net_inflow: 0, stocks: [], match_status: 'matched', board_ref: ref, member_set: { kind, complete, returned: 20, total: 50, has_more: true, board_ref: ref } });
	it.each(['candidate', 'leader'] as const)('never calls %s samples complete even if a response incorrectly says complete', kind => {
		const label = nodeMembershipLabel(node(kind, true));
		expect(label).toContain(kind === 'candidate' ? '关联候选' : '已知领涨');
		expect(label).toContain('不完整');
		expect(label).toContain('返回 20只 / 总数 50');
		expect(label).toContain('还有更多');
	});
	it('shows native completeness, returned/total/paging and source identity', () => {
		expect(nodeMembershipLabel(node('native', true))).toContain('原生成分 · 返回 20只 / 总数 50 · 完整 · 还有更多');
		expect(nodeMembershipLabel(node('native', false))).toContain('不完整');
		expect(nodeMembershipIdentity(node('native', true))).toContain('hy01 · industry');
	});
	it('does not call cross-provider or unidentified native member sets complete', () => {
		const mixed = node('native', true); mixed.member_set!.board_ref = { ...ref, provider: 'eastmoney' };
		expect(nodeMembershipLabel(mixed)).toContain('不完整');
		const unidentified = node('native', true); delete unidentified.board_ref;
		expect(nodeMembershipLabel(unidentified)).toContain('不完整');
	});
	it('renders candidate response counts and paging without claiming full-pool membership', () => {
		const candidate = node('candidate', true);
		const data = { complete: true, coverage: 'full', map: { theme: 'test', name: '样本题材', tabs: [], groups: [{ id: 'g', name: '组', nodes: [candidate] }], meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false } }, order: [], snapshot_id: 'test', sort: 'rank_score', pagination: { page: 1, page_size: 20, total: 50, total_pages: 3, has_more: true } } as NonNullable<ReturnType<typeof useThemeConstituents>['data']>;
		constituentsFixture.current = { key: 'test', data, fetching: false, error: '', status: 'ready' };
		window.location.hash = 'themes';
		const html = renderToStaticMarkup(<App />);
		expect(html).toContain('关联候选 · 返回 20只 / 总数 50 · 不完整 · 还有更多');
		expect(html).toContain('本页返回 0 只 · 当前范围共 50 只 · 还有下一页');
		expect(html).toContain('来源优先顺序');
		expect(html).not.toContain('全池领导力');
		expect(html).toContain('hy01 · industry');
	});
	it('gives response-level false precedence over a complete native page', () => {
		const native = node('native', true);
		const data = { complete: true, membership_complete: false, membership_scope: 'native_partial', map: { theme: 'test', name: '样本题材', tabs: [], groups: [{ id: 'g', name: '组', nodes: [native] }], meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false } }, order: [], snapshot_id: 'test', sort: 'rank_score', pagination: { page: 1, page_size: 20, total: 50, total_pages: 3, has_more: true } } as NonNullable<ReturnType<typeof useThemeConstituents>['data']>;
		constituentsFixture.current = { key: 'test', data, fetching: false, error: '', status: 'ready' };
		window.location.hash = 'themes';
		const html = renderToStaticMarkup(<App />);
		expect(html).toContain('部分原生成分');
		expect(html).not.toContain('全池领导力');
	});
	it('does not infer legacy node completeness from stock length or candidate count', () => {
		const legacy = node('native', true); delete legacy.member_set; legacy.candidate_count = 500;
		expect(nodeMembershipLabel(legacy)).toBe('已加载 0只 · 完整性未确认');
	});
});

describe('workspace shell', () => {
	it.each(['themes', 'market'])('links %s source details to settings without duplicating global observations', mode => {
		window.location.hash = mode;
		const html = renderToStaticMarkup(<App />);
		expect(html).toContain('数据源设置'); expect(html).not.toContain('数据源最近观测'); expect(html).not.toContain('source-health-panel');
		expect(html).toContain('东方财富');
	});
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
