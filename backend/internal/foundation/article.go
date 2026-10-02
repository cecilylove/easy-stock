package foundation

import "time"

// Article is acquired source content, independent of review archive identity,
// subscriptions, AI summaries, or trading conclusions.
type Article struct {
	Source        string    `json:"source"`
	AuthorName    string    `json:"author_name"`
	Title         string    `json:"title"`
	Digest        string    `json:"digest"`
	ContentText   string    `json:"content_text"`
	CoverURL      string    `json:"cover_url,omitempty"`
	OriginalURL   string    `json:"original_url"`
	PublishedAt   time.Time `json:"published_at"`
	FetchedAt     time.Time `json:"fetched_at"`
	ContentSHA256 string    `json:"content_sha256"`
	ContentScope  string    `json:"content_scope"`
	TimeStatus    string    `json:"time_status"`
}
