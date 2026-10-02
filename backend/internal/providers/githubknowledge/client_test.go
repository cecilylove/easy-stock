package githubknowledge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTreeMapsProtocolAndDocumentUsesEscapedPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "easy-stock" {
			t.Error("missing user agent")
		}
		switch r.URL.Path {
		case "/tree":
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Error("missing tree accept header")
			}
			fmt.Fprint(w, `{"sha":"revision-1","truncated":true,"tree":[{"path":"游资心法","type":"tree"},{"path":"游资心法/测试/学习笔记.md","type":"blob"}]}`)
		case "/raw/游资心法/测试/学习笔记.md":
			fmt.Fprint(w, "# 本地协议测试资料\n内容用于验证下载路径保持中文目录。")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client := NewClient(Config{TreeURL: upstream.URL + "/tree", RawBaseURL: upstream.URL + "/raw/"})
	tree, err := client.Tree(context.Background())
	if err != nil || tree.Revision != "revision-1" || tree.Complete || len(tree.Entries) != 2 || tree.Entries[0].File || !tree.Entries[1].File {
		t.Fatalf("unexpected mapped tree %+v: %v", tree, err)
	}
	if !strings.Contains(tree.Entries[1].SourceURL, "%E6%B8%B8") {
		t.Fatalf("unescaped source URL %s", tree.Entries[1].SourceURL)
	}
	document, err := client.Document(context.Background(), tree.Entries[1].Path)
	if err != nil || !strings.Contains(string(document.Content), "中文目录") {
		t.Fatalf("document download: %s %v", document.Content, err)
	}
}

func TestKnowledgeHTTPFailuresAndDocumentLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invalid-tree":
			fmt.Fprint(w, "{invalid")
		case "/raw/large.md":
			fmt.Fprint(w, strings.Repeat("x", (2<<20)+1))
		default:
			http.Error(w, "not available", http.StatusServiceUnavailable)
		}
	}))
	defer upstream.Close()
	client := NewClient(Config{TreeURL: upstream.URL + "/tree", RawBaseURL: upstream.URL + "/raw/"})
	if _, err := client.Tree(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("tree status error = %v", err)
	}
	if _, err := client.Document(context.Background(), "missing.md"); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("document status error = %v", err)
	}
	if doc, err := client.Document(context.Background(), "large.md"); err == nil || len(doc.Content) != 0 {
		t.Fatalf("oversized document accepted: size %d error %v", len(doc.Content), err)
	}
	client.config.TreeURL = upstream.URL + "/invalid-tree"
	if _, err := client.Tree(context.Background()); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Document(ctx, "missing.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}
