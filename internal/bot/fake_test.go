package bot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

// 测试用的假 provider，模拟网易云插件的链接格式与展示链接，
// 让 bot 单测只依赖 provider 接口而不需要 WASM 或真实 API。
const (
	fakeProviderName    = "netease"
	fakeProviderDisplay = "网易云音乐"
	fakeProviderCode    = "ne"
)

// 测试用的网易云链接构造，对应假 provider 的展示链接
func songLink(id int) string    { return fmt.Sprintf("https://music.163.com/song/%d", id) }
func programLink(id int) string { return fmt.Sprintf("https://music.163.com/program?id=%d", id) }

type fakeProvider struct {
	matchFn        func(context.Context, string) (*provider.Ref, bool)
	canonicalizeFn func(context.Context, provider.Ref) (provider.Ref, error)
	resolveFn      func(context.Context, provider.Ref) (*provider.Track, error)
	metadataFn     func(context.Context, provider.Ref) (*provider.TrackSummary, error)
	searchFn       func(context.Context, string, int, int) ([]provider.TrackSummary, int, error)
	lyricsFn       func(context.Context, provider.Ref) (*provider.Lyric, error)
	pageURLFn      func(provider.Ref) string
	shareURLFn     func(provider.Ref) string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		matchFn:        func(_ context.Context, text string) (*provider.Ref, bool) { return fakeMatch(text) },
		canonicalizeFn: func(_ context.Context, ref provider.Ref) (provider.Ref, error) { return ref, nil },
		resolveFn: func(_ context.Context, ref provider.Ref) (*provider.Track, error) {
			return nil, fmt.Errorf("%w: %s", provider.ErrNotFound, ref.ID)
		},
		searchFn: func(context.Context, string, int, int) ([]provider.TrackSummary, int, error) {
			return nil, 0, nil
		},
		lyricsFn: func(_ context.Context, ref provider.Ref) (*provider.Lyric, error) {
			return nil, fmt.Errorf("%w: %s", provider.ErrNotFound, ref.ID)
		},
		pageURLFn:  fakePageURL,
		shareURLFn: fakeShareURL,
	}
}

func (p *fakeProvider) Name() string        { return fakeProviderName }
func (p *fakeProvider) DisplayName() string { return fakeProviderDisplay }
func (p *fakeProvider) Code() string        { return fakeProviderCode }

func (p *fakeProvider) Match(ctx context.Context, text string) (*provider.Ref, bool) {
	if p.matchFn == nil {
		return fakeMatch(text)
	}
	return p.matchFn(ctx, text)
}

func (p *fakeProvider) Canonicalize(ctx context.Context, ref provider.Ref) (provider.Ref, error) {
	if p.canonicalizeFn == nil {
		return ref, nil
	}
	return p.canonicalizeFn(ctx, ref)
}

func (p *fakeProvider) Resolve(ctx context.Context, ref provider.Ref) (*provider.Track, error) {
	if p.resolveFn == nil {
		return nil, fmt.Errorf("%w: %s", provider.ErrNotFound, ref.ID)
	}
	return p.resolveFn(ctx, ref)
}

func (p *fakeProvider) PageURL(ref provider.Ref) string {
	if p.pageURLFn == nil {
		return fakePageURL(ref)
	}
	return p.pageURLFn(ref)
}

func (p *fakeProvider) ShareURL(ref provider.Ref) string {
	if p.shareURLFn == nil {
		return fakeShareURL(ref)
	}
	return p.shareURLFn(ref)
}

func (p *fakeProvider) Metadata(ctx context.Context, ref provider.Ref) (*provider.TrackSummary, error) {
	if p.metadataFn != nil {
		return p.metadataFn(ctx, ref)
	}
	track, err := p.Resolve(ctx, ref)
	if track == nil {
		return nil, err
	}
	return &provider.TrackSummary{
		Ref:        track.Ref,
		Title:      track.Title,
		Artists:    track.Artists,
		Album:      track.Album,
		DurationMS: track.DurationMS,
	}, err
}

func (p *fakeProvider) Search(ctx context.Context, keyword string, limit, offset int) ([]provider.TrackSummary, int, error) {
	if p.searchFn == nil {
		return nil, 0, nil
	}
	return p.searchFn(ctx, keyword, limit, offset)
}

