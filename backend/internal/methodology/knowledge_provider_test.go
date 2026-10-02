package methodology

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/service"
)

type alternateKnowledge struct {
	tree          contracts.KnowledgeTree
	err           error
	treeCalls     atomic.Int32
	documentCalls atomic.Int32
}

type changingKnowledge struct {
	tree          contracts.KnowledgeTree
	text          string
	failPath      string
	treeError     error
	treeCalls     atomic.Int32
	documentCalls atomic.Int32
}

func (p *changingKnowledge) Tree(context.Context) (contracts.KnowledgeTree, error) {
	p.treeCalls.Add(1)
	return p.tree, p.treeError
}
func (p *changingKnowledge) KnowledgeIdentity() contracts.KnowledgeIdentity {
	return contracts.KnowledgeIdentity{SourceURL: p.tree.SourceURL}
}
func (p *changingKnowledge) Document(_ context.Context, path string) (contracts.KnowledgeDocument, error) {
	p.documentCalls.Add(1)
	if path == p.failPath {
		return contracts.KnowledgeDocument{}, errors.New("document unavailable")
	}
	return contracts.KnowledgeDocument{Content: []byte(p.text)}, nil
}
func newChangingKnowledge(sourceURL, text string) *changingKnowledge {
	return &changingKnowledge{tree: contracts.KnowledgeTree{Revision: "v1", Complete: true, SourceURL: sourceURL, Entries: []contracts.KnowledgeTreeEntry{{Path: "游资心法/同名作者/学习笔记.md", File: true, SourceURL: sourceURL + "/document"}}}, text: text}
}
func assertKnowledgeText(t *testing.T, library *Library, expected string) {
	t.Helper()
	current, err := library.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := library.Trader(context.Background(), current.Documents[0].TraderID)
	if err != nil || len(detail.Documents) == 0 || detail.Documents[0].Content != expected {
		t.Fatalf("cached text=%+v err=%v want=%q", detail, err, expected)
	}
}

func TestKnowledgeSupplierSwitchAndRestartDoNotReuseCollidingRevisions(t *testing.T) {
	ctx := context.Background()
	cfg := Config{CacheDir: filepath.Join(t.TempDir(), "cache"), DisableBuiltin: true}
	a := newChangingKnowledge("https://a.example/collection", "来源甲的完整心法正文，必须在来源乙失败时仍然保存。")
	cfg.KnowledgeProvider = service.NewKnowledge("supplier-a", a)
	library := NewLibrary(cfg)
	if _, err := library.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	assertKnowledgeText(t, library, a.text)
	b := newChangingKnowledge("https://b.example/collection", "来源乙同样使用v1版本，但正文与来源甲完全不同。")
	library.SetKnowledgeProvider(service.NewKnowledge("supplier-b", b))
	snapshot, err := library.Snapshot(ctx, false)
	if err != nil || snapshot.SourceURL != b.tree.SourceURL || b.documentCalls.Load() != 1 {
		t.Fatalf("supplier switch reused cache: %+v err=%v calls=%d", snapshot, err, b.documentCalls.Load())
	}
	assertKnowledgeText(t, library, b.text)
	if b.treeCalls.Load() != 1 {
		t.Fatal("successful replacement did not restore TTL cache")
	}
	// A registry identity must distinguish suppliers even when URLs and v1 match.
	c := newChangingKnowledge(b.tree.SourceURL, "来源丙复用相同路径和链接，重启仍须重新获取不同正文。")
	cfg.KnowledgeProvider = service.NewKnowledge("supplier-c", c)
	restarted := NewLibrary(cfg)
	if _, err := restarted.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	assertKnowledgeText(t, restarted, c.text)
	if c.treeCalls.Load() != 1 || c.documentCalls.Load() != 1 {
		t.Fatal("persisted supplier identity did not invalidate fresh cache")
	}
	// Rebinding the same identity explicitly also bypasses revision reuse once.
	c.text = "来源丙显式重绑定以后，供应商仍返回v1但文章正文已经更新。"
	restarted.SetKnowledgeProvider(service.NewKnowledge("supplier-c", c))
	if _, err := restarted.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	assertKnowledgeText(t, restarted, c.text)
	if c.documentCalls.Load() != 2 || c.treeCalls.Load() != 2 {
		t.Fatal("rebinding did not revalidate content once")
	}
	// A changed directory URL under an unchanged revision is also a new tree.
	c.tree.Entries[0].SourceURL = "https://b.example/revised-document"
	c.text = "相同供应商目录链接变化后，必须载入该目录对应的当前正文。"
	if _, err := restarted.Snapshot(ctx, true); err != nil {
		t.Fatal(err)
	}
	assertKnowledgeText(t, restarted, c.text)
	if c.documentCalls.Load() != 3 {
		t.Fatal("changed tree reused colliding revision")
	}
}

