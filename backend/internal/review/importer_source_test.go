package review

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

type neutralArticleFixture struct {
	item    foundation.Article
	request contracts.ArticleRequest
}

func (f *neutralArticleFixture) FetchArticle(_ context.Context, r contracts.ArticleRequest) (foundation.Article, error) {
	f.request = r
	return f.item, nil
}

func TestImporterInjectedArticlePreservesArchiveIdentityAndTimes(t *testing.T) {
	published := time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)
	fetched := published.Add(time.Hour)
	fixture := &neutralArticleFixture{item: foundation.Article{Source: "taoguba", OriginalURL: "https://www.tgb.cn/a/123", AuthorName: "作者", Title: "复盘", Digest: "原文摘要", ContentText: "原文正文", PublishedAt: published, FetchedAt: fetched}}
	importer := NewImporterWithSource(fixture)
	post, err := importer.ImportURL(context.Background(), fixture.item.OriginalURL)
	if err != nil {
		t.Fatal(err)
	}
	old := newPost(fixture.item.Source, fixture.item.OriginalURL, fixture.item.AuthorName, fixture.item.Title, fixture.item.ContentText, "", published)
	if post.ID != old.ID || post.ExternalID != old.ExternalID || post.AuthorID != old.AuthorID || post.Digest != fixture.item.Digest || !post.PublishedAt.Equal(published) || !post.FetchedAt.Equal(fetched) {
		t.Fatalf("projection changed archive identity: %+v", post)
	}
	if fixture.request.URL != post.OriginalURL || fixture.request.Mode != contracts.ArticleAutomatic {
		t.Fatalf("request=%+v", fixture.request)
	}
	if post.RelatedStocks == nil || post.RelatedThemes == nil {
		t.Fatal("archive compatibility arrays lost")
	}
}

func TestImporterKeepsBodyDigestWhenAdapterDoesNotSupplyOne(t *testing.T) {
	item := foundation.Article{Source: "wechat", OriginalURL: "https://mp.weixin.qq.com/s/example", AuthorName: "作者", ContentText: "这是一篇需要展示在归档列表中的原文正文。", PublishedAt: time.Now(), FetchedAt: time.Now()}
	for _, digest := range []string{"", "  "} {
		item.Digest = digest
		post, err := NewImporterWithSource(&neutralArticleFixture{item: item}).ImportURL(context.Background(), item.OriginalURL)
		if err != nil || post.Digest != item.ContentText {
			t.Fatalf("body digest was lost: %+v err=%v", post, err)
		}
	}
}