func (p *fakeProvider) Lyrics(ctx context.Context, ref provider.Ref) (*provider.Lyric, error) {
	if p.lyricsFn == nil {
		return nil, fmt.Errorf("%w: %s", provider.ErrNotFound, ref.ID)
	}
	return p.lyricsFn(ctx, ref)
}

type fakeRecognizer struct {
	name string
	fn   func(context.Context, []byte, string) (*provider.Ref, error)
}

func (r fakeRecognizer) Name() string { return r.name }

func (r fakeRecognizer) Recognize(ctx context.Context, audio []byte, mime string) (*provider.Ref, error) {
	return r.fn(ctx, audio, mime)
}

type fakeAuthenticator struct {
	name string
}

func (a fakeAuthenticator) Name() string { return a.name }

func (a fakeAuthenticator) Status(context.Context) (provider.AuthStatus, error) {
	return provider.AuthStatus{Authenticated: true}, nil
}

func (a fakeAuthenticator) Begin(context.Context) (*provider.AuthChallenge, error) {
	return &provider.AuthChallenge{Type: "qr", Key: "key"}, nil
}

func (a fakeAuthenticator) Poll(context.Context, string) (*provider.AuthState, error) {
	return &provider.AuthState{State: provider.AuthSuccess}, nil
}

func (a fakeAuthenticator) Apply(context.Context, string) error { return nil }

func fakeSongURL(id string) string    { return "https://music.163.com/song?id=" + id }
func fakeSongPage(id string) string   { return "https://music.163.com/song/" + id }
func fakeProgramURL(id string) string { return "https://music.163.com/program?id=" + id }

func fakePageURL(ref provider.Ref) string {
	if ref.Sub == "program" {
		return fakeProgramURL(ref.ID)
	}
	if _, err := strconv.Atoi(ref.ID); err == nil {
		return fakeSongPage(ref.ID)
	}
	return ref.URL
}

func fakeShareURL(ref provider.Ref) string {
	if ref.Sub != "" {
		return ref.URL
	}
	if _, err := strconv.Atoi(ref.ID); err == nil {
		return fakeSongURL(ref.ID)
	}
	return ref.URL
}

// fakeMatch 模拟网易云插件的 Match：纯数字 ID、song/program/dj 链接与 /song/<id> 页面。
func fakeMatch(text string) (*provider.Ref, bool) {
	text = strings.TrimSpace(text)
	if id, err := strconv.Atoi(text); err == nil && id > 0 {
		return &provider.Ref{Provider: fakeProviderName, ID: strconv.Itoa(id)}, true
	}
	if ref, ok := fakeMatchURL(text); ok {
		return ref, true
	}
	// 分享文案里的链接前后可能有其他文字
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, "()[]{}<>,.;!?\"'")
		if ref, ok := fakeMatchURL(field); ok {
			return ref, true
		}
	}
	return nil, false
}

func fakeMatchURL(text string) (*provider.Ref, bool) {
	u, err := url.Parse(text)
	if err != nil || !strings.EqualFold(u.Host, "music.163.com") {
		return nil, false
	}
	path, query := u.Path, u.Query()
	if u.Fragment != "" {
		if fragment, err := url.Parse(u.Fragment); err == nil {
			path, query = fragment.Path, fragment.Query()
		}
	}
	if id := query.Get("id"); id != "" && id != "0" {
		if _, err := strconv.Atoi(id); err != nil {
			return nil, false
		}
		ref := &provider.Ref{Provider: fakeProviderName, ID: id}
		if strings.Contains(path, "program") || strings.Contains(path, "dj") {
			ref.Sub = "program"
			ref.URL = fakeProgramURL(id)
		}
		ref.PageURL = fakePageURL(*ref)
		ref.ShareText = fakeShareURL(*ref)
		return ref, true
	}
	if parts := strings.Split(strings.Trim(path, "/"), "/"); len(parts) == 2 && parts[0] == "song" {
		if _, err := strconv.Atoi(parts[1]); err == nil {
			ref := &provider.Ref{Provider: fakeProviderName, ID: parts[1]}
			ref.PageURL = fakePageURL(*ref)
			ref.ShareText = fakeShareURL(*ref)
			return ref, true
		}
	}
	return nil, false
}

var errFakeInvalidAudio = errors.New("invalid audio")