func TestKnowledgeFailedReplacementKeepsOldDocumentFiles(t *testing.T) {
	ctx := context.Background()
	a := newChangingKnowledge("https://a.example/collection", "这是旧来源经过验证的完整正文，失败替换不能覆盖此内容。")
	library := NewLibrary(Config{CacheDir: filepath.Join(t.TempDir(), "cache"), DisableBuiltin: true, KnowledgeProvider: service.NewKnowledge("supplier-a", a)})
	if _, err := library.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	b := newChangingKnowledge("https://b.example/collection", "新来源同路径先下载成功的正文，不应污染尚未完成替换的旧缓存。")
	b.failPath = "游资心法/同名作者/风险笔记.md"
	b.tree.Entries = append(b.tree.Entries, contracts.KnowledgeTreeEntry{Path: b.failPath, File: true, SourceURL: b.tree.SourceURL + "/risk"})
	library.SetKnowledgeProvider(service.NewKnowledge("supplier-b", b))
	snapshot, err := library.Snapshot(ctx, false)
	if err != nil || !snapshot.Stale || snapshot.SourceURL != a.tree.SourceURL {
		t.Fatalf("failed replacement lost old manifest: %+v err=%v", snapshot, err)
	}
	assertKnowledgeText(t, library, a.text)
	b.failPath = ""
	snapshot, err = library.Snapshot(ctx, false)
	if err != nil || snapshot.Stale || snapshot.SourceURL != b.tree.SourceURL {
		t.Fatalf("replacement could not recover: %+v err=%v", snapshot, err)
	}
	assertKnowledgeText(t, library, b.text)
	calls := b.treeCalls.Load()
	if _, err := library.Snapshot(ctx, false); err != nil || b.treeCalls.Load() != calls {
		t.Fatalf("successful recovery did not restore cache: %v", err)
	}
}

func TestKnowledgeSourceURLChangeAndLegacyManifestFallback(t *testing.T) {
	ctx := context.Background()
	a := newChangingKnowledge("https://a.example/collection", "旧版本manifest里的已验证正文，在来源无法更新时仍须可读。")
	cfg := Config{CacheDir: filepath.Join(t.TempDir(), "cache"), DisableBuiltin: true, KnowledgeProvider: service.NewKnowledge("same-supplier", a)}
	library := NewLibrary(cfg)
	if _, err := library.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	b := newChangingKnowledge("https://b.example/collection", "相同注册ID但集合URL变化，新进程必须识别旧缓存与新集合的差异。")
	cfg.KnowledgeProvider = service.NewKnowledge("same-supplier", b)
	restarted := NewLibrary(cfg)
	if _, err := restarted.Snapshot(ctx, false); err != nil {
		t.Fatal(err)
	}
	assertKnowledgeText(t, restarted, b.text)
	if b.documentCalls.Load() != 1 {
		t.Fatal("new source URL reused fresh old cache")
	}
	// Optional additions must not invalidate the shape of an existing v4 manifest.
	legacy, err := restarted.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	legacy.SourceID, legacy.TreeFingerprint = "", ""
	if err := restarted.saveManifest(legacy); err != nil {
		t.Fatal(err)
	}
	b.treeError = errors.New("new tree unavailable")
	restarted = NewLibrary(cfg)
	snapshot, err := restarted.Snapshot(ctx, false)
	if err != nil || !snapshot.Stale || snapshot.SourceURL != b.tree.SourceURL {
		t.Fatalf("legacy manifest became unreadable: %+v err=%v", snapshot, err)
	}
	assertKnowledgeText(t, restarted, b.text)
}

