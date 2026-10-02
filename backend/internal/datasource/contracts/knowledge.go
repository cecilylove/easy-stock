package contracts

import "context"

// KnowledgeTree describes a remote document collection. Complete distinguishes
// a complete listing from a supplier-truncated listing; an incomplete tree must
// never replace a complete local manifest.
type KnowledgeTree struct {
	SourceID  string
	Revision  string
	Complete  bool
	SourceURL string
	Entries   []KnowledgeTreeEntry
}

// KnowledgeIdentityProvider optionally declares stable acquisition identity
// before fetching a tree, so persisted caches can detect supplier changes.
type KnowledgeIdentity struct {
	SourceID, SourceURL string
}
type KnowledgeIdentityProvider interface {
	KnowledgeIdentity() KnowledgeIdentity
}

type KnowledgeTreeEntry struct {
	Path      string
	File      bool
	SourceURL string
}

type KnowledgeDocument struct {
	Content []byte
}

type KnowledgeTreeProvider interface {
	Tree(context.Context) (KnowledgeTree, error)
}

type KnowledgeDocumentProvider interface {
	Document(ctx context.Context, relativePath string) (KnowledgeDocument, error)
}

type KnowledgeProvider interface {
	KnowledgeTreeProvider
	KnowledgeDocumentProvider
}
