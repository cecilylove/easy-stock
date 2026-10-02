package assembly

import (
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/providers/article"
	"easy-stock/backend/internal/providers/githubknowledge"
	"easy-stock/backend/internal/providers/reviewarchive"
	"easy-stock/backend/internal/providers/reviewautomation"
	"net/http"
)

// Content sources have distinct acquisition modes and no public-market probes.
// Enabled describes the integration route, not the current browser login state.
func Content(client *http.Client, wechatAPIURL, archiveURL string) *registry.Registry {
	articles := article.NewClient(client, wechatAPIURL)
	collections := reviewautomation.NewClient(client, nil)
	entries := []registry.Entry{
		{Descriptor: registry.Descriptor{ID: "xueqiu", ArticleHosts: []string{"xueqiu.com", "*.xueqiu.com"}, Name: "雪球", Mode: "browser", Kinds: []string{"information"}, Usage: "文章链接导入、经授权浏览器采集作者内容", Configuration: "需要有效登录态时使用浏览器桥接；可用性按具体采集结果判断。", Capabilities: []string{"article", "browser-subscription"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Article: articles, BrowserCollection: collections, AuthorLinks: collections}},
		{Descriptor: registry.Descriptor{ID: "taoguba", ArticleHosts: []string{"taoguba.com.cn", "*.taoguba.com.cn", "tgb.cn", "*.tgb.cn"}, Name: "淘股吧", Mode: "browser", Kinds: []string{"information"}, Usage: "文章链接导入、经授权浏览器采集作者内容", Configuration: "登录态与订阅由复盘业务管理；公开页面可读性不代表授权浏览器状态。", Capabilities: []string{"article", "browser-subscription"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Article: articles, BrowserCollection: collections, AuthorLinks: collections}},
		{Descriptor: registry.Descriptor{ID: "wechat", ArticleHosts: []string{"mp.weixin.qq.com"}, Name: "微信公众号", Mode: "public", Kinds: []string{"information"}, Usage: "已知文章链接导入", Configuration: "已知链接可经侧车或公开页面解析；未实现自动公众号订阅。", Capabilities: []string{"article"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Article: articles, AuthorizedArticle: collections}},
		{Descriptor: registry.Descriptor{ID: "official", Name: "预整理每日复盘", Mode: "archive", Kinds: []string{"information"}, Usage: "远程作者清单与已发布复盘正文", Configuration: "远程预整理归档；不是原平台实时抓取，正文先校验身份与哈希。", Capabilities: []string{"review-archive"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Archive: reviewarchive.NewClient(archiveURL, client)}},
		{Descriptor: registry.Descriptor{ID: "githubknowledge", Name: "GitHub 知识资料", Mode: "archive", Kinds: []string{"information"}, Usage: "心法目录与 Markdown 知识内容", Configuration: "知识库管理本地缓存、内置资料和旧缓存回退；历史知识不视为实时市场事实。", Capabilities: []string{"knowledge-tree", "knowledge-document"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Knowledge: githubknowledge.NewClient(githubknowledge.Config{})}},
	}
	sources, err := registry.New(entries...)
	if err != nil {
		panic(err)
	}
	return sources
}
