package article

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"easy-stock/backend/internal/datasource/contracts"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

const articleHead = `<head><meta property="og:title" content="原文"><meta name="author" content="作者"><meta property="article:published_time" content="2026-08-04T15:30:00+08:00"></head>`

func TestTypedArticlePublicModePreservesAuthorizationAndSkipsSidecar(t *testing.T) {
	requests := 0
	client := NewClient(&http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet || r.URL.Hostname() != "mp.weixin.qq.com" {
			t.Fatalf("unexpected sidecar request: %s %s", r.Method, r.URL)
		}
		if requests == 1 && r.Header.Get("Cookie") != "authorized-session" {
			t.Fatal("authorized headers lost")
		}
		return fixtureResponse(r, "<html>"+articleHead+"<body><article>来源正文</article></body></html>"), nil
	})}, "http://127.0.0.1:30000")
	item, err := client.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://mp.weixin.qq.com/s/fixture", Mode: contracts.ArticlePublicPage, Headers: map[string][]string{"Cookie": {"authorized-session"}}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || item.Source != "wechat" || item.ContentText != "来源正文" || item.ContentScope != "body" || item.TimeStatus != "published" || item.FetchedAt.IsZero() {
		t.Fatalf("article=%+v calls=%d", item, requests)
	}
	encoded, _ := json.Marshal(item)
	if strings.Contains(string(encoded), "authorized-session") {
		t.Fatal("credential leaked into neutral article")
	}
	// An explicit public-page request with empty headers also bypasses sidecar,
	// preserving the old ImportURLWithHeaders entry point.
	_, err = client.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://mp.weixin.qq.com/s/fixture", Mode: contracts.ArticlePublicPage})
	if err != nil || requests != 2 {
		t.Fatalf("request count=%d err=%v", requests, err)
	}
}

func TestTypedArticleRetainedContentHashAndScope(t *testing.T) {
	for _, test := range []struct {
		name, body, scope string
		count             int
	}{
		{"truncated", "<article>" + strings.Repeat("文", 120001) + "</article>", "truncated_body", 120000},
		{"page", "<div>页面文本</div>", "page_text", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				return fixtureResponse(r, "<html>"+articleHead+"<body>"+test.body+"</body></html>"), nil
			})}, "")
			item, err := client.FetchArticle(context.Background(), contracts.ArticleRequest{URL: "https://www.tgb.cn/a/test"})
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(item.ContentText))
			if item.ContentSHA256 != hex.EncodeToString(digest[:]) || item.ContentScope != test.scope {
				t.Fatalf("scope=%s hash=%s", item.ContentScope, item.ContentSHA256)
			}
			if test.count > 0 && len([]rune(item.ContentText)) != test.count {
				t.Fatalf("characters=%d", len([]rune(item.ContentText)))
			}
		})
	}
}
func TestTypedArticleRejectsUnsupportedBeforeNetwork(t *testing.T) {
	calls := 0
	client := NewClient(&http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) { calls++; return fixtureResponse(r, ""), nil })}, "")
	for _, request := range []contracts.ArticleRequest{{URL: "https://example.com/article"}, {URL: "https://www.tgb.cn/a/test", Mode: "unsupported"}} {
		if _, err := client.FetchArticle(context.Background(), request); err == nil {
			t.Fatal("unsupported request accepted")
		}
	}
	if calls != 0 {
		t.Fatalf("unsupported request triggered %d calls", calls)
	}
}
