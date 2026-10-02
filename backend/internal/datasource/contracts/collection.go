package contracts

import (
	"context"
	"easy-stock/backend/internal/foundation"
)

// Relative browser publication labels deliberately remain strings until review
// verifies/normalizes them; acquisition cannot invent a publication timestamp.
type BrowserArticle struct {
	Title       string `json:"title"`
	OriginalURL string `json:"original_url"`
	ContentText string `json:"content_text"`
	PublishedAt string `json:"published_at"`
}
type BrowserCollection struct {
	AuthorName string           `json:"author_name"`
	ExternalID string           `json:"external_id"`
	Articles   []BrowserArticle `json:"articles"`
	Error      string           `json:"error"`
}
type BrowserCollectionRequest struct {
	SourceID, BridgeURL, Token, ProfileID, HomepageURL string
	Limit                                              int
}
type BrowserCollectionProvider interface {
	CollectBrowser(context.Context, BrowserCollectionRequest) (BrowserCollection, error)
}
type AuthorLinksRequest struct{ SourceID, HomepageURL, Name, ExternalID, BaseURL, Credential string }
type AuthorLinks struct {
	URLs             []string
	Name, ExternalID string
}
type AuthorLinksProvider interface {
	DiscoverAuthorLinks(context.Context, AuthorLinksRequest) (AuthorLinks, error)
}
type AuthorizedArticleRequest struct{ SourceID, URL, BaseURL, Token string }
type AuthorizedArticleProvider interface {
	FetchAuthorizedArticle(context.Context, AuthorizedArticleRequest) (foundation.Article, error)
}
