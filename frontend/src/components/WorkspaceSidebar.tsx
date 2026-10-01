import { BarChart3, BookMarked, BookOpen, Bot, BrainCircuit, ChartCandlestick, Flame, LayoutDashboard, PanelLeftClose, PanelLeftOpen, Settings, WalletCards, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useModalDialog } from '../lib/use-modal-dialog';

export type WorkspaceMode = 'themes' | 'limit-up' | 'mastery' | 'reviews' | 'stock-detail' | 'stock-ai' | 'portfolio-inspection' | 'ai' | 'market' | 'token-usage';
export const sidebarStorageKey = 'easy-stock.sidebar-expanded.v1';

export function readSidebarExpanded() {
	try { return window.localStorage.getItem(sidebarStorageKey) !== 'false'; }
	catch { return true; }
}

export function useSidebarPreference() {
	const [expanded, setExpanded] = useState(readSidebarExpanded);
	useEffect(() => {
		try { window.localStorage.setItem(sidebarStorageKey, String(expanded)); } catch { /* Keep navigation usable without storage. */ }
	}, [expanded]);
	const toggle = () => setExpanded(current => !current);
	return { expanded, toggle };
}

const navigationGroups = [
	{ label: '市场观察', items: [
		{ mode: 'market', label: '行情总览', icon: BarChart3 },
		{ mode: 'themes', label: '趋势题材', icon: LayoutDashboard },
		{ mode: 'limit-up', label: '短线连板', icon: Flame },
	] },
	{ label: '研究与决策', items: [
		{ mode: 'stock-detail', label: '个股详情', icon: ChartCandlestick },
		{ mode: 'stock-ai', label: '个股分析', icon: BrainCircuit },
		{ mode: 'portfolio-inspection', label: '持仓AI巡检', icon: WalletCards },
		{ mode: 'ai', label: 'AI 对话', icon: Bot },
	] },
	{ label: '复盘与学习', items: [
		{ mode: 'reviews', label: '大V复盘日记', icon: BookOpen },
		{ mode: 'mastery', label: '游资心法', icon: BookMarked },
	] },
] as const;

type Props = {
	mode: WorkspaceMode;
	expanded: boolean;
	mobileOpen: boolean;
	blocked?: boolean;
	onNavigate: (mode: WorkspaceMode) => void;
	onToggle: () => void;
	onMobileClose: () => void;
	onSettings: () => void;
};

export function WorkspaceSidebar({ mode, expanded, mobileOpen, blocked, onNavigate, onToggle, onMobileClose, onSettings }: Props) {
	const sidebarRef = useRef<HTMLElement>(null);
	useModalDialog(mobileOpen, sidebarRef, onMobileClose);
	useEffect(() => {
		const query = window.matchMedia('(max-width: 760px)');
		const closeOnDesktop = () => { if (!query.matches) onMobileClose(); };
		query.addEventListener('change', closeOnDesktop);
		return () => query.removeEventListener('change', closeOnDesktop);
	}, [onMobileClose]);

	return <>
		{mobileOpen && <div className="mobile-nav-backdrop" aria-hidden="true" onClick={onMobileClose} />}
		<aside id="workspace-navigation" ref={sidebarRef} className="app-sidebar" inert={blocked} aria-label="功能导航" role={mobileOpen ? 'dialog' : undefined} aria-modal={mobileOpen || undefined} tabIndex={-1}>
			<div className="sidebar-brand">
				<div className="sidebar-logo"><img src={`${import.meta.env.BASE_URL}easy-stock-mark.svg`} alt="" /></div>
				<div className="sidebar-brand-copy"><strong>easy-stock</strong><span>AI STOCK LAB</span></div>
				<button type="button" className="mobile-nav-close" onClick={onMobileClose} aria-label="关闭功能导航" data-dialog-autofocus><X size={19} aria-hidden="true" /></button>
			</div>
			<nav aria-label="工作台">
				{navigationGroups.map(group => <div className="sidebar-nav-group" key={group.label}>
					<span className="sidebar-group-label">{group.label}</span>
					{group.items.map(({ mode: target, label, icon: Icon }) => <button type="button" key={target} className={mode === target ? 'active' : ''} aria-current={mode === target ? 'page' : undefined} aria-label={label} title={label} onClick={() => onNavigate(target)}><Icon size={18} aria-hidden="true" /><span>{label}</span></button>)}
				</div>)}
			</nav>
			<div className="sidebar-bottom-actions">
				<button type="button" className={`sidebar-settings ${mode === 'token-usage' ? 'active' : ''}`} onClick={() => onNavigate('token-usage')} aria-current={mode === 'token-usage' ? 'page' : undefined} aria-label="打开Token统计" title="Token统计"><BarChart3 size={17} aria-hidden="true" /><span>Token统计</span></button>
				<button type="button" className="sidebar-settings" onClick={onSettings} aria-label="打开系统设置" title="系统设置"><Settings size={17} aria-hidden="true" /><span>系统设置</span></button>
				<button type="button" className="sidebar-toggle" onClick={onToggle} aria-expanded={expanded} aria-controls="workspace-navigation" aria-label={expanded ? '收起侧边栏' : '展开侧边栏'} title={expanded ? '收起侧边栏' : '展开侧边栏'}>{expanded ? <PanelLeftClose size={17} aria-hidden="true" /> : <PanelLeftOpen size={17} aria-hidden="true" />}{(expanded || mobileOpen) && <span>收起侧栏</span>}</button>
			</div>
		</aside>
	</>;
}
