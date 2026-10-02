package contracts

import (
	"context"

	"easy-stock/backend/internal/foundation"
)

type ArticleAccessMode string

const (
	ArticleAutomatic  ArticleAccessMode = ""
	ArticlePublicPage ArticleAccessMode = "public_page"
)

// Headers carry caller-authorized acquisition context and never enter results,
// registry metadata, cache identity, or diagnostic output.
type ArticleRequest struct {
	Mode    ArticleAccessMode
	URL     string
	Headers map[string][]string
}
type ArticleSource interface {
	FetchArticle(context.Context, ArticleRequest) (foundation.Article, error)
}
