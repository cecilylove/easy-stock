package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"net/url"
	"strings"
)

// Articles selects by explicit platform identity. Authorization headers remain
// caller-owned and are passed only to the selected adapter.
type Articles struct {
	routes []ArticleRoute
}
type ArticleRoute struct {
	SourceID string
	Hosts    []string
	Provider contracts.ArticleSource
}

func NewArticleRoutes(routes ...ArticleRoute) *Articles {
	s := &Articles{}
	for _, route := range routes {
		route.Hosts = append([]string(nil), route.Hosts...)
		s.routes = append(s.routes, route)
	}
	return s
}

func NewArticles(sources map[string]contracts.ArticleSource) *Articles {
	return NewArticleRoutes(ArticleRoute{SourceID: "xueqiu", Hosts: []string{"xueqiu.com", "*.xueqiu.com"}, Provider: sources["xueqiu"]}, ArticleRoute{SourceID: "taoguba", Hosts: []string{"taoguba.com.cn", "*.taoguba.com.cn", "tgb.cn", "*.tgb.cn"}, Provider: sources["taoguba"]}, ArticleRoute{SourceID: "wechat", Hosts: []string{"mp.weixin.qq.com"}, Provider: sources["wechat"]})
}
func (s *Articles) FetchArticle(ctx context.Context, request contracts.ArticleRequest) (foundation.Article, error) {
	request.URL = strings.TrimSpace(request.URL)
	u, err := url.Parse(request.URL)
	if err != nil {
		return foundation.Article{}, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return foundation.Article{}, &contracts.Error{Kind: contracts.InvalidResponse, Capability: "article-url"}
	}
	id := ""
	host := strings.ToLower(u.Hostname())
	var provider contracts.ArticleSource
	for _, route := range s.routes {
		for _, scope := range route.Hosts {
			scope = strings.ToLower(scope)
			matched := host == scope || (strings.HasPrefix(scope, "*.") && strings.HasSuffix(host, scope[1:]))
			if !matched {
				continue
			}
			if provider != nil && id != route.SourceID {
				return foundation.Article{}, &contracts.Error{Kind: contracts.Unsupported, Capability: "ambiguous-article-route"}
			}
			id, provider = route.SourceID, route.Provider
			break
		}
	}
	if provider == nil {
		return foundation.Article{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: id, Capability: "article"}
	}
	return provider.FetchArticle(ctx, request)
}
