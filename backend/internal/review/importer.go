package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/article"
)

const maxArticleBytes = article.MaxArticleBytes

// Importer projects acquired content into the review archive's stable identity.
// Network and source-specific parsing are owned by the content adapter.
type Importer struct{ source contracts.ArticleSource }

func NewImporter(client *http.Client, wechatAPIURL string) *Importer {
	return NewImporterWithSource(article.NewClient(client, wechatAPIURL))
}
func NewImporterWithSource(source contracts.ArticleSource) *Importer {
	return &Importer{source: source}
}

func (i *Importer) ImportURL(ctx context.Context, rawURL string) (Post, error) {
	item, err := i.source.FetchArticle(ctx, contracts.ArticleRequest{URL: rawURL})
	if err != nil {
		return Post{}, err
	}
	return acquiredArticlePost(item), nil
}
func (i *Importer) ImportURLWithHeaders(ctx context.Context, rawURL string, headers http.Header) (Post, error) {
	item, err := i.source.FetchArticle(ctx, contracts.ArticleRequest{URL: rawURL, Mode: contracts.ArticlePublicPage, Headers: map[string][]string(headers)})
	if err != nil {
		return Post{}, err
	}
	return acquiredArticlePost(item), nil
}
func acquiredArticlePost(item foundation.Article) Post {
	post := newPost(item.Source, item.OriginalURL, item.AuthorName, item.Title, item.ContentText, item.CoverURL, item.PublishedAt)
	if strings.TrimSpace(item.Digest) != "" {
		post.Digest = item.Digest
	}
	post.FetchedAt = item.FetchedAt
	return post
}

// Thin helpers preserve automation and fixture compatibility while its browser
// and subscription lifecycle stays in review. Source parsing is delegated.
var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func classifyURL(raw string) (*url.URL, string, error) { return article.ClassifyURL(raw) }
func publicPagePublishedAt(document string) (time.Time, bool) {
	return article.PublicPagePublishedAt(document)
}
func normalizeImportedContent(content string) (string, error) {
	return article.NormalizeImportedContent(content)
}
func cleanDocument(document string) string   { return article.CleanDocument(document) }
func articleDocument(document string) string { return article.ArticleDocument(document) }
func cleanInline(value string) string        { return article.CleanInline(value) }
func metaValue(document, attribute, value string) string {
	return article.MetaValue(document, attribute, value)
}
func matchText(pattern *regexp.Regexp, document string) string {
	return article.MatchText(pattern, document)
}

func newPost(source, originalURL, author, title, content, cover string, publishedAt time.Time) Post {
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(source + "\n" + originalURL))
	id := hex.EncodeToString(hash[:16])
	author = firstNonEmpty(strings.TrimSpace(author), sourceName(source))
	authorHash := sha256.Sum256([]byte(source + "\n" + author))
	return Post{
		ID:            id,
		Source:        source,
		ExternalID:    id,
		AuthorID:      hex.EncodeToString(authorHash[:12]),
		AuthorName:    author,
		Title:         strings.TrimSpace(title),
		Digest:        truncateRunes(content, 180),
		ContentText:   strings.TrimSpace(content),
		CoverURL:      cover,
		OriginalURL:   originalURL,
		PublishedAt:   publishedAt.UTC(),
		FetchedAt:     now,
		RelatedStocks: []string{},
		RelatedThemes: []string{},
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func sourceName(source string) string {
	switch source {
	case "wechat":
		return "微信公众号"
	case "xueqiu":
		return "雪球作者"
	case "taoguba":
		return "淘股吧作者"
	case "official":
		return "每日复盘"
	default:
		return source
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
