package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeProvider 只实现 Provider 必需方法，便于验证 Registry 的分派顺序。
type fakeProvider struct {
	name        string
	display     string
	code        string
	prefix      string
	resolveCall int
	matchCall   int
}

func (f *fakeProvider) Name() string        { return f.name }
func (f *fakeProvider) DisplayName() string { return f.display }
func (f *fakeProvider) Code() string        { return f.code }

func (f *fakeProvider) Match(_ context.Context, text string) (*Ref, bool) {
	f.matchCall++
	if !strings.HasPrefix(text, f.prefix) {
		return nil, false
	}
	return &Ref{ID: strings.TrimPrefix(text, f.prefix)}, true
}

func (f *fakeProvider) Canonicalize(_ context.Context, ref Ref) (Ref, error) {
	return ref, nil
}

func (f *fakeProvider) Resolve(_ context.Context, ref Ref) (*Track, error) {
	f.resolveCall++
	return &Track{Ref: ref, Title: "title"}, nil
}

func (f *fakeProvider) PageURL(ref Ref) string { return "https://" + f.name + "/page/" + ref.ID }

func (f *fakeProvider) Search(_ context.Context, keyword string, _, _ int) ([]TrackSummary, int, error) {
	return []TrackSummary{{Ref: Ref{ID: keyword}, Title: keyword}}, 1, nil
}

func (f *fakeProvider) Lyrics(_ context.Context, ref Ref) (*Lyric, error) {
	return &Lyric{LRC: "[00:00.00]lyric", Title: ref.ID}, nil
}

// shareProvider 只额外实现 ShareURLProvider。
type shareProvider struct {
	*fakeProvider
	shareURL string
}

func (f *shareProvider) ShareURL(ref Ref) string { return f.shareURL + ref.ID }

// metaProvider 只额外实现 MetadataProvider。
type metaProvider struct {
	*fakeProvider
	metadata *TrackSummary
}

func (f *metaProvider) Metadata(_ context.Context, _ Ref) (*TrackSummary, error) {
	return f.metadata, nil
}

