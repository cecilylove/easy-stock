import { RefreshCw } from 'lucide-react';
import type { SourceHealth, SourceProbeResult } from '../lib/backend';
import { sourceFallbackPolicies } from '../lib/source-integrations';
import { SourceIntegrationCatalog } from './SourceIntegrationCatalog';

type Props = {
	id: string;
	sources: SourceHealth[];
	probes?: SourceProbeResult[];
	now?: number;
	checking?: boolean;
	checkError?: string;
	error?: string;
	loading?: boolean;
	readAt?: string;
	onRefresh: () => void;
};

export function SourceHealthPanel({ id, sources, probes = [], now = Date.now(), checking = false, checkError, error, loading = false, readAt, onRefresh }: Props) {
	return <section id={id} className="source-health-panel" aria-label="数据源详情与观测规则">
		<header><div><strong>来源与最近请求观测</strong><small>{loading ? '读取已有记录中…' : readAt ? `记录读取时间：${new Date(readAt).toLocaleString('zh-CN')}` : '尚未读取到观测记录'}</small></div><button type="button" className="source-check-button" disabled={checking} onClick={onRefresh}><RefreshCw size={14} className={checking ? 'spin' : ''} aria-hidden="true" />{checking ? '检测中…' : '刷新检测'}</button></header>
		<p className="source-health-rule">点击刷新检测会实际请求每个来源的代表接口；检测可用只说明该接口返回有效内容，不代表全部功能，也不保证后续请求成功。设置打开且页面可见时每 30 秒读取已有记录，不自动发起检测。检测与业务观测有效期 10 分钟，超期分别显示“检测已过期”“观测已过期”；服务重启后清空。缓存读取不会续期。未主动检测、无实际请求记录均不表示不可用。业务页面的刷新间隔不限制手动检测。</p>
		{checkError && <p className="source-check-error" role="alert">本次检测请求失败：{checkError}。检测结果未更新；{probes.length ? '保留上次检测记录，不代表当前可用性。' : '尚无检测结果，不能据此判定来源不可用。'}</p>}
		{error && <p role="alert">观测记录读取失败：{error}。{readAt ? '保留上次读取的记录，不代表当前可用性。' : '尚无可读取的观测，下面只展示功能目录。'}</p>}
		<SourceIntegrationCatalog sources={sources} probes={probes} now={now} checking={checking} probeRetained={Boolean(checkError || error)} retained={Boolean(error)} recordsRead={Boolean(readAt)} />
		<details className="source-fallback-details"><summary>各功能如何取数，失败后能否继续使用？</summary><p>按功能选择来源，不会把所有来源简单平均。最近成功不等于正在使用：业务模块实际使用的来源、缓存与抓取时间以该模块的来源说明为准。融合结果应展示多源列表，不能将某一家成功冒充整页可用。</p><div className="source-fallback-table"><table><thead><tr><th>功能</th><th>来源链路</th><th>失效边界</th></tr></thead><tbody>{sourceFallbackPolicies.map(policy => <tr key={policy.feature}><th scope="row">{policy.feature}</th><td>{policy.chain}</td><td>{policy.boundary}</td></tr>)}</tbody></table></div></details>
	</section>;
}
