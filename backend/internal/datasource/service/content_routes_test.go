package service_test

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
	"testing"
)

type articleRecorder struct {
	calls   int
	url     string
	headers map[string][]string
}

func (a *articleRecorder) FetchArticle(_ context.Context, r contracts.ArticleRequest) (foundation.Article, error) {
	a.calls++
	a.url = r.URL
	a.headers = r.Headers
	return foundation.Article{Source: "replacement", OriginalURL: r.URL}, nil
}
func TestArticleRoutesSupportNewPlatformsAliasesAndSafeRemoval(t *testing.T) {
	replacement := &articleRecorder{}
	legacy := &articleRecorder{}
	access := service.NewArticleRoutes(service.ArticleRoute{SourceID: "replacement", Hosts: []string{"research.example", "*.research.example"}, Provider: replacement}, service.ArticleRoute{SourceID: "taoguba", Hosts: []string{"tgb.cn", "*.tgb.cn"}, Provider: legacy})
	if _, err := access.FetchArticle(context.Background(), contracts.ArticleRequest{URL: " https://author.research.example/a ", Headers: map[string][]string{"Cookie": {"authorized-context"}}}); err != nil || replacement.calls != 1 || replacement.url != "https://author.research.example/a" || replacement.headers["Cookie"][0] != "authorized-context" {
		t.Fatalf("new platform not routed err=%v calls=%d", err, replacement.calls)
	}
	if _, err := access.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://www.tgb.cn/a/abc"}); err != nil || legacy.calls != 1 {
		t.Fatal("legacy alias lost", err)
	}
	if _, err := access.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://research.example.evil/a"}); contracts.Kind(err) != contracts.Unsupported || replacement.calls != 1 {
		t.Fatal("unregistered host requested", err)
	}
	if _, err := access.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "file://research.example/a"}); contracts.Kind(err) != contracts.InvalidResponse || replacement.calls != 1 {
		t.Fatal("unsupported URL scheme contacted supplier", err)
	}
	removed := service.NewArticleRoutes()
	if _, err := removed.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://research.example/a"}); contracts.Kind(err) != contracts.Unsupported {
		t.Fatal("removed platform restored", err)
	}
}
func TestRemovedContentSourcesNeverConstructImplicitDefaults(t *testing.T) {
	empty, _ := registry.New()
	collections := service.NewCollections(empty)
	if _, err := collections.CollectBrowser(context.Background(), contracts.BrowserCollectionRequest{SourceID: "xueqiu"}); contracts.Kind(err) != contracts.Unsupported {
		t.Fatal(err)
	}
	if _, err := collections.DiscoverAuthorLinks(context.Background(), contracts.AuthorLinksRequest{SourceID: "wechat"}); contracts.Kind(err) != contracts.Unsupported {
		t.Fatal(err)
	}
	if _, err := service.NewArchive("removed", nil).FetchAuthors(context.Background(), contracts.ArchiveAuthorsRequest{}); contracts.Kind(err) != contracts.Unsupported {
		t.Fatal(err)
	}
	if _, err := service.NewKnowledge("removed", nil).Tree(context.Background()); contracts.Kind(err) != contracts.Unsupported {
		t.Fatal(err)
	}
}
