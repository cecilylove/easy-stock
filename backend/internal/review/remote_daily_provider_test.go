package review

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"testing"
	"time"
)

type archiveProviderFixture struct {
	author       RemoteDailyAuthor
	article      RemoteDailyArticle
	authorsCalls int
	articleCalls int
	seenETag     string
}

func (f *archiveProviderFixture) FetchAuthors(_ context.Context, q contracts.ArchiveAuthorsRequest) (contracts.ArchiveAuthorsResult, error) {
	f.authorsCalls++
	f.seenETag = q.ETag
	if f.authorsCalls > 1 {
		return contracts.ArchiveAuthorsResult{NotModified: true}, nil
	}
	return contracts.ArchiveAuthorsResult{Found: true, ETag: "fixture-etag", Manifest: contracts.ArchiveManifest{SchemaVersion: 1, Authors: []contracts.ArchiveAuthor{f.author}}}, nil
}
func (f *archiveProviderFixture) FetchArchiveArticle(_ context.Context, q contracts.ArchiveArticleRequest) (contracts.ArchiveArticleResult, error) {
	f.articleCalls++
	return contracts.ArchiveArticleResult{Found: true, Article: f.article, ObjectURL: "https://archive.test/" + q.AuthorID + "/" + q.TradeDate + ".json"}, nil
}
func TestRemoteDailyInjectedProviderRetainsETagAndLocalDedup(t *testing.T) {
	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	author := RemoteDailyAuthor{ID: "a", Name: "作者", Enabled: true}
	provider := &archiveProviderFixture{author: author, article: testRemoteArticle(now, author)}
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{Provider: provider, Now: func() time.Time { return now }})
	first, err := syncer.SyncToday(context.Background())
	if err != nil || first.SyncedAuthors != 1 {
		t.Fatalf("status=%+v err=%v", first, err)
	}
	second, err := syncer.SyncToday(context.Background())
	if err != nil || second.LocalAuthors != 1 || provider.articleCalls != 1 || provider.seenETag != "fixture-etag" {
		t.Fatalf("dedup/etag changed: status=%+v calls=%d etag=%s err=%v", second, provider.articleCalls, provider.seenETag, err)
	}
	post, err := store.GetPost(context.Background(), provider.article.ID)
	if err != nil || post.OriginalURL != "https://archive.test/a/2026-08-11.json" || post.Source != "official" || !post.FetchedAt.Equal(now) {
		t.Fatalf("archive projection=%+v err=%v", post, err)
	}
}
func TestRemoteDailyInjectedProviderCannotBypassHashValidation(t *testing.T) {
	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	author := RemoteDailyAuthor{ID: "a", Name: "作者", Enabled: true}
	provider := &archiveProviderFixture{author: author, article: testRemoteArticle(now, author)}
	provider.article.ContentSHA256 = "bad"
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{Provider: provider, Now: func() time.Time { return now }})
	status, err := syncer.SyncToday(context.Background())
	if err != nil || status.PendingAuthors != 1 || len(status.Authors) != 1 || status.Authors[0].Status != "error" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	_, total, err := store.ListPosts(context.Background(), Query{Limit: 20})
	if err != nil || total != 0 {
		t.Fatalf("invalid article persisted: total=%d err=%v", total, err)
	}
}
