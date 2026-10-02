package reviewarchive

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type archiveTransport func(*http.Request) (*http.Response, error)

func (f archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func archiveResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestArchiveAuthorsAcquisitionPreservesETagProtocol(t *testing.T) {
	calls := 0
	client := NewClient("https://archive.test/daily/", &http.Client{Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/daily/authors.json" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("request=%+v", r)
		}
		if calls == 1 {
			response := archiveResponse(r, http.StatusOK, `{"schema_version":1,"authors":[{"id":"a","name":"作者","platform":"wechat","enabled":true}]}`)
			response.Header.Set("ETag", `"version-1"`)
			return response, nil
		}
		if r.Header.Get("If-None-Match") != `"version-1"` {
			t.Fatal("conditional request lost")
		}
		return archiveResponse(r, http.StatusNotModified, ""), nil
	})})
	first, err := client.FetchAuthors(context.Background(), contracts.ArchiveAuthorsRequest{})
	if err != nil || !first.Found || first.ETag != `"version-1"` || len(first.Manifest.Authors) != 1 {
		t.Fatalf("result=%+v err=%v", first, err)
	}
	second, err := client.FetchAuthors(context.Background(), contracts.ArchiveAuthorsRequest{ETag: first.ETag})
	if err != nil || !second.NotModified || calls != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", second, err, calls)
	}
}

func TestArchiveArticleAcquisitionDistinguishesPendingFromFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		client := NewClient("https://archive.test", &http.Client{Transport: archiveTransport(func(r *http.Request) (*http.Response, error) { return archiveResponse(r, status, "pending"), nil })})
		result, err := client.FetchArchiveArticle(context.Background(), contracts.ArchiveArticleRequest{AuthorID: "author/a", TradeDate: "2026-08-11"})
		if status == http.StatusNotFound {
			if err != nil || result.Found || result.ObjectURL != "https://archive.test/author%2Fa/2026-08-11.json" {
				t.Fatalf("pending=%+v err=%v", result, err)
			}
		} else if err == nil {
			t.Fatal("upstream failure became pending")
		}
	}
}

func TestArchiveAcquisitionLeavesPublicationValidationToReview(t *testing.T) {
	item := contracts.ArchivedArticle{SchemaVersion: 99, AuthorID: "a", ContentText: "正文", ContentSHA256: "invalid"}
	body, _ := json.Marshal(item)
	client := NewClient("https://archive.test", &http.Client{Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
		return archiveResponse(r, http.StatusOK, string(body)), nil
	})})
	result, err := client.FetchArchiveArticle(context.Background(), contracts.ArchiveArticleRequest{AuthorID: "a", TradeDate: "2026-08-11"})
	if err != nil || !result.Found || result.Article.SchemaVersion != 99 || result.Article.ContentSHA256 != "invalid" {
		t.Fatalf("adapter imposed business validation: %+v err=%v", result, err)
	}
}
func TestArchiveAcquisitionRejectsUnknownFieldsAndOversizedBody(t *testing.T) {
	for _, body := range []string{`{"schema_version":1,"unknown":true}`, strings.Repeat(" ", maxBodyBytes+1)} {
		client := NewClient("https://archive.test", &http.Client{Transport: archiveTransport(func(r *http.Request) (*http.Response, error) { return archiveResponse(r, http.StatusOK, body), nil })})
		if _, err := client.FetchAuthors(context.Background(), contracts.ArchiveAuthorsRequest{}); err == nil {
			t.Fatal("invalid author payload accepted")
		}
		if _, err := client.FetchArchiveArticle(context.Background(), contracts.ArchiveArticleRequest{AuthorID: "a", TradeDate: "2026-08-11"}); err == nil {
			t.Fatal("invalid article payload accepted")
		}
	}
}
