package contracts

import (
	"context"
	"time"
)

// These acquisition payloads are independent of review archive entities. Review
// validates publication schema, identity and fingerprints before persisting them.
type ArchiveAuthor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Enabled  bool   `json:"enabled"`
}

type ArchiveManifest struct {
	SchemaVersion int             `json:"schema_version"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Authors       []ArchiveAuthor `json:"authors"`
}

type ArchivedArticle struct {
	SchemaVersion int       `json:"schema_version"`
	TradeDate     string    `json:"trade_date"`
	ID            string    `json:"id"`
	ExternalID    string    `json:"external_id"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	Platform      string    `json:"platform"`
	Title         string    `json:"title"`
	Digest        string    `json:"digest"`
	ContentText   string    `json:"content_text"`
	ContentSHA256 string    `json:"content_sha256"`
	SourceURL     string    `json:"source_url,omitempty"`
	PublishedAt   time.Time `json:"published_at"`
	RelatedStocks []string  `json:"related_stocks"`
	RelatedThemes []string  `json:"related_themes"`
}

type ArchiveAuthorsRequest struct{ ETag string }
type ArchiveAuthorsResult struct {
	Manifest    ArchiveManifest
	ETag        string
	Found       bool
	NotModified bool
}
type ArchiveArticleRequest struct {
	AuthorID  string
	TradeDate string
}
type ArchiveArticleResult struct {
	Article   ArchivedArticle
	ObjectURL string
	Found     bool
}
type ArchiveProvider interface {
	FetchAuthors(context.Context, ArchiveAuthorsRequest) (ArchiveAuthorsResult, error)
	FetchArchiveArticle(context.Context, ArchiveArticleRequest) (ArchiveArticleResult, error)
}