func newTestRegistry(t *testing.T, defaultName string, providers ...Provider) *Registry {
	t.Helper()
	r, err := NewRegistry(defaultName, providers...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRegistryValidation(t *testing.T) {
	a := &fakeProvider{name: "a", prefix: "a:"}
	b := &fakeProvider{name: "b", prefix: "b:"}
	if _, err := NewRegistry("missing", a); err == nil {
		t.Error("默认 provider 未加载时应报错")
	}
	if _, err := NewRegistry("a", a, a); err == nil {
		t.Error("重复注册应报错")
	}
	if r := newTestRegistry(t, "", a, b); r.DefaultName() != "a" {
		t.Errorf("空默认名应取第一个 provider, got %q", r.DefaultName())
	}
}

func TestRegistryMatchOrder(t *testing.T) {
	a := &fakeProvider{name: "a", code: "aa", prefix: "x:"}
	b := &fakeProvider{name: "b", prefix: "x:"}
	r := newTestRegistry(t, "b", a, b)

	// 默认 provider 优先
	ref, p, ok := r.Match(context.Background(), "x:1")
	if !ok || p.Name() != "b" || ref.Provider != "b" || ref.ID != "1" {
		t.Fatalf("默认 provider 应优先: %+v %v %v", ref, p, ok)
	}

	// 显式 provider:id 优先于 Match
	ref, p, ok = r.Match(context.Background(), "a:42")
	if !ok || p.Name() != "a" || ref.ID != "42" {
		t.Fatalf("显式 provider 未命中: %+v %v %v", ref, p, ok)
	}

	// 也接受 provider 短 code
	ref, p, ok = r.Match(context.Background(), "aa:7")
	if !ok || p.Name() != "a" || ref.Provider != "a" || ref.ID != "7" {
		t.Fatalf("provider code 未命中: %+v %v %v", ref, p, ok)
	}

	a.matchCall, b.matchCall = 0, 0
	if _, _, ok := r.Match(context.Background(), "nope"); ok {
		t.Error("无法识别的输入不应命中")
	}
	if a.matchCall != 1 || b.matchCall != 1 {
		t.Errorf("每个 provider 应只匹配一次, got a=%d b=%d", a.matchCall, b.matchCall)
	}
}

func TestRegistryMatchProvider(t *testing.T) {
	a := &fakeProvider{name: "a", prefix: "x:"}
	r := newTestRegistry(t, "a", a)
	ref, ok := r.MatchProvider(context.Background(), "a", "x:7")
	if !ok || ref.ID != "7" || ref.Provider != "a" {
		t.Fatalf("MatchProvider = %+v, %v", ref, ok)
	}
	if _, ok := r.MatchProvider(context.Background(), "missing", "x:7"); ok {
		t.Error("未知 provider 不应命中")
	}
}

func TestRegistryShareURLFallback(t *testing.T) {
	withShare := &shareProvider{fakeProvider: &fakeProvider{name: "a", prefix: "a:"}, shareURL: "https://share/"}
	without := &fakeProvider{name: "b", prefix: "b:"}
	r := newTestRegistry(t, "a", withShare, without)

	if got := r.ShareURL(Ref{Provider: "a", ID: "1"}); got != "https://share/1" {
		t.Errorf("ShareURL = %q", got)
	}
	if got := r.ShareURL(Ref{Provider: "b", ID: "2"}); got != "https://b/page/2" {
		t.Errorf("未实现 ShareURLProvider 应回退 PageURL, got %q", got)
	}
	if got := r.ShareURL(Ref{Provider: "missing", ID: "3", URL: "https://raw"}); got != "https://raw" {
		t.Errorf("未知 provider 应返回 ref.URL, got %q", got)
	}
}

func TestRegistryMetadataFallback(t *testing.T) {
	a := &metaProvider{fakeProvider: &fakeProvider{name: "a", prefix: "a:"}, metadata: &TrackSummary{Title: "meta"}}
	b := &fakeProvider{name: "b", prefix: "b:"}
	r := newTestRegistry(t, "a", a, b)

	summary, err := r.Metadata(context.Background(), Ref{Provider: "a", ID: "1"})
	if err != nil || summary.Title != "meta" || summary.Ref.Provider != "a" {
		t.Fatalf("Metadata = %+v, %v", summary, err)
	}

	summary, err = r.Metadata(context.Background(), Ref{Provider: "b", ID: "2"})
	if err != nil || summary.Title != "title" || b.resolveCall != 1 {
		t.Fatalf("未实现 MetadataProvider 应回退 Resolve: %+v %v, calls %d", summary, err, b.resolveCall)
	}

	if _, err := r.Metadata(context.Background(), Ref{Provider: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知 provider 应返回 ErrNotFound, got %v", err)
	}
}

func TestRegistryDelegates(t *testing.T) {
	a := &fakeProvider{name: "a", prefix: "a:"}
	r := newTestRegistry(t, "a", a)
	ctx := context.Background()

	ref, err := r.Canonicalize(ctx, Ref{Provider: "a", ID: "1"})
	if err != nil || ref.Provider != "a" || ref.ID != "1" {
		t.Fatalf("Canonicalize = %+v, %v", ref, err)
	}
	if _, err := r.Canonicalize(ctx, Ref{Provider: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Canonicalize 未知 provider 应返回 ErrNotFound, got %v", err)
	}

	track, err := r.Resolve(ctx, Ref{Provider: "a", ID: "1"})
	if err != nil || track.Ref.Provider != "a" {
		t.Fatalf("Resolve = %+v, %v", track, err)
	}
	if _, err := r.Resolve(ctx, Ref{Provider: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve 未知 provider 应返回 ErrNotFound, got %v", err)
	}

	items, total, err := r.Search(ctx, "", "keyword", 10, 0)
	if err != nil || total != 1 || items[0].Ref.Provider != "a" {
		t.Fatalf("Search = %+v, %d, %v", items, total, err)
	}
	if _, _, err := r.Search(ctx, "missing", "keyword", 10, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Search 未知 provider 应返回 ErrNotFound, got %v", err)
	}

	if _, err := r.Lyrics(ctx, Ref{Provider: "a", ID: "1"}); err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if _, err := r.Lyrics(ctx, Ref{Provider: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Lyrics 未知 provider 应返回 ErrNotFound, got %v", err)
	}
}

// hostsProvider 额外声明 LinkHosts。
type hostsProvider struct {
	*fakeProvider
	hosts []string
}

func (f *hostsProvider) LinkHosts() []string { return f.hosts }

func TestMatchSkipsProvidersByLinkHosts(t *testing.T) {
	ne := &hostsProvider{fakeProvider: &fakeProvider{name: "netease", prefix: "https://music.163.com/song?id="}, hosts: []string{"music.163.com"}}
	sp := &hostsProvider{fakeProvider: &fakeProvider{name: "spotify", prefix: "https://open.spotify.com/track/"}, hosts: []string{"open.spotify.com"}}
	plain := &fakeProvider{name: "plain", prefix: "plain:"}
	r, err := NewRegistry("netease", ne, sp, plain)
	if err != nil {
		t.Fatal(err)
	}

	ref, _, ok := r.Match(context.Background(), "https://open.spotify.com/track/abc")
	if !ok || ref.Provider != "spotify" {
		t.Fatalf("spotify link matched %+v, %v", ref, ok)
	}
	if ne.matchCall != 0 {
		t.Errorf("netease Match called %d times for a spotify link", ne.matchCall)
	}

	// 未声明域名的 provider 总会被调用；与任何域名都不符的链接跳过声明了域名的 provider
	r.Match(context.Background(), "https://example.com/x")
	if ne.matchCall != 0 || sp.matchCall != 1 || plain.matchCall != 1 {
		t.Errorf("match calls ne=%d sp=%d plain=%d", ne.matchCall, sp.matchCall, plain.matchCall)
	}

	// 不像链接的文本（纯 ID）不预筛
	r.Match(context.Background(), "12345")
	if ne.matchCall != 1 || sp.matchCall != 2 {
		t.Errorf("plain id: match calls ne=%d sp=%d", ne.matchCall, sp.matchCall)
	}
}