func (p *alternateKnowledge) Tree(context.Context) (contracts.KnowledgeTree, error) {
	p.treeCalls.Add(1)
	return p.tree, p.err
}

func (p *alternateKnowledge) Document(_ context.Context, relativePath string) (contracts.KnowledgeDocument, error) {
	p.documentCalls.Add(1)
	if relativePath != "游资心法/替换来源/学习笔记.md" {
		return contracts.KnowledgeDocument{}, errors.New("business filtering requested an unrelated document")
	}
	return contracts.KnowledgeDocument{Content: []byte("# 替换来源学习笔记\n\n只在情绪启动阶段关注龙头，先确定风险与仓位管理。")}, nil
}

func TestLibraryUsesReplaceableKnowledgeProviderAndRetainsCache(t *testing.T) {
	provider := &alternateKnowledge{tree: contracts.KnowledgeTree{Revision: "alternate-1", Complete: true, SourceURL: "https://fixture.example/collection", Entries: []contracts.KnowledgeTreeEntry{
		{Path: "游资心法/替换来源/学习笔记.md", File: true, SourceURL: "https://fixture.example/document"},
		{Path: "README.md", File: true},
		{Path: "游资心法/替换来源/ignored.md", File: false},
	}}}
	library := NewLibrary(Config{CacheDir: filepath.Join(t.TempDir(), "cache"), HermesHome: filepath.Join(t.TempDir(), "hermes"), DisableBuiltin: true, KnowledgeProvider: provider})
	snapshot, err := library.Snapshot(context.Background(), false)
	if err != nil || snapshot.SourceCommit != "alternate-1" || len(snapshot.Traders) != 1 || snapshot.Traders[0].Name != "替换来源" {
		t.Fatalf("alternate provider result %+v: %v", snapshot, err)
	}
	if snapshot.SourceURL != "https://fixture.example/collection" || !strings.HasPrefix(snapshot.Traders[0].SourceURL, snapshot.SourceURL) {
		t.Fatalf("alternate source metadata leaked GitHub identity: %+v", snapshot)
	}
	detail, err := library.Trader(context.Background(), snapshot.Traders[0].ID)
	if err != nil || len(detail.Documents) != 1 || detail.Documents[0].SourceURL != "https://fixture.example/document" {
		t.Fatalf("provider provenance %+v: %v", detail, err)
	}
	if provider.treeCalls.Load() != 1 || provider.documentCalls.Load() != 1 {
		t.Fatalf("cache access fetched unexpectedly: %d/%d", provider.treeCalls.Load(), provider.documentCalls.Load())
	}
	if _, err := library.Snapshot(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if provider.documentCalls.Load() != 1 {
		t.Fatal("unchanged revision redownloaded document")
	}
	provider.tree.Complete = false
	stale, err := library.Snapshot(context.Background(), true)
	if err != nil || !stale.Stale || !strings.Contains(stale.KnowledgeMessage, "目录不完整") {
		t.Fatalf("partial tree replaced complete cache %+v: %v", stale, err)
	}
	provider.err = errors.New("alternate source unavailable")
	stale, err = library.Snapshot(context.Background(), true)
	if err != nil || !stale.Stale || stale.SourceCommit != "alternate-1" {
		t.Fatalf("failed source did not retain old cache %+v: %v", stale, err)
	}
	text, err := library.ContextForPrompt(context.Background(), "游资替换来源的龙头和仓位管理心法？", 4000)
	if err != nil || !strings.Contains(text, "只在情绪启动阶段") {
		t.Fatalf("prompt business consumer lost supplied document: %s %v", text, err)
	}
}
